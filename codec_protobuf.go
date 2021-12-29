package gnet

import (
	"encoding/binary"
	"google.golang.org/protobuf/proto"
)

// proto.Message构造函数
type ProtoMessageCreator func() proto.Message

type ProtoRegister interface {
	Register(command PacketCommand, creator ProtoMessageCreator )
}

// proto.Message编解码
type ProtoCodec struct {
	RingBufferCodec

	// 在proto序列化后的数据,再做一层编码
	ProtoPacketBytesEncoder func(protoPacketBytes [][]byte) [][]byte

	// 在proto反序列化之前,先做一层解码
	ProtoPacketBytesDecoder func(packetData []byte) []byte

	// 消息号和proto.Message构造函数的映射表
	MessageCreatorMap map[PacketCommand]ProtoMessageCreator
}

func NewProtoCodec(messageCreatorMap map[PacketCommand]ProtoMessageCreator) *ProtoCodec {
	codec := &ProtoCodec{
		RingBufferCodec:   RingBufferCodec{},
		MessageCreatorMap: messageCreatorMap,
	}
	if codec.MessageCreatorMap == nil {
		codec.MessageCreatorMap = make(map[PacketCommand]ProtoMessageCreator)
	}
	codec.DataEncoder = codec.EncodePacket
	codec.DataDecoder = codec.DecodePacket
	return codec
}

// 注册消息
func (this *ProtoCodec) Register(command PacketCommand, creator ProtoMessageCreator ) {
	this.MessageCreatorMap[command] = creator
}

func (this *ProtoCodec) EncodePacket(connection Connection, packet Packet) [][]byte {
	protoMessage := packet.Message()
	// 先写入消息号
	commandBytes := make([]byte,2)
	binary.LittleEndian.PutUint16(commandBytes, uint16(packet.Command()))
	var messageBytes []byte
	if protoMessage != nil {
		var err error
		messageBytes,err = proto.Marshal(protoMessage)
		if err != nil {
			logger.Error("proto encode err:%v cmd:%v", err, packet.Command())
			return nil
		}
	} else {
		// 支持提前序列化好的数据
		messageBytes = packet.GetStreamData()
	}
	// 这里可以继续对messageBytes进行编码,如异或,加密,压缩等
	if this.ProtoPacketBytesEncoder != nil {
		return this.ProtoPacketBytesEncoder([][]byte{commandBytes,messageBytes})
	}
	return [][]byte{commandBytes,messageBytes}
	//fullData := make([]byte, len(commandBytes)+len(messageBytes))
	//n := copy(fullData, commandBytes)
	//copy(fullData[n:], messageBytes)
	//return fullData
}

func (this *ProtoCodec) DecodePacket(connection Connection, packetHeader *PacketHeader, packetData []byte) Packet {
	decodedPacketData := packetData
	// Q:这里可以对packetData进行解码,如异或,解密,解压等
	if this.ProtoPacketBytesDecoder != nil {
		decodedPacketData = this.ProtoPacketBytesDecoder(packetData)
	}
	if len(decodedPacketData) < 2 {
		return nil
	}
	command := binary.LittleEndian.Uint16(decodedPacketData[:2])
	if messageCreator,ok := this.MessageCreatorMap[PacketCommand(command)]; ok {
		var newProtoMessage proto.Message
		if messageCreator != nil {
			newProtoMessage = messageCreator()
			err := proto.Unmarshal(decodedPacketData[2:], newProtoMessage)
			if err != nil {
				logger.Error("proto decode err:%v cmd:%v", err, command)
				return nil
			}
		}
		return &ProtoPacket{
			command: PacketCommand(command),
			message: newProtoMessage,
		}
	}
	logger.Error("unsupport command:%v", command)
	return nil
}
