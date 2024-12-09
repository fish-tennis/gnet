package gnet

import (
	"encoding/binary"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"unsafe"
)

const (
	// 默认包头长度
	// the default packet header size
	DefaultPacketHeaderSize = int(unsafe.Sizeof(DefaultPacketHeader{}))

	// 数据包长度限制(16M)
	// the default packet data size limit
	MaxPacketDataSize = 0x00FFFFFF

	// packet header contains rpcCallId
	RpcCall uint8 = 1 << 0
	// compress packet data
	Compress uint8 = 1 << 1
	// packet contains error code
	ErrorCode uint8 = 1 << 2
)

// interface for PacketHeader
type PacketHeader interface {
	Len() uint32
	HasFlag(flag uint8) bool
	ReadFrom(packetHeaderData []byte)
	WriteTo(packetHeaderData []byte)
}

// 消息号[0,65535]
type PacketCommand uint16

// 默认包头,支持小于16M的数据包
//
//	default packet header
type DefaultPacketHeader struct {
	// (flags << 24) | len
	// flags [0,255)
	// len [0,16M)
	LenAndFlags uint32
}

func NewDefaultPacketHeader(len uint32, flags uint8) *DefaultPacketHeader {
	return &DefaultPacketHeader{
		LenAndFlags: uint32(flags)<<24 | len,
	}
}

// 包体长度,不包含包头的长度
//
//	packet body length (without packet header's length)
//	[0,0x00FFFFFF]
func (h *DefaultPacketHeader) Len() uint32 {
	return h.LenAndFlags & 0x00FFFFFF
}

// 标记 [0,0xFF]
func (h *DefaultPacketHeader) Flags() uint8 {
	return uint8(h.LenAndFlags >> 24)
}

func (h *DefaultPacketHeader) SetFlags(flags uint8) {
	h.LenAndFlags = uint32(flags)<<24 | h.Len()
}

func (h *DefaultPacketHeader) AddFlags(flag uint8) {
	flags := h.Flags() | flag
	h.SetFlags(flags)
}

func (h *DefaultPacketHeader) HasFlag(flag uint8) bool {
	return (h.Flags() & flag) == flag
}

// 从字节流读取数据,len(messageHeaderData)>=MessageHeaderSize
// 使用小端字节序
//
//	parse LenAndFlags from stream data
func (h *DefaultPacketHeader) ReadFrom(packetHeaderData []byte) {
	h.LenAndFlags = binary.LittleEndian.Uint32(packetHeaderData)
}

// 写入字节流,使用小端字节序
//
//	write LenAndFlags to stream data
func (h *DefaultPacketHeader) WriteTo(packetHeaderData []byte) {
	binary.LittleEndian.PutUint32(packetHeaderData, h.LenAndFlags)
}

// interface for packet
type Packet interface {
	// 消息号
	// 没有把消息号放在PacketHeader里,因为对TCP网络层来说,只需要知道每个数据包的分割长度就可以了,
	// 至于数据包具体的格式,不该是网络层关心的事情
	// 消息号也不是必须放在这里的,但是游戏项目一般都是用消息号,为了减少封装层次,就放这里了
	//  packet command number
	Command() PacketCommand

	RpcCallId() uint32

	SetRpcCallId(rpcCallId uint32)

	ErrorCode() uint32

	// default protobuf
	Message() proto.Message

	// 提供一个二进制数据的接口,支持外部直接传入序列化的字节流数据
	//  support stream data, outside can direct pass the serialized data
	GetStreamData() []byte

	// deep copy
	Clone() Packet
}

// packet for proto.Message
type ProtoPacket struct {
	command   PacketCommand
	rpcCallId uint32
	errorCode uint32
	message   proto.Message
	data      []byte
}

