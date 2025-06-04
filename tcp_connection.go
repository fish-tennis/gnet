package gnet

import (
	"context"
	"errors"
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
	// notify chan for readLoop goroutine end
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
			connectionId:        NewConnectionId(),
			config:              config,
			codec:               config.Codec,
			handler:             config.Handler,
			sendPacketCache:     make(chan Packet, config.SendPacketCacheCap),
			writeStopNotifyChan: make(chan struct{}),
			rpcCalls:            newRpcCalls(),
		},
		readStopNotifyChan: make(chan struct{}, 1),
	}
	newConnection.tmpReadPacketHeader = config.Codec.CreatePacketHeader(newConnection, nil, nil)
	return newConnection
}

func (c *TcpConnection) Connect(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		atomic.StoreInt32(&c.isConnected, 0)
		logger.Error("Connect failed %v: %v %v", c.GetConnectionId(), address, err.Error())
		if c.handler != nil {
			c.handler.OnConnected(c, false)
		}
		return false
	}
	c.conn = conn
	atomic.StoreInt32(&c.isConnected, 1)
	return true
}

// start read&write goroutine
func (c *TcpConnection) Start(ctx context.Context, netMgrWg *sync.WaitGroup, onClose func(connection Connection)) {
	c.onClose = onClose
	// 开启收包协程
	netMgrWg.Add(1)
	go func() {
		defer func() {
			netMgrWg.Done()
			if err := recover(); err != nil {
				logger.Error("read fatal %v: %v", c.GetConnectionId(), err.(error))
				LogStack()
			}
		}()
		c.readLoop()
		c.Close()
		// 读协程结束了,通知写协程也结束
		// when read goroutine end, notify write goroutine to exit
		c.readStopNotifyChan <- struct{}{}
	}()

	// 开启发包协程
	netMgrWg.Add(1)
	go func(ctx context.Context) {
		defer func() {
			netMgrWg.Done()
			if err := recover(); err != nil {
				logger.Error("write fatal %v: %v", c.GetConnectionId(), err.(error))
				LogStack()
			}
		}()
		c.writeLoop(ctx)
		c.Close()
		// 写协程结束了,通知阻塞中的SendPacket结束
		close(c.writeStopNotifyChan)
	}(ctx)

	if c.handler != nil {
		c.handler.OnConnected(c, true)
	}
}

// read goroutine
func (c *TcpConnection) readLoop() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("readLoop fatal %v: %v", c.GetConnectionId(), err.(error))
			LogStack()
		}
	}()

	logger.Debug("readLoop begin %v", c.GetConnectionId())
	c.recvBuffer = c.createRecvBuffer()
	c.tmpReadPacketHeaderData = make([]byte, c.codec.PacketHeaderSize())
	for c.IsConnected() {
		// 可写入的连续buffer
		writeBuffer := c.recvBuffer.WriteBuffer()
		if len(writeBuffer) == 0 {
			// 不会运行到这里来,除非recvBuffer的大小设置太小:小于了包头的长度
			logger.Error("%v recvBuffer full", c.GetConnectionId())
			return
		}
		n, err := c.conn.Read(writeBuffer)
		if err != nil {
			if err != io.EOF {
				logger.Debug("readLoop %v err:%v", c.GetConnectionId(), err.Error())
			}
			break
		}
		//LogDebug("%v Read:%v", c.GetConnectionId(), n)
		c.recvBuffer.SetWrote(n)
		for c.IsConnected() {
			newPacket, decodeError := c.codec.Decode(c, c.recvBuffer.ReadBuffer())
			if errors.Is(decodeError, ErrPacketDataNotRead) {
				// recvBuffer中的未读取数据还不够组成一个完整的包
				break
			}
			if decodeError != nil {
				logger.Error("%v decodeError:%v", c.GetConnectionId(), decodeError.Error())
				return
			}
			// 最近收到完整数据包的时间
			// 有一种极端情况,网速太慢,即使没有掉线,也可能触发收包超时检测
			atomic.StoreInt64(&c.lastRecvPacketTick, GetCurrentTimeStamp())
			if c.handler != nil && newPacket != nil {
				if c.rpcCalls.putReply(newPacket) {
					continue
				}
				c.handler.OnRecvPacket(c, newPacket)
			}
		}
	}
	//logger.Debug("readLoop end %v", c.GetConnectionId())
}

