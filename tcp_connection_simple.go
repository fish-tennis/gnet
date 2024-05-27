package gnet

import (
	"context"
	"errors"
	"google.golang.org/protobuf/proto"
	"io"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

// 不使用RingBuffer的TcpConnection
// 需要搭配对应的codec
//
//	TcpConnection without RingBuffer
type TcpConnectionSimple struct {
	baseConnection
	conn net.Conn
	// 读协程结束标记
	// notify chan for read goroutine end
	readStopNotifyChan chan struct{}
	closeOnce          sync.Once
	// close callback
	onClose func(connection Connection)
	// 最近收到完整数据包的时间(时间戳:秒)
	lastRecvPacketTick int64
	// 发包缓存chan
	sendPacketCache chan Packet
}

func NewTcpConnectionSimple(config *ConnectionConfig) *TcpConnectionSimple {
	newConnection := createTcpConnectionSimple(config)
	newConnection.isConnector = true
	return newConnection
}

func NewTcpConnectionSimpleAccept(conn net.Conn, config *ConnectionConfig) *TcpConnectionSimple {
	newConnection := createTcpConnectionSimple(config)
	newConnection.isConnector = false
	atomic.StoreInt32(&newConnection.isConnected, 1)
	newConnection.conn = conn
	return newConnection
}

func createTcpConnectionSimple(config *ConnectionConfig) *TcpConnectionSimple {
	newConnection := &TcpConnectionSimple{
		baseConnection: baseConnection{
			connectionId: NewConnectionId(),
			config:       config,
			codec:        config.Codec,
			handler:      config.Handler,
		},
		readStopNotifyChan: make(chan struct{}, 1),
		sendPacketCache:    make(chan Packet, config.SendPacketCacheCap),
	}
	return newConnection
}

func (this *TcpConnectionSimple) Connect(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		atomic.StoreInt32(&this.isConnected, 0)
		logger.Error("Connect failed %v: %v", this.GetConnectionId(), err.Error())
		if this.handler != nil {
			this.handler.OnConnected(this, false)
		}
		return false
	}
	this.conn = conn
	atomic.StoreInt32(&this.isConnected, 1)
	return true
}

// start read&write goroutine
func (this *TcpConnectionSimple) Start(ctx context.Context, netMgrWg *sync.WaitGroup, onClose func(connection Connection)) {
	this.onClose = onClose
	// 开启收包协程
	netMgrWg.Add(1)
	go func() {
		defer func() {
			netMgrWg.Done()
			if err := recover(); err != nil {
				logger.Error("read fatal %v: %v", this.GetConnectionId(), err.(error))
				LogStack()
			}
		}()
		this.readLoop()
		this.Close()
		// 读协程结束了,通知写协程也结束
		// when read goroutine end, notify write goroutine to exit
		this.readStopNotifyChan <- struct{}{}
	}()

	// 开启发包协程
	netMgrWg.Add(1)
	go func(ctx context.Context) {
		defer func() {
			netMgrWg.Done()
			if err := recover(); err != nil {
				logger.Error("write fatal %v: %v", this.GetConnectionId(), err.(error))
				LogStack()
			}
		}()
		this.writeLoop(ctx)
		this.Close()
	}(ctx)

	if this.handler != nil {
		this.handler.OnConnected(this, true)
	}
}

// read goroutine
func (this *TcpConnectionSimple) readLoop() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("readLoop fatal %v: %v", this.GetConnectionId(), err.(error))
			LogStack()
		}
	}()

	logger.Debug("readLoop begin %v isConnector:%v", this.GetConnectionId(), this.IsConnector())
	for this.IsConnected() {
		// 先读取消息头
		// read packet header first
		messageHeaderData := make([]byte, this.codec.PacketHeaderSize())
		readHeaderSize, err := io.ReadFull(this.conn, messageHeaderData)
		if err != nil {
			if err != io.EOF {
				logger.Debug("readLoop %v err:%v", this.GetConnectionId(), err.Error())
			}
			break
		}
		if readHeaderSize != len(messageHeaderData) {
			break
		}
		newPacketHeader := this.codec.CreatePacketHeader(this, nil, nil)
		newPacketHeader.ReadFrom(messageHeaderData)
		packetDataLen := int(newPacketHeader.Len())
		fullPacketData := make([]byte, len(messageHeaderData)+packetDataLen)
		copy(fullPacketData, messageHeaderData)
		if packetDataLen > 0 {
			// 读取消息体
			// read packet body
			readDataSize, err := io.ReadFull(this.conn, fullPacketData[readHeaderSize:])
			if err != nil {
				if err != io.EOF {
					logger.Debug("readLoop %v err:%v", this.GetConnectionId(), err.Error())
				}
				break
			}
			if readDataSize != packetDataLen {
				break
			}
		}
		newPacket, decodeError := this.codec.Decode(this, fullPacketData)
		if decodeError != nil {
			logger.Error("%v decodeError:%v", this.GetConnectionId(), decodeError.Error())
			return
		}
		if newPacket == nil {
			break
		}
		// 最近收到完整数据包的时间
		this.lastRecvPacketTick = GetCurrentTimeStamp()
		if this.handler != nil {
			this.handler.OnRecvPacket(this, newPacket)
		}
	}
	//logger.Debug("readLoop end %v IsConnector:%v", this.GetConnectionId(), this.IsConnector())
}

