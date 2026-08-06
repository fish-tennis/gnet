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
		SendPacketCacheCap: 256,
		SendBufferSize:     4096, // 4K
		RecvBufferSize:     4096, // 4K
		MaxPacketSize:      MaxPacketDataSize,
		RecvTimeout:        20, // 20s
		WriteTimeout:       10, // 10s
		HeartBeatInterval:  5,  // 5s
		InsecureSkipVerify: true,
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
	//  try send a packet with Timeout
	TrySendPacket(packet Packet, timeout time.Duration, opts ...SendOption) bool

	// Rpc 发送RPC请求并阻塞等待回复
	//
	// opts控制写入sendPacketCache的行为,与SendPacket的opts语义完全一致:
	//   - Timeout: 写入sendPacketCache的超时(默认DefaultSendTimeout)
	//   - WithInfiniteTimeout: 无限等待
	//   - WithDiscard: 非阻塞写入
	//
	// 等待回复的超时固定为DefaultRpcTimeout,如需自定义请使用RpcTimeout
	Rpc(request Packet, reply proto.Message, opts ...SendOption) error

	// RpcTimeout 与Rpc功能相同,但额外支持自定义等待回复的超时时间
	//
	// replyTimeout: 等待回复的超时时间,<=0表示使用DefaultRpcTimeout
	// opts: 控制写入sendPacketCache的行为,与Rpc完全一致
	RpcTimeout(request Packet, reply proto.Message, replyTimeout time.Duration, opts ...SendOption) error

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
	//  if RecvTimeout is zero,it will not check Timeout
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

	// wss连接时是否跳过证书验证(默认跳过,兼容自签名证书场景)
	//  whether to skip certificate verification for wss connections
	InsecureSkipVerify bool
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
	// NOTE: codec只在初始化时设置,运行时不修改,无需加锁
	codec Codec
	// 关联数据
	//  the associated tag
	tag interface{}
	// 保护tag并发读写
	tagLock sync.RWMutex

	// 发包缓存chan
	sendPacketCache chan Packet // TODO: chan sendPacket
	// notify chan for writeLoop goroutine end
	writeStopNotifyChan chan struct{}

	rpcCalls *rpcCalls
}

// unique id
func (c *baseConnection) GetConnectionId() uint32 {
	return c.connectionId
}

func (c *baseConnection) IsConnector() bool {
	return c.isConnector
}

func (c *baseConnection) IsConnected() bool {
	return atomic.LoadInt32(&c.isConnected) > 0
}

func (c *baseConnection) GetCodec() Codec {
	return c.codec
}

// SetCodec 仅在初始化阶段(连接Start之前)调用,运行时调用会导致readLoop/writeLoop编解码不一致
func (c *baseConnection) SetCodec(codec Codec) {
	c.codec = codec
}

// 获取关联数据
//
//	get the associated tag
func (c *baseConnection) GetTag() interface{} {
	c.tagLock.RLock()
	defer c.tagLock.RUnlock()
	return c.tag
}

// 设置关联数据
//
//	set the associated tag
func (c *baseConnection) SetTag(tag interface{}) {
	c.tagLock.Lock()
	defer c.tagLock.Unlock()
	c.tag = tag
}

func (c *baseConnection) GetHandler() ConnectionHandler {
	return c.handler
}

// 发送proto包
//
//	NOTE:如果是异步调用Send(command,message),调用之后,不要再对message进行读写!
func (c *baseConnection) Send(command PacketCommand, message proto.Message, opts ...SendOption) bool {
	packet := NewProtoPacket(command, message)
	return c.SendPacket(packet, opts...)
}

// 发送数据
//
//	NOTE:如果是异步调用SendPacket(command,message),调用之后,不要再对message进行读写!
func (c *baseConnection) SendPacket(packet Packet, opts ...SendOption) (ret bool) {
	if !c.IsConnected() {
		return false
	}
	defer func() {
		// close(sendPacketCache)后,再执行sendPacketCache <- packet,会panic
		if err := recover(); err != nil {
			ret = false
			if c.IsConnected() {
				logger.Error("SendPacket fatal %v: %v", c.GetConnectionId(), err)
			}
		}
	}()
	// 使用值类型避免堆分配
	sendOpts := sendOptions{timeout: DefaultSendTimeout}
	for _, opt := range opts {
		opt.apply(&sendOpts)
	}
	if sendOpts.timeout > 0 {
		sendTimer := time.NewTimer(sendOpts.timeout)
		defer sendTimer.Stop()
		for {
			select {
			case c.sendPacketCache <- packet:
				return true
			case <-sendTimer.C:
				return false
			case <-c.writeStopNotifyChan:
				return false
			}
		}
	} else {
		if sendOpts.discard {
			// 非阻塞方式写chan
			select {
			case c.sendPacketCache <- packet:
				return true
			case <-c.writeStopNotifyChan:
				return false
			default:
				return false
			}
		} else {
			select {
			// NOTE:当sendPacketCache满时,这里会阻塞
			case c.sendPacketCache <- packet:
				return true
			case <-c.writeStopNotifyChan:
				return false
			}
		}
	}
}