// write goroutine
func (c *TcpConnection) writeLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("writeLoop fatal %v: %v", c.GetConnectionId(), err.(error))
			//LogStack()
		}
		logger.Debug("writeLoop end %v", c.GetConnectionId())
	}()

	logger.Debug("writeLoop begin %v", c.GetConnectionId())
	// 收包超时计时,用于检测掉线
	recvTimeoutTimer := time.NewTimer(time.Second * time.Duration(c.config.RecvTimeout))
	defer recvTimeoutTimer.Stop()
	// 心跳包计时
	heartBeatTimer := time.NewTimer(time.Second * time.Duration(c.config.HeartBeatInterval))
	defer heartBeatTimer.Stop()
	c.sendBuffer = c.createSendBuffer()
	for c.IsConnected() {
		var delaySendDecodePacketData []byte
		select {
		case packet := <-c.sendPacketCache:
			if packet == nil {
				logger.Error("packet==nil %v", c.GetConnectionId())
				return
			}
			hasError := false
			delaySendDecodePacketData, hasError = c.writePacket(packet)
			if hasError {
				return
			}

		case <-recvTimeoutTimer.C:
			if !c.checkRecvTimeout(recvTimeoutTimer) {
				return
			}

		case <-heartBeatTimer.C:
			delaySendDecodePacketData = c.onHeartBeatTimeUp(heartBeatTimer)

		case <-c.readStopNotifyChan:
			logger.Debug("recv readStopNotify %v", c.GetConnectionId())
			return

		case <-ctx.Done():
			// 收到外部的关闭通知
			logger.Debug("recv closeNotify %v", c.GetConnectionId())
			return
		}

		sendErr := c.sendEncodedBuffer(delaySendDecodePacketData)
		if sendErr != nil {
			return
		}
	}
}

// 数据包编码
// Encode里面会把编码后的数据直接写入sendBuffer
func (c *TcpConnection) writePacket(packet Packet) (delaySendDecodePacketData []byte, hasError bool) {
	// 数据包编码
	// Encode里面会把编码后的数据直接写入sendBuffer
	delaySendDecodePacketData = c.codec.Encode(c, packet)
	if len(delaySendDecodePacketData) > 0 {
		// Encode里面写不完的数据延后处理
		logger.Debug("%v sendBuffer is full delaySize:%v", c.GetConnectionId(), len(delaySendDecodePacketData))
		return
	}
	packetCount := len(c.sendPacketCache)
	// 还有其他数据包在chan里,就进行批量合并
	// batch write to RingBuffer
	if packetCount > 0 {
		for i := 0; i < packetCount; i++ {
			// 这里不会阻塞
			// not block here
			newPacket, ok := <-c.sendPacketCache
			if !ok {
				logger.Error("newPacket==nil %v", c.GetConnectionId())
				hasError = true
				return
			}
			// 数据包编码
			delaySendDecodePacketData = c.codec.Encode(c, newPacket)
			if len(delaySendDecodePacketData) > 0 {
				// if the RingBuffer is full, leave the rest packet to process later
				logger.Debug("%v sendBuffer is full delaySize:%v", c.GetConnectionId(), len(delaySendDecodePacketData))
				break
			}
		}
	}
	return
}

