package gnet

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// TcpConnection use RingBuffer to optimize
type TcpConnection struct {
	baseConnection
	conn net.Conn
	// 读协程结束标记
	// notify chan for read goroutine end
	readStopNotifyChan chan struct{}
	// 防止执行多次关闭操作
	closeOnce sync.Once
	// close callback
	onClose func(connection Connection)
	// 最近收到完整数据包的时间(时间戳:秒)
	lastRecvPacketTick int64
	// RingBuffer for send
	sendBuffer *RingBuffer
	// RingBuffer for recv
	recvBuffer *RingBuffer
	// 解码时,用到的一些临时变量
	tmpReadPacketHeader     PacketHeader
	tmpReadPacketHeaderData []byte
	curReadPacketHeader     PacketHeader
}

func NewTcpConnector(config *ConnectionConfig) *TcpConnection {
	if config.MaxPacketSize == 0 {
		config.MaxPacketSize = MaxPacketDataSize
	}
	if config.MaxPacketSize > MaxPacketDataSize {
		config.MaxPacketSize = MaxPacketDataSize
	}
	newConnection := createTcpConnection(config)
	newConnection.isConnector = true
	return newConnection
}

func NewTcpConnectionAccept(conn net.Conn, config *ConnectionConfig) *TcpConnection {
	if config.MaxPacketSize == 0 {
		config.MaxPacketSize = MaxPacketDataSize
	}
	if config.MaxPacketSize > MaxPacketDataSize {
		config.MaxPacketSize = MaxPacketDataSize
	}
	newConnection := createTcpConnection(config)
	newConnection.isConnector = false
	atomic.StoreInt32(&newConnection.isConnected, 1)
	newConnection.conn = conn
	return newConnection
}

func createTcpConnection(config *ConnectionConfig) *TcpConnection {
	newConnection := &TcpConnection{
		baseConnection: baseConnection{
			connectionId:    NewConnectionId(),
			config:          config,
			codec:           config.Codec,
			handler:         config.Handler,
			sendPacketCache: make(chan Packet, config.SendPacketCacheCap),
			rpcCalls:        newRpcCalls(),
		},
		readStopNotifyChan: make(chan struct{}, 1),
	}
	newConnection.tmpReadPacketHeader = config.Codec.CreatePacketHeader(newConnection, nil, nil)
	return newConnection
}

func (this *TcpConnection) Connect(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		atomic.StoreInt32(&this.isConnected, 0)
		logger.Error("Connect failed %v: %v %v", this.GetConnectionId(), address, err.Error())
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
func (this *TcpConnection) Start(ctx context.Context, netMgrWg *sync.WaitGroup, onClose func(connection Connection)) {
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
func (this *TcpConnection) readLoop() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("readLoop fatal %v: %v", this.GetConnectionId(), err.(error))
			LogStack()
		}
	}()

	logger.Debug("readLoop begin %v", this.GetConnectionId())
	this.recvBuffer = this.createRecvBuffer()
	this.tmpReadPacketHeaderData = make([]byte, this.codec.PacketHeaderSize())
	for this.IsConnected() {
		// 可写入的连续buffer
		writeBuffer := this.recvBuffer.WriteBuffer()
		if len(writeBuffer) == 0 {
			// 不会运行到这里来,除非recvBuffer的大小设置太小:小于了包头的长度
			logger.Error("%v recvBuffer full", this.GetConnectionId())
			return
		}
		n, err := this.conn.Read(writeBuffer)
		if err != nil {
			if err != io.EOF {
				logger.Debug("readLoop %v err:%v", this.GetConnectionId(), err.Error())
			}
			break
		}
		//LogDebug("%v Read:%v", this.GetConnectionId(), n)
		this.recvBuffer.SetWrote(n)
		for this.IsConnected() {
			newPacket, decodeError := this.codec.Decode(this, this.recvBuffer.ReadBuffer())
			if decodeError != nil {
				logger.Error("%v decodeError:%v", this.GetConnectionId(), decodeError.Error())
				return
			}
			if newPacket == nil {
				break
			}
			// 最近收到完整数据包的时间
			// 有一种极端情况,网速太慢,即使没有掉线,也可能触发收包超时检测
			atomic.StoreInt64(&this.lastRecvPacketTick, GetCurrentTimeStamp())
			if this.handler != nil {
				if this.rpcCalls.putReply(newPacket) {
					continue
				}
				this.handler.OnRecvPacket(this, newPacket)
			}
		}
	}
	//logger.Debug("readLoop end %v", this.GetConnectionId())
}