// write goroutine
func (this *TcpConnectionSimple) writeLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("writeLoop fatal %v: %v", this.GetConnectionId(), err.(error))
			LogStack()
		}
		logger.Debug("writeLoop end %v IsConnector:%v", this.GetConnectionId(), this.IsConnector())
	}()

	logger.Debug("writeLoop begin %v isConnector:%v", this.GetConnectionId(), this.IsConnector())
	// 收包超时计时,用于检测掉线
	recvTimeoutTimer := time.NewTimer(time.Second * time.Duration(this.config.RecvTimeout))
	defer recvTimeoutTimer.Stop()
	// 心跳包计时
	heartBeatTimer := time.NewTimer(time.Second * time.Duration(this.config.HeartBeatInterval))
	defer heartBeatTimer.Stop()
	for this.IsConnected() {
		select {
		case packet := <-this.sendPacketCache:
			if packet == nil {
				logger.Error("packet==nil %v", this.GetConnectionId())
				return
			}
			if !this.writePacket(packet) {
				return
			}

		case <-recvTimeoutTimer.C:
			if !this.checkRecvTimeout(recvTimeoutTimer) {
				return
			}

		case <-heartBeatTimer.C:
			if !this.onHeartBeatTimeUp(heartBeatTimer) {
				return
			}

		case <-this.readStopNotifyChan:
			logger.Debug("recv readStopNotify %v", this.GetConnectionId())
			return

		case <-ctx.Done():
			// 收到外部的关闭通知
			logger.Debug("recv closeNotify %v", this.GetConnectionId())
			return
		}
	}
	//logger.Debug("writeLoop end %v isConnector:%v", this.GetConnectionId(), this.IsConnector())
}

func (this *TcpConnectionSimple) writePacket(packet Packet) bool {
	// 这里编码的是包体,不包含包头
	packetData := this.codec.Encode(this, packet)
	// 包头数据
	newPacketHeader := this.codec.CreatePacketHeader(this, packet, packetData)
	packetHeaderData := make([]byte, this.codec.PacketHeaderSize())
	newPacketHeader.WriteTo(packetHeaderData)
	writeCount := 0
	// 先发送包头数据
	for writeCount < len(packetHeaderData) {
		if this.config.WriteTimeout > 0 {
			setTimeoutErr := this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout) * time.Second))
			// Q:什么情况会导致SetWriteDeadline返回err?
			if setTimeoutErr != nil {
				// ...
				logger.Debug("%v setTimeoutErr:%v", this.GetConnectionId(), setTimeoutErr.Error())
				return false
			}
		}
		n, err := this.conn.Write(packetHeaderData[writeCount:])
		if err != nil {
			logger.Error("%v send error:%v", this.GetConnectionId(), err.Error())
			return false
		}
		writeCount += n
	}

	writeCount = 0
	// 再发送包体数据
	for writeCount < len(packetData) {
		if this.config.WriteTimeout > 0 {
			setTimeoutErr := this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout) * time.Second))
			// Q:什么情况会导致SetWriteDeadline返回err?
			if setTimeoutErr != nil {
				// ...
				logger.Debug("%v setTimeoutErr:%v", this.GetConnectionId(), setTimeoutErr.Error())
				return false
			}
		}
		n, err := this.conn.Write(packetData[writeCount:])
		if err != nil {
			logger.Error("%v send error:%v", this.GetConnectionId(), err.Error())
			return false
		}
		writeCount += n
	}
	return true
}

