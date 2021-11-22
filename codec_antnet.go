package gnet

import (
	"encoding/binary"
	"google.golang.org/protobuf/proto"
)

// 模拟antnet的包头格式
type AntnetHeader struct {
	//Len   uint32 //数据长度,不包含head的长度
	Cmd   uint8  //命令
	Act   uint8  //动作
	Error uint16 //错误码
}

// 模拟antnet的数据包格式
type AntnetPacket struct {
	Head *AntnetHeader
	message proto.Message
}

func NewAntnetPacket(cmd, act uint8, err uint16, message proto.Message) *AntnetPacket {
	return &AntnetPacket{
		Head: &AntnetHeader{
			Cmd: cmd,
			Act: act,
			Error: err,
		},
		message: message,
	}
}

func (this *AntnetPacket) Command() PacketCommand {
	if this.Head == nil {
		return 0
	}
	return PacketCommand(uint16(this.Head.Cmd)<<8 + uint16(this.Head.Act))
}

func (this *AntnetPacket) Message() proto.Message {
	return this.message
}

func (this *AntnetPacket) GetStreamData() []byte {
	return nil
}

func (this *AntnetPacket) Clone() Packet {
	return &AntnetPacket{
		Head: &AntnetHeader{
			Cmd: this.Head.Cmd,
			Act: this.Head.Act,
			Error: this.Head.Error,
		},
		message: proto.Clone(this.message),
	}
}

func (this *AntnetPacket) CmdAct() int {
	return int(this.Command())
}

func (this *AntnetPacket) Cmd() uint8 {
	if this.Head == nil {
		return 0
	}
	return this.Head.Cmd
}

func (r *AntnetPacket) Act() uint8 {
	if r.Head == nil {
		return 0
	}
	return r.Head.Act
}

// 模拟antnet的编解码
type AntnetCodec struct {
	RingBufferCodec

	// 是否跟antnet的程序通讯
	// 如果gnet程序和gnet程序通讯,只是模拟antnet的数据包格式,PacketHeader.Len()=完整包(包头+包体)的长度-4
	// 如果gnet程序和antnet程序通讯,发给antnet的PacketHeader.Len()=完整包(包头+包体)的长度-8
	communicateWithAntnet bool

	// 在proto序列化后的数据,再做一层编码
	ProtoPacketBytesEncoder func(protoPacketBytes [][]byte) [][]byte

	// 在proto反序列化之前,先做一层解码
	ProtoPacketBytesDecoder func(packetData []byte) []byte

	// 消息号和proto.Message构造函数的映射表
	messageCreatorMap map[PacketCommand]ProtoMessageCreator
}

func NewAntnetCodec(communicateWithAntnet bool, messageCreatorMap map[PacketCommand]ProtoMessageCreator) *AntnetCodec {
	codec := &AntnetCodec{
		RingBufferCodec:RingBufferCodec{},
		communicateWithAntnet: communicateWithAntnet,
		messageCreatorMap: messageCreatorMap,
	}
	if codec.messageCreatorMap == nil {
		codec.messageCreatorMap = make(map[PacketCommand]ProtoMessageCreator)
	}
	codec.HeaderEncoder = codec.EncodeHeader
	codec.HeaderDecoder = codec.DecodeHeader
	codec.DataEncoder = codec.EncodePacket
	codec.DataDecoder = codec.DecodePacket
	return codec
}

// 注册消息
func (this *AntnetCodec) Register(command PacketCommand, creator ProtoMessageCreator ) {
	this.messageCreatorMap[command] = creator
}

func (this *AntnetCodec) EncodeHeader(connection Connection, packet Packet, headerData []byte) {
	if this.communicateWithAntnet {
		packetHeader := &PacketHeader{}
		packetHeader.ReadFrom(headerData)
		// 转换成antnet能正确解析的消息长度
		packetHeader.lenAndFlags = packetHeader.Len()-4
		packetHeader.WriteTo(headerData)
	}
}

func (this *AntnetCodec) DecodeHeader(connection Connection, headerData []byte) {
	if this.communicateWithAntnet {
		packetHeader := &PacketHeader{}
		packetHeader.ReadFrom(headerData)
		// 转换成gnet能正确解析的消息长度
		packetHeader.lenAndFlags = packetHeader.Len()+4
		packetHeader.WriteTo(headerData)
	}
}

func (this *AntnetCodec) EncodePacket(connection Connection, packet Packet) [][]byte {
	if antnetPacket,ok := packet.(*AntnetPacket); ok {
		// Cmd   uint8  //命令
		// Act   uint8  //动作
		// Error uint16 //错误码
		// 对gnet来说,Cmd,Act,Error都属于包体数据
		commandBytes := make([]byte,4)
		// BigEndian是为了兼容antnet
		// 写入Cmd+Act
		binary.BigEndian.PutUint16(commandBytes[0:2], uint16(packet.Command()))
		// 写入Error
		binary.BigEndian.PutUint16(commandBytes[2:], antnetPacket.Head.Error)
		messageBytes,err := proto.Marshal(antnetPacket.message)
		if err != nil {
			LogError("proto encode err:%v cmd:%v", err, packet.Command())
			return nil
		}
		// 这里可以继续对messageBytes进行编码,如异或,加密,压缩等
		if this.ProtoPacketBytesEncoder != nil {
			return this.ProtoPacketBytesEncoder([][]byte{commandBytes,messageBytes})
		}
		return [][]byte{commandBytes,messageBytes}
	}
	return nil
}

func (this *AntnetCodec) DecodePacket(connection Connection, packetHeader *PacketHeader, packetData []byte) Packet {
	decodedPacketData := packetData
	// Q:这里可以对packetData进行解码,如异或,解密,解压等
	if this.ProtoPacketBytesDecoder != nil {
		decodedPacketData = this.ProtoPacketBytesDecoder(packetData)
	}
	if len(decodedPacketData) < 4 {
		return nil
	}
	command := binary.BigEndian.Uint16(decodedPacketData[:2])
	errorCode := binary.BigEndian.Uint16(decodedPacketData[2:4])
	if messageCreator,ok := this.messageCreatorMap[PacketCommand(command)]; ok {
		newProtoMessage := messageCreator()
		err := proto.Unmarshal(decodedPacketData[4:], newProtoMessage)
		if err != nil {
			LogError("proto decode err:%v cmd:%v", err, command)
			return nil
		}
		return &AntnetPacket{
			Head: &AntnetHeader{
				Cmd: uint8(command>>8),
				Act: uint8(command&0xFF),
				Error: errorCode,
			},
			message: newProtoMessage,
		}
	}
	LogError("unsupport command:%v", command)
	return nil
}
