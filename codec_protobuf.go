package gnet

import (
	"encoding/binary"
	"google.golang.org/protobuf/proto"
	"reflect"
)

// proto.Message ctor func
type ProtoMessageCreator func() proto.Message

// Packet ctor func
type PacketCreator func() Packet

type ProtoRegister interface {
	Register(command PacketCommand, protoMessage proto.Message)
}

// codec for protobuf
//
//	use DefaultPacketHeader,RingBufferCodec
type ProtoCodec struct {
	RingBufferCodec

	// 在proto序列化后的数据,再做一层编码
	// encoder after proto.Message serialize
	ProtoPacketBytesEncoder func(protoPacketBytes [][]byte) [][]byte

	// 在proto反序列化之前,先做一层解码
	// decoder before proto.Message deserialize
	ProtoPacketBytesDecoder func(packetData []byte) []byte

	// 消息号和proto.Message type的映射表
	MessageCreatorMap map[PacketCommand]reflect.Type
}

func NewProtoCodec(protoMessageTypeMap map[PacketCommand]reflect.Type) *ProtoCodec {
	codec := &ProtoCodec{
		RingBufferCodec:   RingBufferCodec{},
		MessageCreatorMap: protoMessageTypeMap,
	}
	if codec.MessageCreatorMap == nil {
		codec.MessageCreatorMap = make(map[PacketCommand]reflect.Type)
	}
	codec.DataEncoder = codec.EncodePacket
	codec.DataDecoder = codec.DecodePacket
	return codec
}

// 注册消息和proto.Message的映射
//
//	protoMessage can be nil
func (this *ProtoCodec) Register(command PacketCommand, protoMessage proto.Message) {
	if protoMessage == nil {
		this.MessageCreatorMap[command] = nil
		return
	}
	this.MessageCreatorMap[command] = reflect.TypeOf(protoMessage).Elem()
}

func (this *ProtoCodec) EncodePacket(connection Connection, packet Packet) ([][]byte, uint8) {
	protoMessage := packet.Message()
	headerFlags := uint8(0)
	// 先写入消息号
	// write PacketCommand
	commandBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(commandBytes, uint16(packet.Command()))
	rpcCallId := packet.(*ProtoPacket).rpcCallId
	var rpcCallIdBytes []byte
	// rpcCall才会写入rpcCallId
	if rpcCallId > 0 {
		rpcCallIdBytes = make([]byte, 4)
		binary.LittleEndian.PutUint32(rpcCallIdBytes, rpcCallId)
		headerFlags |= RpcCall
		//logger.Debug("write rpcCallId:%v", rpcCallId)
	}
	var errorCodeBytes []byte
	if p, ok := packet.(*ProtoPacket); ok && p.errorCode != 0 {
		errorCodeBytes = make([]byte, 4)
		binary.LittleEndian.PutUint32(errorCodeBytes, uint32(p.errorCode))
		headerFlags |= HasError
	}
	var messageBytes []byte
	if protoMessage != nil {
		var err error
		messageBytes, err = proto.Marshal(protoMessage)
		if err != nil {
			logger.Error("proto encode err:%v cmd:%v", err, packet.Command())
			return nil, 0
		}
	} else {
		// 支持提前序列化好的数据
		// support direct encoded data from application layer
		messageBytes = packet.GetStreamData()
	}
	// 这里可以继续对messageBytes进行编码,如异或,加密,压缩等
	// you can continue to encode messageBytes here, such as XOR, encryption, compression, etc
	if this.ProtoPacketBytesEncoder != nil {
		if rpcCallId > 0 {
			return this.ProtoPacketBytesEncoder([][]byte{commandBytes, rpcCallIdBytes, errorCodeBytes, messageBytes}), headerFlags
		}
		return this.ProtoPacketBytesEncoder([][]byte{commandBytes, errorCodeBytes, messageBytes}), headerFlags
	}
	if rpcCallId > 0 {
		return [][]byte{commandBytes, rpcCallIdBytes, errorCodeBytes, messageBytes}, headerFlags
	}
	return [][]byte{commandBytes, errorCodeBytes, messageBytes}, headerFlags
}

func (this *ProtoCodec) DecodePacket(connection Connection, packetHeader PacketHeader, packetData []byte) Packet {
	decodedPacketData := packetData
	// Q:这里可以对packetData进行解码,如异或,解密,解压等
	// you can decode packetData here, such as XOR, decryption, decompression, etc
	if this.ProtoPacketBytesDecoder != nil {
		decodedPacketData = this.ProtoPacketBytesDecoder(packetData)
	}
	if len(decodedPacketData) < 2 {
		return nil
	}
	command := binary.LittleEndian.Uint16(decodedPacketData[:2])
	decodedPacketData = decodedPacketData[2:]
	rpcCallId := uint32(0)
	if packetHeader.HasFlag(RpcCall) {
		if len(decodedPacketData) < 4 {
			return nil
		}
		rpcCallId = binary.LittleEndian.Uint32(decodedPacketData[:4])
		decodedPacketData = decodedPacketData[4:]
		//logger.Debug("read rpcCallId:%v", rpcCallId)
	}
	errorCode := uint32(0)
	if packetHeader.HasFlag(HasError) {
		if len(decodedPacketData) < 4 {
			return nil
		}
		errorCode = binary.LittleEndian.Uint32(decodedPacketData[:4])
		decodedPacketData = decodedPacketData[4:]
	}
	if protoMessageType, ok := this.MessageCreatorMap[PacketCommand(command)]; ok {
		if protoMessageType != nil {
			newProtoMessage := reflect.New(protoMessageType).Interface().(proto.Message)
			// TODO: check len(decodedPacketData) > 0?
			err := proto.Unmarshal(decodedPacketData, newProtoMessage)
			if err != nil {
				logger.Error("proto decode err:%v cmd:%v", err, command)
				return nil
			}
			return &ProtoPacket{
				command:   PacketCommand(command),
				rpcCallId: rpcCallId,
				errorCode: errorCode,
				message:   newProtoMessage,
			}
		} else {
			// 支持只注册了消息号,没注册proto结构体的用法
			// support Register(command, nil), return the direct stream data to application layer
			return &ProtoPacket{
				command:   PacketCommand(command),
				rpcCallId: rpcCallId,
				errorCode: errorCode,
				data:      decodedPacketData,
			}
		}
	}
	// rpc模式允许response消息不注册,留给业务层解析
	if rpcCallId > 0 {
		return &ProtoPacket{
			command:   PacketCommand(command),
			rpcCallId: rpcCallId,
			errorCode: errorCode,
			data:      decodedPacketData,
		}
	}
	logger.Error("unSupport command:%v", command)
	return nil
}