// 超时发包,超时未发送则丢弃,适用于某些允许丢弃的数据包
// 可以防止某些"不重要的"数据包造成chan阻塞,比如游戏项目常见的聊天广播
//
//	asynchronous send with Timeout (write to chan, not send immediately)
//	if return false, means not write to chan
func (c *baseConnection) TrySendPacket(packet Packet, timeout time.Duration, opts ...SendOption) bool {
	sendOpts := opts
	if timeout == 0 {
		sendOpts = append(sendOpts, WithDiscard())
	} else {
		sendOpts = append(sendOpts, Timeout(timeout))
	}
	return c.SendPacket(packet, sendOpts...)
}

// Rpc send a request to target and block wait reply
// Rpc 发送RPC请求并阻塞等待回复
//
// opts控制写入sendPacketCache的行为,与SendPacket的opts语义完全一致:
//   - Timeout: 写入sendPacketCache的超时(默认DefaultSendTimeout)
//   - WithInfiniteTimeout: 无限等待
//   - WithDiscard: 非阻塞写入
//
// 等待回复的超时固定为DefaultRpcTimeout,如需自定义请使用RpcTimeout
func (c *baseConnection) Rpc(request Packet, reply proto.Message, opts ...SendOption) (rpcErr error) {
	if !c.IsConnected() {
		return errors.New("disconnected")
	}
	call := c.rpcCalls.newRpcCall()
	defer func() {
		if err := recover(); err != nil {
			// panic时清理rpcCall,防止map泄漏
			c.rpcCalls.removeReply(call.id)
			rpcErr = errors.New("rpc panic")
			if c.IsConnected() {
				logger.Error("Rpc fatal %v: %v", c.GetConnectionId(), err)
			}
		}
	}()
	request.SetRpcCallId(call.id)
	// 使用SendPacket写入sendPacketCache,完整复用opts的语义
	if !c.SendPacket(request, opts...) {
		c.rpcCalls.removeReply(call.id)
		return errors.New("send failed")
	}
	// 等待回复,使用DefaultRpcTimeout
	return c.waitRpcReply(call, reply, DefaultRpcTimeout)
}

// RpcTimeout 与Rpc功能相同,但额外支持自定义等待回复的超时时间
//
// replyTimeout: 等待回复的超时时间,<=0表示使用DefaultRpcTimeout
// opts: 控制写入sendPacketCache的行为,与Rpc完全一致
func (c *baseConnection) RpcTimeout(request Packet, reply proto.Message, replyTimeout time.Duration, opts ...SendOption) (rpcErr error) {
	if !c.IsConnected() {
		return errors.New("disconnected")
	}
	call := c.rpcCalls.newRpcCall()
	defer func() {
		if err := recover(); err != nil {
			// panic时清理rpcCall,防止map泄漏
			c.rpcCalls.removeReply(call.id)
			rpcErr = errors.New("rpc panic")
			if c.IsConnected() {
				logger.Error("RpcTimeout fatal %v: %v", c.GetConnectionId(), err)
			}
		}
	}()
	request.SetRpcCallId(call.id)
	// 使用SendPacket写入sendPacketCache,完整复用opts的语义
	if !c.SendPacket(request, opts...) {
		c.rpcCalls.removeReply(call.id)
		return errors.New("send failed")
	}
	// 等待回复,使用自定义的replyTimeout
	if replyTimeout <= 0 {
		replyTimeout = DefaultRpcTimeout
	}
	return c.waitRpcReply(call, reply, replyTimeout)
}

// waitRpcReply 阻塞等待RPC回复
func (c *baseConnection) waitRpcReply(call *rpcCall, reply proto.Message, replyTimeout time.Duration) error {
	rpcTimer := time.NewTimer(replyTimeout)
	defer rpcTimer.Stop()
	select {
	case <-rpcTimer.C:
		c.rpcCalls.removeReply(call.id)
		return errors.New("timeout")
	case <-c.writeStopNotifyChan:
		c.rpcCalls.removeReply(call.id)
		return errors.New("connection closed")
	case replyPacket := <-call.reply:
		if replyPacket == nil {
			return errors.New("reply is nil")
		}
		// 如果网络层已经反序列化了,直接赋值
		if replyPacket.Message() != nil {
			valueReply := reflect.ValueOf(reply)
			if valueReply.Kind() != reflect.Ptr {
				return errors.New("reply is not a ptr")
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

func (c *baseConnection) GetSendPacketChanLen() int {
	return len(c.sendPacketCache)
}

func NewConnectionId() uint32 {
	return atomic.AddUint32(&_connectionIdCounter, 1)
}

type ConnectionCreator func(config *ConnectionConfig) Connection

type AcceptConnectionCreator func(conn net.Conn, config *ConnectionConfig) Connection

// safeCall 安全执行回调,防止panic影响后续逻辑(如channel关闭)
func safeCall(f func()) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("safeCall panic: %v", err)
		}
	}()
	f()
}
