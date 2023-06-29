package gnet

import (
	"context"
	"google.golang.org/protobuf/proto"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var (
	_connectionIdCounter uint32 = 0
)

// interface for Connection
type Connection interface {
	// unique id
	GetConnectionId() uint32

	// is connector
	IsConnector() bool

	// send a packet(proto.Message)
	//  NOTE: 调用Send(command,message)之后,不要再对message进行读写!
	//  NOTE: do not read or modify message after call Send
	Send(command PacketCommand, message proto.Message) bool

	// send a packet(Packet)
	//  NOTE:调用SendPacket(packet)之后,不要再对packet进行读写!
	//  NOTE: do not read or modify Packet after call SendPacket
	SendPacket(packet Packet) bool

	// 超时发包,超时未发送则丢弃,适用于某些允许丢弃的数据包
	// try send a packet with timeout
	TrySendPacket(packet Packet, timeout time.Duration) bool

	// is connected
	IsConnected() bool

	// codec for this connection
	GetCodec() Codec

	// set codec
	SetCodec(codec Codec)

	// handler for this connection
	GetHandler() ConnectionHandler

	// LocalAddr returns the local network address.
	LocalAddr() net.Addr

	// RemoteAddr returns the remote network address.
	RemoteAddr() net.Addr

	// close this connection
	Close()

	// 获取关联数据
	// get the associated tag
	GetTag() interface{}

	// 设置关联数据
	// set the associated tag
	SetTag(tag interface{})

	// connect to target server
	//  address format ip:port
	Connect(address string) bool

	// 开启读写协程
	// start the read&write goroutine
	Start(ctx context.Context, netMgrWg *sync.WaitGroup, onClose func(connection Connection))
}

// connection options
type ConnectionConfig struct {
	// 发包缓存chan大小(缓存数据包chan容量)
	// capacity for send packet chan
	SendPacketCacheCap uint32

	// 发包Buffer大小(byte)
	// size of send RingBuffer (byte)
	SendBufferSize uint32

	// 收包Buffer大小(byte)
	// size of recv RingBuffer (byte)
	RecvBufferSize uint32

	// 最大包体大小设置(byte),不包含PacketHeader
	// 允许该值大于SendBufferSize和RecvBufferSize
	//  max size of packet (byte), not include PacketHeader's size
	//  allow MaxPacketSize lager than SendBufferSize and RecvBufferSize
	MaxPacketSize uint32

	// 收包超时设置(秒)
	//  if the connection dont recv packet for RecvTimeout seconds,the connection will close
	//  if RecvTimeout is zero,it will not check timeout
	RecvTimeout uint32

	// 心跳包发送间隔(秒),对connector有效
	//  heartbeat packet sending interval(seconds)
	//  only valid for connector
	HeartBeatInterval uint32

	// 发包超时设置(秒)
	//  net.Conn.SetWriteDeadline
	WriteTimeout uint32
}

type baseConnection struct {
	// unique id
	connectionId uint32
	// options
	config *ConnectionConfig
	// is connector
	isConnector bool
	// is connected
	isConnected int32
	// handler
	handler ConnectionHandler
	// 编解码接口
	codec Codec
	// 关联数据
	//  the associated tag
	tag interface{}
}

// unique id
func (this *baseConnection) GetConnectionId() uint32 {
	return this.connectionId
}

func (this *baseConnection) IsConnector() bool {
	return this.isConnector
}

func (this *baseConnection) IsConnected() bool {
	return atomic.LoadInt32(&this.isConnected) > 0
}

func (this *baseConnection) GetCodec() Codec {
	return this.codec
}

func (this *baseConnection) SetCodec(codec Codec) {
	this.codec = codec
}

// 获取关联数据
//  get the associated tag
func (this *baseConnection) GetTag() interface{} {
	return this.tag
}

// 设置关联数据
//  set the associated tag
func (this *baseConnection) SetTag(tag interface{}) {
	this.tag = tag
}

func (this *baseConnection) GetHandler() ConnectionHandler {
	return this.handler
}

func NewConnectionId() uint32 {
	return atomic.AddUint32(&_connectionIdCounter, 1)
}

type ConnectionCreator func(config *ConnectionConfig, codec Codec, handler ConnectionHandler) Connection

type AcceptConnectionCreator func(conn net.Conn, config *ConnectionConfig, codec Codec, handler ConnectionHandler) Connection