// write goroutine
func (this *TcpConnection) writeLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("writeLoop fatal %v: %v", this.GetConnectionId(), err.(error))
			//LogStack()
		}
		logger.Debug("writeLoop end %v", this.GetConnectionId())
	}()

	logger.Debug("writeLoop begin %v", this.GetConnectionId())
	// 收包超时计时,用于检测掉线
	recvTimeoutTimer := time.NewTimer(time.Second * time.Duration(this.config.RecvTimeout))
	defer recvTimeoutTimer.Stop()
	// 心跳包计时
	heartBeatTimer := time.NewTimer(time.Second * time.Duration(this.config.HeartBeatInterval))
	defer heartBeatTimer.Stop()
	this.sendBuffer = this.createSendBuffer()
	for this.IsConnected() {
		var delaySendDecodePacketData []byte
		select {
		case packet := <-this.sendPacketCache:
			if packet == nil {
				logger.Error("packet==nil %v", this.GetConnectionId())
				return
			}
			hasError := false
			delaySendDecodePacketData, hasError = this.writePacket(packet)
			if hasError {
				return
			}

		case <-recvTimeoutTimer.C:
			if !this.checkRecvTimeout(recvTimeoutTimer) {
				return
			}

		case <-heartBeatTimer.C:
			delaySendDecodePacketData = this.onHeartBeatTimeUp(heartBeatTimer)

		case <-this.readStopNotifyChan:
			logger.Debug("recv readStopNotify %v", this.GetConnectionId())
			return

		case <-ctx.Done():
			// 收到外部的关闭通知
			logger.Debug("recv closeNotify %v", this.GetConnectionId())
			return
		}

		sendErr := this.sendEncodedBuffer(delaySendDecodePacketData)
		if sendErr != nil {
			return
		}
	}
}

// 数据包编码
// Encode里面会把编码后的数据直接写入sendBuffer
func (this *TcpConnection) writePacket(packet Packet) (delaySendDecodePacketData []byte, hasError bool) {
	// 数据包编码
	// Encode里面会把编码后的数据直接写入sendBuffer
	delaySendDecodePacketData = this.codec.Encode(this, packet)
	if len(delaySendDecodePacketData) > 0 {
		// Encode里面写不完的数据延后处理
		logger.Debug("%v sendBuffer is full delaySize:%v", this.GetConnectionId(), len(delaySendDecodePacketData))
		return
	}
	packetCount := len(this.sendPacketCache)
	// 还有其他数据包在chan里,就进行批量合并
	// batch write to RingBuffer
	if packetCount > 0 {
		for i := 0; i < packetCount; i++ {
			// 这里不会阻塞
			// not block here
			newPacket, ok := <-this.sendPacketCache
			if !ok {
				logger.Error("newPacket==nil %v", this.GetConnectionId())
				hasError = true
				return
			}
			// 数据包编码
			delaySendDecodePacketData = this.codec.Encode(this, newPacket)
			if len(delaySendDecodePacketData) > 0 {
				// if the RingBuffer is full, leave the rest packet to process later
				logger.Debug("%v sendBuffer is full delaySize:%v", this.GetConnectionId(), len(delaySendDecodePacketData))
				break
			}
		}
	}
	return
}

