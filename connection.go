package gnet

import (
	"context"
	"errors"
	"google.golang.org/protobuf/proto"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

var (
	_connectionIdCounter uint32 = 0

	DefaultConnectionConfig = ConnectionConfig{
		SendPacketCacheCap: 16,
		SendBufferSize:     4096,
		RecvBufferSize:     4096,
		MaxPacketSize:      MaxPacketDataSize,
		RecvTimeout:        7,
		HeartBeatInterval:  3,
	}
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
	Send(command PacketCommand, message proto.Message, opts ...SendOption) bool

	// send a packet(Packet)
	//  NOTE:调用SendPacket(packet)之后,不要再对packet进行读写!
	//  NOTE: do not read or modify Packet after call SendPacket
	SendPacket(packet Packet, opts ...SendOption) bool

	// 超时发包,超时未发送则丢弃,适用于某些允许丢弃的数据包
	//  try send a packet with timeout
	TrySendPacket(packet Packet, timeout time.Duration, opts ...SendOption) bool

	// Rpc send a request to target and block wait reply
	Rpc(request Packet, reply proto.Message, opts ...SendOption) error

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

	Codec Codec

	Handler ConnectionHandler

	// ws或wss的http路径,如"/ws"或"/wss"
	Path string

	// "ws"或"wss"
	Scheme string
}

// TODO: support block send mode?
type sendPacket struct {
	packet  Packet
	onSendC chan struct{}
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

	// 发包缓存chan
	sendPacketCache chan Packet // TODO: chan sendPacket

	rpcCalls *rpcCalls
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
//
//	get the associated tag
func (this *baseConnection) GetTag() interface{} {
	return this.tag
}

// 设置关联数据
//
//	set the associated tag
func (this *baseConnection) SetTag(tag interface{}) {
	this.tag = tag
}

func (this *baseConnection) GetHandler() ConnectionHandler {
	return this.handler
}

// 发送proto包
//
//	NOTE:如果是异步调用Send(command,message),调用之后,不要再对message进行读写!
func (this *baseConnection) Send(command PacketCommand, message proto.Message, opts ...SendOption) bool {
	packet := NewProtoPacket(command, message)
	return this.SendPacket(packet, opts...)
}

// 发送数据
//
//	NOTE:如果是异步调用SendPacket(command,message),调用之后,不要再对message进行读写!
func (this *baseConnection) SendPacket(packet Packet, opts ...SendOption) bool {
	if !this.IsConnected() {
		return false
	}
	sendOpts := defaultSendOptions
	for _, opt := range opts {
		opt.apply(&sendOpts)
	}
	// TODO: block mode
	if sendOpts.timeout > 0 {
		sendTimeout := time.After(sendOpts.timeout)
		for {
			select {
			case this.sendPacketCache <- packet:
				return true
			case <-sendTimeout:
				return false
			}
		}
	} else {
		if sendOpts.discard {
			// 非阻塞方式写chan
			select {
			case this.sendPacketCache <- packet:
				return true
			default:
				return false
			}
		} else {
			// NOTE:当sendPacketCache满时,这里会阻塞
			this.sendPacketCache <- packet
		}
	}
	return true
}

// 超时发包,超时未发送则丢弃,适用于某些允许丢弃的数据包
// 可以防止某些"不重要的"数据包造成chan阻塞,比如游戏项目常见的聊天广播
//
//	asynchronous send with timeout (write to chan, not send immediately)
//	if return false, means not write to chan
func (this *baseConnection) TrySendPacket(packet Packet, timeout time.Duration, opts ...SendOption) bool {
	sendOpts := opts
	if timeout == 0 {
		sendOpts = append(sendOpts, Discard())
	} else {
		sendOpts = append(sendOpts, Timeout(timeout))
	}
	return this.SendPacket(packet, sendOpts...)
}

// Rpc send a request to target and block wait reply
func (this *baseConnection) Rpc(request Packet, reply proto.Message, opts ...SendOption) error {
	if !this.IsConnected() {
		return errors.New("disconnected")
	}
	sendOpts := defaultSendOptions
	for _, opt := range opts {
		opt.apply(&sendOpts)
	}
	if sendOpts.timeout == 0 {
		sendOpts.timeout = DefaultRpcTimeout
	}
	call := this.rpcCalls.newRpcCall()
	if rpcCallIdSetter, ok := request.(RpcCallIdSetter); ok {
		rpcCallIdSetter.SetRpcCallId(call.id)
	} else {
		return errors.New("request must be RpcCallIdSetter")
	}
	// NOTE:当sendPacketCache满时,这里会阻塞
	this.sendPacketCache <- request
	sendTimeout := sendOpts.timeout
	if sendTimeout < 0 {
		sendTimeout = time.Hour * 24 * 365
	}
	timeout := time.After(sendTimeout)
	select {
	case <-timeout:
		this.rpcCalls.removeReply(call.id)
		return errors.New("timeout")
	case replyPacket := <-call.reply:
		// 如果网络层已经反序列化了,直接赋值
		if replyPacket.Message() != nil {
			valueReply := reflect.ValueOf(reply)
			if valueReply.Kind() != reflect.Ptr {
				return errors.New("request is not a ptr")
			}
			dstMsg, srcMsg := reply.ProtoReflect(), replyPacket.Message().ProtoReflect()
			if dstMsg.Descriptor() != srcMsg.Descriptor() {
				return errors.New("proto message type err")
			}
			valueReply.Elem().Set(reflect.ValueOf(replyPacket.Message()).Elem())
			return nil
		}
		// 否则,反序列化
		err := proto.Unmarshal(replyPacket.GetStreamData(), reply)
		if err != nil {
			return err
		}
		return nil
	}
}

func (this *baseConnection) GetSendPacketChanLen() int {
	return len(this.sendPacketCache)
}

func NewConnectionId() uint32 {
	return atomic.AddUint32(&_connectionIdCounter, 1)
}

type ConnectionCreator func(config *ConnectionConfig) Connection

type AcceptConnectionCreator func(conn net.Conn, config *ConnectionConfig) Connection
