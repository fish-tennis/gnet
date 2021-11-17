package gnet

import (
	"encoding/binary"
	"unsafe"
)

// 数据包
type Packet struct {
	data []byte
}

func NewPacket(data []byte) *Packet {
	return &Packet{data: data}
}

func (this *Packet) GetData() []byte {
	return this.data
}

// 包头
type PacketHeader struct {
	// (flags << 24) | len
	lenAndFlags uint32
}

func NewPacketHeader(len uint32,flags uint8) *PacketHeader {
	return &PacketHeader{
		lenAndFlags: uint32(flags)<<24 | len,
	}
}

// 包体长度,不包含包头的长度
// [0,0x00FFFFFF]
func (this *PacketHeader) GetLen() uint32 {
	return this.lenAndFlags & 0x00FFFFFF
}

// 标志 [0,0xFF]
func (this *PacketHeader) GetFlags() uint32 {
	return this.lenAndFlags >> 24
}

// 从字节流读取数据,len(messageHeaderData)>=MessageHeaderSize
// 使用小端字节序
func (this *PacketHeader) ReadFrom(messageHeaderData []byte) {
	this.lenAndFlags = binary.LittleEndian.Uint32(messageHeaderData)
}

// 写入字节流,使用小端字节序
func (this *PacketHeader) WriteTo(messageHeaderData []byte) {
	binary.LittleEndian.PutUint32(messageHeaderData, this.lenAndFlags)
}

const (
	// 消息头长度
	PacketHeaderSize = int(unsafe.Sizeof(PacketHeader{}))
	MaxPacketDataSize = 0x00FFFFFF
)
