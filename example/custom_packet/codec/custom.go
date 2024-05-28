package codec

import (
	"encoding/binary"
	"github.com/fish-tennis/gnet"
	"google.golang.org/protobuf/proto"
	"unsafe"
)

// 自定义包头
// implement of PacketHeader
type CustomPacketHeader struct {
	len     uint32 // 支持更大的Packet
	command uint16 // 消息号
	flags   uint16 // 预留标记
}

// 包体长度,不包含包头的长度
// [0,0xFFFFFFFF]
func (this *CustomPacketHeader) Len() uint32 {
	return this.len
}

// 消息号
func (this *CustomPacketHeader) Command() uint16 {
	return this.command
}

// 标记
func (this *CustomPacketHeader) Flags() uint16 {
	return this.flags
}

// 从字节流读取数据,len(messageHeaderData)>=MessageHeaderSize
// 使用小端字节序
func (this *CustomPacketHeader) ReadFrom(packetHeaderData []byte) {
	this.len = binary.LittleEndian.Uint32(packetHeaderData)
	this.command = binary.LittleEndian.Uint16(packetHeaderData[4:])
	this.flags = binary.LittleEndian.Uint16(packetHeaderData[6:])
}

// 写入字节流,使用小端字节序
func (this *CustomPacketHeader) WriteTo(packetHeaderData []byte) {
	binary.LittleEndian.PutUint32(packetHeaderData, this.len)
	binary.LittleEndian.PutUint16(packetHeaderData[4:], this.command)
	binary.LittleEndian.PutUint16(packetHeaderData[6:], this.flags)
}

// 包含一个消息号和[]byte的数据包
type CustomDataPacket struct {
	command uint16
	data    []byte
}

func NewCustomDataPacket(command uint16, data []byte) *CustomDataPacket {
	return &CustomDataPacket{
		command: command,
		data:    data,
	}
}

func (this *CustomDataPacket) Command() gnet.PacketCommand {
	return gnet.PacketCommand(this.command)
}

func (this *CustomDataPacket) Message() proto.Message {
	return nil
}

func (this *CustomDataPacket) GetStreamData() []byte {
	return this.data
}

// deep copy
func (this *CustomDataPacket) Clone() gnet.Packet {
	newPacket := &CustomDataPacket{data: make([]byte, len(this.data))}
	newPacket.command = this.command
	copy(newPacket.data, this.data)
	return newPacket
}

// 自定义编解码
type CustomCodec struct {
}

// 使用CustomPacketHeader
func (this *CustomCodec) CreatePacketHeader(connection gnet.Connection, packet gnet.Packet, packetData []byte) gnet.PacketHeader {
	if packet == nil {
		return &CustomPacketHeader{
			len: uint32(len(packetData)),
		}
	}
	return &CustomPacketHeader{
		len:     uint32(len(packetData)),
		command: uint16(packet.Command()),
	}
}

func (this *CustomCodec) PacketHeaderSize() uint32 {
	return uint32(int(unsafe.Sizeof(CustomPacketHeader{})))
}

// 这里直接返回原包的字节流数据
// 实际业务可以在此进行编码,如加密,压缩等
func (this *CustomCodec) Encode(connection gnet.Connection, packet gnet.Packet) []byte {
	return packet.GetStreamData()
}

// 这里的data是完整的包数据,包含了包头
func (this *CustomCodec) Decode(connection gnet.Connection, data []byte) (newPacket gnet.Packet, err error) {
	packetHeader := &CustomPacketHeader{}
	packetHeader.ReadFrom(data[0:])
	newPacket = &CustomDataPacket{
		command: packetHeader.Command(),
		data:    data[this.PacketHeaderSize():],
	}
	return
}