func (c *TcpConnection) sendEncodedBuffer(delaySendDecodePacketData []byte) error {
	if c.sendBuffer.UnReadLength() == 0 {
		return nil
	}
	// 可读数据有可能分别存在数组的尾部和头部,所以需要循环发送,有可能需要发送多次
	// the data may be separate at tail and head of the RingBuffer, so we need to send loop
	for c.IsConnected() && c.sendBuffer.UnReadLength() > 0 {
		if c.config.WriteTimeout > 0 {
			setTimeoutErr := c.conn.SetWriteDeadline(time.Now().Add(time.Duration(c.config.WriteTimeout) * time.Second))
			// Q:什么情况会导致SetWriteDeadline返回err?
			if setTimeoutErr != nil {
				// ...
				logger.Debug("%v setTimeoutErr:%v", c.GetConnectionId(), setTimeoutErr.Error())
				return setTimeoutErr
			}
		}
		readBuffer := c.sendBuffer.ReadBuffer()
		writeCount, err := c.conn.Write(readBuffer)
		if err != nil {
			logger.Debug("%v write Err:%v", c.GetConnectionId(), err.Error())
			return err
		}
		c.sendBuffer.SetReaded(writeCount)
		if len(delaySendDecodePacketData) > 0 {
			wroteLen, _ := c.sendBuffer.Write(delaySendDecodePacketData)
			// 这里不一定能全部写完
			if wroteLen < len(delaySendDecodePacketData) {
				delaySendDecodePacketData = delaySendDecodePacketData[wroteLen:]
				logger.Debug("%v write delay buffer :%v", c.GetConnectionId(), wroteLen)
			} else {
				delaySendDecodePacketData = nil
			}
		}
	}
	return nil
}

func (c *TcpConnection) checkRecvTimeout(recvTimeoutTimer *time.Timer) bool {
	if c.config.RecvTimeout > 0 {
		nextTimeoutTime := int64(c.config.RecvTimeout) + atomic.LoadInt64(&c.lastRecvPacketTick) - GetCurrentTimeStamp()
		if nextTimeoutTime > 0 {
			if nextTimeoutTime > int64(c.config.RecvTimeout) {
				nextTimeoutTime = int64(c.config.RecvTimeout)
			}
			recvTimeoutTimer.Reset(time.Second * time.Duration(nextTimeoutTime))
		} else {
			// 指定时间内,一直未读取到数据包,则认为该连接掉线了,可能处于"假死"状态了
			// 需要主动关闭该连接,防止连接"泄漏"
			logger.Debug("recv Timeout %v IsConnector %v", c.GetConnectionId(), c.IsConnector())
			return false
		}
	}
	return true
}

func (c *TcpConnection) onHeartBeatTimeUp(heartBeatTimer *time.Timer) (delaySendDecodePacketData []byte) {
	if c.isConnector && c.config.HeartBeatInterval > 0 && c.handler != nil {
		if heartBeatPacket := c.handler.CreateHeartBeatPacket(c); heartBeatPacket != nil {
			delaySendDecodePacketData = c.codec.Encode(c, heartBeatPacket)
			heartBeatTimer.Reset(time.Second * time.Duration(c.config.HeartBeatInterval))
		}
	}
	return
}

func (c *TcpConnection) Close() {
	c.closeOnce.Do(func() {
		atomic.StoreInt32(&c.isConnected, 0)
		if c.conn != nil {
			c.conn.Close()
			//c.conn = nil
		}
		if c.handler != nil {
			c.handler.OnDisconnected(c)
		}
		if c.onClose != nil {
			c.onClose(c)
		}
	})
}

// 创建用于批量发包的RingBuffer
func (c *TcpConnection) createSendBuffer() *RingBuffer {
	ringBufferSize := c.config.SendBufferSize
	if ringBufferSize == 0 {
		ringBufferSize = DefaultConnectionConfig.SendBufferSize
	}
	return NewRingBuffer(int(ringBufferSize))
}

// 创建用于批量收包的RingBuffer
func (c *TcpConnection) createRecvBuffer() *RingBuffer {
	ringBufferSize := c.config.RecvBufferSize
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
func (c *TcpConnection) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (c *TcpConnection) RemoteAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}
