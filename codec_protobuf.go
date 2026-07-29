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

// ProtoCreatorRegister 支持注册工厂函数,完全消除反射
type ProtoCreatorRegister interface {
	RegisterCreator(command PacketCommand, creator ProtoMessageCreator)
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

	// 消息号和proto.Message工厂函数的映射表
	MessageCreatorMap map[PacketCommand]ProtoMessageCreator
}

func NewProtoCodec(protoMessageTypeMap map[PacketCommand]reflect.Type) *ProtoCodec {
	codec := &ProtoCodec{
		RingBufferCodec:   RingBufferCodec{},
		MessageCreatorMap: make(map[PacketCommand]ProtoMessageCreator),
	}
	// 兼容旧的map[PacketCommand]reflect.Type初始化方式
	if protoMessageTypeMap != nil {
		for cmd, t := range protoMessageTypeMap {
			if t != nil {
				reflectType := t
				codec.MessageCreatorMap[cmd] = func() proto.Message {
					return reflect.New(reflectType).Interface().(proto.Message)
				}
			}
		}
	}
	codec.DataEncoder = codec.EncodePacket
	codec.DataDecoder = codec.DecodePacket
	return codec
}

// 注册消息和proto.Message的映射(内部仍有反射,兼容旧用法)
//
//	protoMessage can be nil
func (c *ProtoCodec) Register(command PacketCommand, protoMessage proto.Message) {
	if protoMessage == nil {
		c.MessageCreatorMap[command] = nil
		return
	}
	// 用闭包捕获reflect.Type,后续创建消息时直接调用工厂函数,避免每次reflect.New
	reflectType := reflect.TypeOf(protoMessage).Elem()
	c.MessageCreatorMap[command] = func() proto.Message {
		return reflect.New(reflectType).Interface().(proto.Message)
	}
}

// 注册消息工厂函数,完全无反射
func (c *ProtoCodec) RegisterCreator(command PacketCommand, creator ProtoMessageCreator) {
	c.MessageCreatorMap[command] = creator
}

func (c *ProtoCodec) EncodePacket(connection Connection, packet Packet) ([][]byte, uint8) {
	protoMessage := packet.Message()
	headerFlags := uint8(0)
	// 先写入消息号
	// write PacketCommand
	commandBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(commandBytes, uint16(packet.Command()))
	var rpcCallId uint32
	if p, ok := packet.(*ProtoPacket); ok {
		rpcCallId = p.rpcCallId
	}
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
		headerFlags |= ErrorCode
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
	if c.ProtoPacketBytesEncoder != nil {
		if rpcCallId > 0 {
			return c.ProtoPacketBytesEncoder([][]byte{commandBytes, rpcCallIdBytes, errorCodeBytes, messageBytes}), headerFlags
		}
		return c.ProtoPacketBytesEncoder([][]byte{commandBytes, errorCodeBytes, messageBytes}), headerFlags
	}
	if rpcCallId > 0 {
		return [][]byte{commandBytes, rpcCallIdBytes, errorCodeBytes, messageBytes}, headerFlags
	}
	return [][]byte{commandBytes, errorCodeBytes, messageBytes}, headerFlags
}

func (c *ProtoCodec) DecodePacket(connection Connection, packetHeader PacketHeader, packetData []byte) Packet {
	decodedPacketData := packetData
	// Q:这里可以对packetData进行解码,如异或,解密,解压等
	// you can decode packetData here, such as XOR, decryption, decompression, etc
	if c.ProtoPacketBytesDecoder != nil {
		decodedPacketData = c.ProtoPacketBytesDecoder(packetData)
	}
	if len(decodedPacketData) < 2 {
		return nil
	}
	command := binary.LittleEndian.Uint16(decodedPacketData[:2])
	decodedPacketData = decodedPacketData[2:]
	rpcCallId := uint32(0)
	errorCode := uint32(0)
	// packetHeader can be nil when called directly (e.g. in tests),
	// in which case rpcCallId and errorCode are absent.
	if packetHeader != nil {
		if packetHeader.HasFlag(RpcCall) {
			if len(decodedPacketData) < 4 {
				return nil
			}
			rpcCallId = binary.LittleEndian.Uint32(decodedPacketData[:4])
			decodedPacketData = decodedPacketData[4:]
			//logger.Debug("read rpcCallId:%v", rpcCallId)
		}
		if packetHeader.HasFlag(ErrorCode) {
			if len(decodedPacketData) < 4 {
				return nil
			}
			errorCode = binary.LittleEndian.Uint32(decodedPacketData[:4])
			decodedPacketData = decodedPacketData[4:]
		}
	}
	if messageCreator, ok := c.MessageCreatorMap[PacketCommand(command)]; ok {
		if messageCreator != nil {
			newProtoMessage := messageCreator()
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
			// packetData有可能是ringbuffer里返回的内存,所以拷贝一份
			rawData := make([]byte, len(decodedPacketData))
			copy(rawData, decodedPacketData)
			return &ProtoPacket{
				command:   PacketCommand(command),
				rpcCallId: rpcCallId,
				errorCode: errorCode,
				data:      rawData,
			}
		}
	}
	if rpcCallId == 0 {
		logger.Warn("unregistered command:%v", command)
	}
	// 允许command不注册,留给业务层解析
	// packetData有可能是ringbuffer里返回的内存,所以拷贝一份
	rawData := make([]byte, len(decodedPacketData))
	copy(rawData, decodedPacketData)
	return &ProtoPacket{
		command:   PacketCommand(command),
		rpcCallId: rpcCallId,
		errorCode: errorCode,
		data:      rawData,
	}
}