func (this *TcpConnectionSimple) checkRecvTimeout(recvTimeoutTimer *time.Timer) bool {
	if this.config.RecvTimeout > 0 {
		nextTimeoutTime := int64(this.config.RecvTimeout) + this.lastRecvPacketTick - GetCurrentTimeStamp()
		if nextTimeoutTime > 0 {
			if nextTimeoutTime > int64(this.config.RecvTimeout) {
				nextTimeoutTime = int64(this.config.RecvTimeout)
			}
			recvTimeoutTimer.Reset(time.Second * time.Duration(nextTimeoutTime))
		} else {
			// 指定时间内,一直未读取到数据包,则认为该连接掉线了,可能处于"假死"状态了
			// 需要主动关闭该连接,防止连接"泄漏"
			logger.Debug("recv timeout %v", this.GetConnectionId())
			return false
		}
	}
	return true
}

func (this *TcpConnectionSimple) onHeartBeatTimeUp(heartBeatTimer *time.Timer) bool {
	if this.isConnector && this.config.HeartBeatInterval > 0 && this.handler != nil {
		if heartBeatPacket := this.handler.CreateHeartBeatPacket(this); heartBeatPacket != nil {
			if !this.writePacket(heartBeatPacket) {
				return false
			}
			heartBeatTimer.Reset(time.Second * time.Duration(this.config.HeartBeatInterval))
		}
	}
	return true
}

func (this *TcpConnectionSimple) Close() {
	this.closeOnce.Do(func() {
		atomic.StoreInt32(&this.isConnected, 0)
		if this.conn != nil {
			this.conn.Close()
			//this.conn = nil
		}
		if this.handler != nil {
			this.handler.OnDisconnected(this)
		}
		if this.onClose != nil {
			this.onClose(this)
		}
	})
}

// 异步发送proto包
// NOTE:调用Send(command,message)之后,不要再对message进行读写!
//
//	asynchronous send (write to chan, not send immediately)
func (this *TcpConnectionSimple) Send(command PacketCommand, message proto.Message) bool {
	if !this.IsConnected() {
		return false
	}
	packet := NewProtoPacket(command, message)
	// NOTE:当sendPacketCache满时,这里会阻塞
	this.sendPacketCache <- packet
	return true
}

// 异步发送数据
// NOTE:调用SendPacket(packet)之后,不要再对packet进行读写!
//
//	asynchronous send (write to chan, not send immediately)
func (this *TcpConnectionSimple) SendPacket(packet Packet) bool {
	if !this.IsConnected() {
		return false
	}
	// NOTE:当sendPacketCache满时,这里会阻塞
	this.sendPacketCache <- packet
	return true
}

// 超时发包,超时未发送则丢弃,适用于某些允许丢弃的数据包
// 可以防止某些"不重要的"数据包造成chan阻塞,比如游戏项目常见的聊天广播
//
//	asynchronous send with timeout (write to chan, not send immediately)
//	if return false, means not write to chan
func (this *TcpConnectionSimple) TrySendPacket(packet Packet, timeout time.Duration) bool {
	if timeout == 0 {
		// 非阻塞方式写chan
		select {
		case this.sendPacketCache <- packet:
			return true
		default:
			return false
		}
	}
	sendTimeout := time.After(timeout)
	for {
		select {
		case this.sendPacketCache <- packet:
			return true
		case <-sendTimeout:
			return false
		}
	}
	return false
}

func (this *TcpConnectionSimple) Rpc(request Packet, reply proto.Message) error {
	if !this.IsConnected() {
		return errors.New("disconnected")
	}
	call := this.rpcCalls.newRpcCall()
	if rpcCallIdSetter, ok := request.(RpcCallIdSetter); ok {
		rpcCallIdSetter.SetRpcCallId(call.id)
	} else {
		return errors.New("request must be RpcCallIdSetter")
	}
	// NOTE:当sendPacketCache满时,这里会阻塞
	this.sendPacketCache <- request
	timeout := time.After(time.Second * 3)
	select {
	case <-timeout:
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

// LocalAddr returns the local network address.
func (this *TcpConnectionSimple) LocalAddr() net.Addr {
	if this.conn == nil {
		return nil
	}
	return this.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (this *TcpConnectionSimple) RemoteAddr() net.Addr {
	if this.conn == nil {
		return nil
	}
	return this.conn.RemoteAddr()
}

func (this *TcpConnectionSimple) GetSendPacketChanLen() int {
	return len(this.sendPacketCache)
}
