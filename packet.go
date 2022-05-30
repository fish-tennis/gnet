package gnet

import (
	"encoding/binary"
	"google.golang.org/protobuf/proto"
	"unsafe"
)

const (
	// 默认包头长度
	DefaultPacketHeaderSize = int(unsafe.Sizeof(DefaultPacketHeader{}))
	// 数据包长度限制(16M)
	MaxPacketDataSize = 0x00FFFFFF
)

// 包头接口
type PacketHeader interface {
	Len() uint32
	ReadFrom(messageHeaderData []byte)
	WriteTo(messageHeaderData []byte)
}

// 消息号
type PacketCommand uint16

// 默认包头,支持小于16M的数据包
type DefaultPacketHeader struct {
	// (flags << 24) | len
	// flags [0,255)
	// len [0,16M)
	LenAndFlags uint32
}

func NewDefaultPacketHeader(len uint32,flags uint8) *DefaultPacketHeader {
	return &DefaultPacketHeader{
		LenAndFlags: uint32(flags)<<24 | len,
	}
}

// 包体长度,不包含包头的长度
// [0,0x00FFFFFF]
func (this *DefaultPacketHeader) Len() uint32 {
	return this.LenAndFlags & 0x00FFFFFF
}

// 标记 [0,0xFF]
func (this *DefaultPacketHeader) Flags() uint8 {
	return uint8(this.LenAndFlags >> 24)
}

// 从字节流读取数据,len(messageHeaderData)>=MessageHeaderSize
// 使用小端字节序
func (this *DefaultPacketHeader) ReadFrom(messageHeaderData []byte) {
	this.LenAndFlags = binary.LittleEndian.Uint32(messageHeaderData)
}

// 写入字节流,使用小端字节序
func (this *DefaultPacketHeader) WriteTo(messageHeaderData []byte) {
	binary.LittleEndian.PutUint32(messageHeaderData, this.LenAndFlags)
}


// 数据包接口
type Packet interface {
	// 消息号
	// 没有把消息号放在PacketHeader里,因为对TCP网络层来说,只需要知道每个数据包的分割长度就可以了,
	// 至于数据包具体的格式,不该是网络层关心的事情
	// 消息号也不是必须放在这里的,但是游戏项目一般都是用消息号,为了减少封装层次,就放这里了
	Command() PacketCommand

	// 默认使用protobuf
	Message() proto.Message

	// 预留一个二进制数据的接口,支持外部直接传入序列号的字节流数据
	GetStreamData() []byte

	// deep copy
	Clone() Packet
}


// proto数据包
type ProtoPacket struct {
	command PacketCommand
	message proto.Message
	data []byte
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
		data: data,
	}
}

func (this *ProtoPacket) Command() PacketCommand {
	return this.command
}

func (this *ProtoPacket) Message() proto.Message {
	return this.message
}

// 某些特殊需求会直接使用序列化好的数据
func (this *ProtoPacket) GetStreamData() []byte {
	return this.data
}

// deep copy
func (this *ProtoPacket) Clone() Packet {
	return &ProtoPacket{
		command: this.command,
		message: proto.Clone(this.message),
	}
}


// 只包含一个[]byte的数据包
type DataPacket struct {
	data []byte
}

func NewDataPacket(data []byte) *DataPacket {
	return &DataPacket{data: data}
}

func (this *DataPacket) Command() PacketCommand {
	return 0
}

func (this *DataPacket) Message() proto.Message {
	return nil
}

func (this *DataPacket) GetStreamData() []byte {
	return this.data
}

// deep copy
func (this *DataPacket) Clone() Packet {
	newPacket := &DataPacket{data: make([]byte,len(this.data))}
	copy(newPacket.data, this.data)
	return newPacket
}
