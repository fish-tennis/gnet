package gnet

import (
	"encoding/binary"
	"errors"
	"google.golang.org/protobuf/proto"
	"reflect"
	"unsafe"
)

const (
	SimplePacketHeaderSize = int(unsafe.Sizeof(SimplePacketHeader{}))
)

// a simple packet header for TcpConnectionSimple
// contains packet len and packet command
type SimplePacketHeader struct {
	// (flags << 24) | len
	// flags [0,255)
	// len [0,16M)
	LenAndFlags uint32
	Command     uint16
}

func NewSimplePacketHeader(len uint32, flags uint8, command PacketCommand) *SimplePacketHeader {
	return &SimplePacketHeader{
		LenAndFlags: uint32(flags)<<24 | len,
		Command:     uint16(command),
	}
}

// 包体长度,不包含包头的长度
//
//	packet body length (without packet header's length)
//	[0,0x00FFFFFF]
func (this *SimplePacketHeader) Len() uint32 {
	return this.LenAndFlags & 0x00FFFFFF
}

// 标记 [0,0xFF]
func (this *SimplePacketHeader) Flags() uint8 {
	return uint8(this.LenAndFlags >> 24)
}

func (this *SimplePacketHeader) SetFlags(flags uint8) {
	this.LenAndFlags = uint32(flags)<<24 | this.Len()
}

func (this *SimplePacketHeader) AddFlags(flag uint8) {
	flags := this.Flags() | flag
	this.SetFlags(flags)
}

func (this *SimplePacketHeader) HasFlag(flag uint8) bool {
	return (this.Flags() & flag) == flag
}

// 从字节流读取数据,len(messageHeaderData)>=MessageHeaderSize
// 使用小端字节序
//
//	parse LenAndFlags,Command from stream data
func (this *SimplePacketHeader) ReadFrom(packetHeaderData []byte) {
	this.LenAndFlags = binary.LittleEndian.Uint32(packetHeaderData)
	this.Command = binary.LittleEndian.Uint16(packetHeaderData[4:])
}

// 写入字节流,使用小端字节序
//
//	write LenAndFlags,Command to stream data
func (this *SimplePacketHeader) WriteTo(packetHeaderData []byte) {
	binary.LittleEndian.PutUint32(packetHeaderData, this.LenAndFlags)
	binary.LittleEndian.PutUint16(packetHeaderData[4:], this.Command)
}

// a simple protobuf codec for TcpConnectionSimple, without RingBuffer
// use SimplePacketHeader as PacketHeader
type SimpleProtoCodec struct {
	// 消息号和proto.Message type的映射表
	MessageCreatorMap map[PacketCommand]reflect.Type
}

func NewSimpleProtoCodec() *SimpleProtoCodec {
	codec := &SimpleProtoCodec{
		MessageCreatorMap: make(map[PacketCommand]reflect.Type),
	}
	return codec
}

func (this *SimpleProtoCodec) PacketHeaderSize() uint32 {
	return uint32(SimplePacketHeaderSize)
}

// 注册消息和proto.Message的映射
//
//	protoMessage can be nil
func (this *SimpleProtoCodec) Register(command PacketCommand, protoMessage proto.Message) {
	if protoMessage == nil {
		this.MessageCreatorMap[command] = nil
		return
	}
	this.MessageCreatorMap[command] = reflect.TypeOf(protoMessage).Elem()
}

func (this *SimpleProtoCodec) CreatePacketHeader(connection Connection, packet Packet, packetData []byte) PacketHeader {
	if packet == nil {
		return NewSimplePacketHeader(0, 0, 0)
	}
	return NewSimplePacketHeader(uint32(len(packetData)), 0, packet.Command())
}

func (this *SimpleProtoCodec) Encode(connection Connection, packet Packet) []byte {
	packetBodyData := packet.GetStreamData()
	if packetBodyData == nil {
		protoMsg := packet.Message()
		if protoMsg != nil {
			var err error
			packetBodyData, err = proto.Marshal(protoMsg)
			if err != nil {
				logger.Error("%v proto %v err:%v", connection.GetConnectionId(), packet.Command(), err.Error())
				return nil
			}
		}
	}
	if packet.ErrorCode() != 0 || packet.RpcCallId() > 0 {
		extraLen := 4
		offset := 0
		if packet.ErrorCode() != 0 && packet.RpcCallId() > 0 {
			extraLen = 8
		}
		packetData := make([]byte, len(packetBodyData)+extraLen)
		if packet.ErrorCode() != 0 {
			binary.LittleEndian.PutUint32(packetData, packet.ErrorCode())
			offset += 4
		}
		if packet.RpcCallId() > 0 {
			binary.LittleEndian.PutUint32(packetData[offset:], packet.RpcCallId())
			offset += 4
		}
		copy(packetData[offset:], packetBodyData)
		return packetData
	}
	return packetBodyData
}

func (this *SimpleProtoCodec) Decode(connection Connection, data []byte) (newPacket Packet, err error) {
	decodedPacketData := data
	if len(decodedPacketData) < SimplePacketHeaderSize {
		return nil, ErrPacketLength
	}
	packetHeader := &SimplePacketHeader{}
	packetHeader.ReadFrom(decodedPacketData)
	decodedPacketData = decodedPacketData[SimplePacketHeaderSize:]
	command := packetHeader.Command
	rpcCallId := uint32(0)
	if packetHeader.HasFlag(RpcCall) {
		if len(decodedPacketData) < 4 {
			return nil, errors.New("rpcCallId decode err")
		}
		rpcCallId = binary.LittleEndian.Uint32(decodedPacketData[:4])
		decodedPacketData = decodedPacketData[4:]
	}
	errorCode := uint32(0)
	if packetHeader.HasFlag(ErrorCode) {
		if len(decodedPacketData) < 4 {
			return nil, errors.New("errorCode decode err")
		}
		errorCode = binary.LittleEndian.Uint32(decodedPacketData[:4])
		decodedPacketData = decodedPacketData[4:]
	}
	if protoMessageType, ok := this.MessageCreatorMap[PacketCommand(command)]; ok {
		if protoMessageType != nil {
			newProtoMessage := reflect.New(protoMessageType).Interface().(proto.Message)
			// TODO: check len(decodedPacketData) > 0?
			err = proto.Unmarshal(decodedPacketData, newProtoMessage)
			if err != nil {
				logger.Error("proto decode err:%v cmd:%v", err, command)
				return nil, err
			}
			return &ProtoPacket{
				command:   PacketCommand(command),
				rpcCallId: rpcCallId,
				errorCode: errorCode,
				message:   newProtoMessage,
			}, nil
		} else {
			// 支持只注册了消息号,没注册proto结构体的用法
			// support Register(command, nil), return the direct stream data to application layer
			return &ProtoPacket{
				command:   PacketCommand(command),
				rpcCallId: rpcCallId,
				errorCode: errorCode,
				data:      decodedPacketData,
			}, nil
		}
	}
	// rpc模式允许response消息不注册,留给业务层解析
	if rpcCallId > 0 {
		return &ProtoPacket{
			command:   PacketCommand(command),
			rpcCallId: rpcCallId,
			errorCode: errorCode,
			data:      decodedPacketData,
		}, nil
	}
	logger.Error("unSupport command:%v", command)
	return nil, ErrNotSupport
}
