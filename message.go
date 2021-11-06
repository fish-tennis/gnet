package gnet

import (
	"encoding/binary"
	"unsafe"
)

type Message struct {
	//len uint32
	//cmd uint16
	//flags uint8
	data []byte
}

type MessageHeader struct {
	// (flags << 24) | len
	lenAndFlags uint32
}

// [0,0x00FFFFFF]
func (this *MessageHeader) GetLen() uint32 {
	return this.lenAndFlags & 0x00FFFFFF
}

// [0,0xFF]
func (this *MessageHeader) GetFlags() uint32 {
	return this.lenAndFlags >> 24
}

// 从字节流读取数据,len(messageHeaderData)>=MessageHeaderSize
// 使用小端字节序
func (this *MessageHeader) ReadFrom(messageHeaderData []byte) {
	this.lenAndFlags = binary.LittleEndian.Uint32(messageHeaderData)
}

// 写入字节流,使用小端字节序
func (this *MessageHeader) WriteTo(messageHeaderData []byte) {
	binary.LittleEndian.PutUint32(messageHeaderData, this.lenAndFlags)
}

const (
	// 消息头长度
	MessageHeaderSize = unsafe.Sizeof(MessageHeader{})
)

//type ProtoMessage struct {
//	cmd uint16
//	message proto.Message
//}