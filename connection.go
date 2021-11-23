package gnet

import (
	"google.golang.org/protobuf/proto"
	"net"
	"sync/atomic"
)

// 连接接口定义
type Connection interface {
	// 连接唯一id
	GetConnectionId() uint32

	// 是否是发起连接的一方
	IsConnector() bool

	// 发包(protobuf)
	// NOTE:调用Send(command,message)之后,不要再对message进行读写!
	Send(command PacketCommand, message proto.Message) bool

	// 发包
	// NOTE:调用SendPacket(packet)之后,不要再对packet进行读写!
	SendPacket(packet Packet) bool

	// 是否连接成功
	IsConnected() bool

	// 获取编解码接口
	GetCodec() Codec

	// 设置编解码接口
	SetCodec(codec Codec)

	// LocalAddr returns the local network address.
	LocalAddr() net.Addr

	// RemoteAddr returns the remote network address.
	RemoteAddr() net.Addr

	// 关闭连接
	Close()
}

// 连接设置
type ConnectionConfig struct {
	// 发包缓存chan大小(缓存数据包chan容量)
	SendPacketCacheCap uint32
	// 发包Buffer大小(byte)
	SendBufferSize uint32
	// 收包Buffer大小(byte)
	RecvBufferSize uint32
	// 最大包大小设置(byte)
	MaxPacketSize uint32
	// 收包超时设置(秒)
	RecvTimeout uint32
	// 心跳包发送间隔(秒),对connector有效
	HeartBeatInterval uint32
	// 发包超时设置(秒)
	WriteTimeout uint32
	// TODO:其他流量控制设置
}

// 连接
type baseConnection struct {
	// 连接唯一id
	connectionId uint32
	// 连接设置
	config ConnectionConfig
	// 是否是连接方
	isConnector bool
	// 是否连接成功
	isConnected bool
	// 接口
	handler ConnectionHandler
	// 编解码接口
	codec Codec
}

// 连接唯一id
func (this *baseConnection) GetConnectionId() uint32 {
	return this.connectionId
}

func (this *baseConnection) IsConnector() bool {
	return this.isConnector
}

// 是否连接成功
func (this *baseConnection) IsConnected() bool {
	return this.isConnected
}

// 获取编解码接口
func (this *baseConnection) GetCodec() Codec {
	return this.codec
}

// 设置编解码接口
func (this *baseConnection) SetCodec(codec Codec) {
	this.codec = codec
}

var (
	connectionIdCounter uint32 = 0
)

func newConnectionId() uint32 {
	return atomic.AddUint32(&connectionIdCounter, 1)
}