func NewProtoPacketEx(args ...any) *ProtoPacket {
	p := &ProtoPacket{}
	for _, arg := range args {
		if arg == nil {
			continue
		}
		switch v := arg.(type) {
		case PacketCommand:
			p.command = v
		case protoreflect.Enum:
			// protobuf生成的枚举值
			p.command = PacketCommand(v.Number())
		case uint16:
			p.command = PacketCommand(v)
		case int:
			p.command = PacketCommand(v)
		case int16:
			p.command = PacketCommand(v)
		case int32:
			p.command = PacketCommand(v)
		case int64:
			p.command = PacketCommand(v)
		case uint:
			p.command = PacketCommand(v)
		case uint32:
			p.command = PacketCommand(v)
		case uint64:
			p.command = PacketCommand(v)
		case proto.Message:
			p.message = v
		case []byte:
			p.data = v
		default:
			logger.Error("unsupported type:%v", v)
		}
	}
	return p
}

func NewProtoPacket(command PacketCommand, message proto.Message) *ProtoPacket {
	return &ProtoPacket{
		command: command,
		message: message,
	}
}

func NewProtoPacketWithData(command PacketCommand, data []byte) *ProtoPacket {
	return &ProtoPacket{
		command: command,
		data:    data,
	}
}

func (p *ProtoPacket) Command() PacketCommand {
	return p.command
}

func (p *ProtoPacket) Message() proto.Message {
	return p.message
}

func (p *ProtoPacket) RpcCallId() uint32 {
	return p.rpcCallId
}

func (p *ProtoPacket) SetRpcCallId(rpcCallId uint32) {
	p.rpcCallId = rpcCallId
}

func (p *ProtoPacket) WithRpc(arg any) *ProtoPacket {
	switch v := arg.(type) {
	case uint32:
		p.rpcCallId = v
	case Packet:
		p.rpcCallId = v.RpcCallId()
	}
	return p
}

func (p *ProtoPacket) ErrorCode() uint32 {
	return p.errorCode
}

func (p *ProtoPacket) SetErrorCode(code uint32) *ProtoPacket {
	p.errorCode = code
	return p
}

// 某些特殊需求会直接使用序列化好的数据
//
//	support stream data
func (p *ProtoPacket) GetStreamData() []byte {
	return p.data
}

// deep copy
func (p *ProtoPacket) Clone() Packet {
	newPacket := &ProtoPacket{
		command:   p.command,
		rpcCallId: p.rpcCallId,
		errorCode: p.errorCode,
		message:   proto.Clone(p.message),
	}
	if len(p.data) > 0 {
		newPacket.data = make([]byte, len(p.data))
		copy(newPacket.data, p.data)
	}
	return newPacket
}

// 只包含一个[]byte的数据包
//
//	packet which only have a byte array
type DataPacket struct {
	// NOTE: 暂未实现rpcCallId和errorCode
	// 由业务层自行实现
	// ProtoPacket也支持[]byte,所以建议直接使用ProtoPacket
	data []byte
}

func NewDataPacket(data []byte) *DataPacket {
	return &DataPacket{data: data}
}

func NewDataPacketWithHeader(header PacketHeader, data []byte) *DataPacket {
	return &DataPacket{data: data}
}

func (p *DataPacket) Command() PacketCommand {
	return 0
}

func (p *DataPacket) Message() proto.Message {
	return nil
}

func (p *DataPacket) GetStreamData() []byte {
	return p.data
}

// NOTE: 暂未实现rpcCallId
func (p *DataPacket) RpcCallId() uint32 {
	return 0
}

// NOTE: 暂未实现rpcCallId
func (p *DataPacket) SetRpcCallId(rpcCallId uint32) {
}

// NOTE: 暂未实现ErrorCode
func (p *DataPacket) ErrorCode() uint32 {
	return 0
}

// NOTE: 暂未实现ErrorCode
func (p *DataPacket) SetErrorCode(code uint32) *DataPacket {
	return p
}

// deep copy
func (p *DataPacket) Clone() Packet {
	newPacket := &DataPacket{data: make([]byte, len(p.data))}
	copy(newPacket.data, p.data)
	return newPacket
}