func (this *TcpConnection) sendEncodedBuffer(delaySendDecodePacketData []byte) error {
	if this.sendBuffer.UnReadLength() == 0 {
		return nil
	}
	// 可读数据有可能分别存在数组的尾部和头部,所以需要循环发送,有可能需要发送多次
	// the data may be separate at tail and head of the RingBuffer, so we need to send loop
	for this.IsConnected() && this.sendBuffer.UnReadLength() > 0 {
		if this.config.WriteTimeout > 0 {
			setTimeoutErr := this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout) * time.Second))
			// Q:什么情况会导致SetWriteDeadline返回err?
			if setTimeoutErr != nil {
				// ...
				logger.Debug("%v setTimeoutErr:%v", this.GetConnectionId(), setTimeoutErr.Error())
				return setTimeoutErr
			}
		}
		readBuffer := this.sendBuffer.ReadBuffer()
		writeCount, err := this.conn.Write(readBuffer)
		if err != nil {
			logger.Debug("%v write Err:%v", this.GetConnectionId(), err.Error())
			return err
		}
		this.sendBuffer.SetReaded(writeCount)
		if len(delaySendDecodePacketData) > 0 {
			wroteLen, _ := this.sendBuffer.Write(delaySendDecodePacketData)
			// 这里不一定能全部写完
			if wroteLen < len(delaySendDecodePacketData) {
				delaySendDecodePacketData = delaySendDecodePacketData[wroteLen:]
				logger.Debug("%v write delay buffer :%v", this.GetConnectionId(), wroteLen)
			} else {
				delaySendDecodePacketData = nil
			}
		}
	}
	return nil
}

func (this *TcpConnection) checkRecvTimeout(recvTimeoutTimer *time.Timer) bool {
	if this.config.RecvTimeout > 0 {
		nextTimeoutTime := int64(this.config.RecvTimeout) + atomic.LoadInt64(&this.lastRecvPacketTick) - GetCurrentTimeStamp()
		if nextTimeoutTime > 0 {
			if nextTimeoutTime > int64(this.config.RecvTimeout) {
				nextTimeoutTime = int64(this.config.RecvTimeout)
			}
			recvTimeoutTimer.Reset(time.Second * time.Duration(nextTimeoutTime))
		} else {
			// 指定时间内,一直未读取到数据包,则认为该连接掉线了,可能处于"假死"状态了
			// 需要主动关闭该连接,防止连接"泄漏"
			logger.Debug("recv Timeout %v IsConnector %v", this.GetConnectionId(), this.IsConnector())
			return false
		}
	}
	return true
}

func (this *TcpConnection) onHeartBeatTimeUp(heartBeatTimer *time.Timer) (delaySendDecodePacketData []byte) {
	if this.isConnector && this.config.HeartBeatInterval > 0 && this.handler != nil {
		if heartBeatPacket := this.handler.CreateHeartBeatPacket(this); heartBeatPacket != nil {
			delaySendDecodePacketData = this.codec.Encode(this, heartBeatPacket)
			heartBeatTimer.Reset(time.Second * time.Duration(this.config.HeartBeatInterval))
		}
	}
	return
}

func (this *TcpConnection) Close() {
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

// 创建用于批量发包的RingBuffer
func (this *TcpConnection) createSendBuffer() *RingBuffer {
	ringBufferSize := this.config.SendBufferSize
	if ringBufferSize == 0 {
		ringBufferSize = DefaultConnectionConfig.SendBufferSize
	}
	return NewRingBuffer(int(ringBufferSize))
}

// 创建用于批量收包的RingBuffer
func (this *TcpConnection) createRecvBuffer() *RingBuffer {
	ringBufferSize := this.config.RecvBufferSize
	if ringBufferSize == 0 {
		ringBufferSize = DefaultConnectionConfig.RecvBufferSize
	}
	return NewRingBuffer(int(ringBufferSize))
}

//// 发包RingBuffer
//func (this *TcpConnection) GetSendBuffer() *RingBuffer {
//	return this.sendBuffer
//}
//
//// 收包RingBuffer
//func (this *TcpConnection) GetRecvBuffer() *RingBuffer {
//	return this.recvBuffer
//}

// LocalAddr returns the local network address.
func (this *TcpConnection) LocalAddr() net.Addr {
	if this.conn == nil {
		return nil
	}
	return this.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (this *TcpConnection) RemoteAddr() net.Addr {
	if this.conn == nil {
		return nil
	}
	return this.conn.RemoteAddr()
}
