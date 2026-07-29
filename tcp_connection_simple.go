package gnet

import (
	"context"
	"io"
	"net"
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
	// 收包复用buffer,避免每包make
	readHeaderBuf []byte
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
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}
	newConnection.conn = conn
	return newConnection
}

func createTcpConnectionSimple(config *ConnectionConfig) *TcpConnectionSimple {
	newConnection := &TcpConnectionSimple{
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
	return newConnection
}

func (c *TcpConnectionSimple) Connect(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		atomic.StoreInt32(&c.isConnected, 0)
		logger.Error("Connect failed %v: %v", c.GetConnectionId(), err.Error())
		if c.handler != nil {
			c.handler.OnConnected(c, false)
		}
		return false
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}
	c.conn = conn
	atomic.StoreInt32(&c.isConnected, 1)
	return true
}

// start read&write goroutine
func (c *TcpConnectionSimple) Start(ctx context.Context, netMgrWg *sync.WaitGroup, onClose func(connection Connection)) {
	c.onClose = onClose
	// 先通知业务层连接已建立,避免goroutine中的OnDisconnected先于OnConnected触发
	if c.handler != nil {
		c.handler.OnConnected(c, true)
	}
	// 开启收包协程
	netMgrWg.Add(1)
	go func() {
		defer func() {
			netMgrWg.Done()
			if err := recover(); err != nil {
				logger.Error("read fatal %v: %v", c.GetConnectionId(), err)
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
				logger.Error("write fatal %v: %v", c.GetConnectionId(), err)
				LogStack()
			}
		}()
		c.writeLoop(ctx)
		c.Close()
		// 写协程结束了,通知阻塞中的SendPacket结束
		close(c.writeStopNotifyChan)
	}(ctx)
}

// read goroutine
func (c *TcpConnectionSimple) readLoop() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("readLoop fatal %v: %v", c.GetConnectionId(), err)
			LogStack()
		}
	}()

	logger.Debug("readLoop begin %v isConnector:%v", c.GetConnectionId(), c.IsConnector())
	codec := c.GetCodec()
	headerSize := int(codec.PacketHeaderSize())
	// 预分配header buffer,整个连接生命周期复用
	c.readHeaderBuf = make([]byte, headerSize)
	for c.IsConnected() {
		// 先读取消息头
		// read packet header first
		readHeaderSize, err := io.ReadFull(c.conn, c.readHeaderBuf)
		if err != nil {
			if err != io.EOF {
				logger.Debug("readLoop %v err:%v", c.GetConnectionId(), err.Error())
			}
			break
		}
		if readHeaderSize != headerSize {
			break
		}
		newPacketHeader := codec.CreatePacketHeader(c, nil, nil)
		newPacketHeader.ReadFrom(c.readHeaderBuf)
		packetDataLen := int(newPacketHeader.Len())
		fullPacketData := make([]byte, headerSize+packetDataLen)
		copy(fullPacketData, c.readHeaderBuf)
		if packetDataLen > 0 {
			// 读取消息体
			// read packet body
			readDataSize, err := io.ReadFull(c.conn, fullPacketData[readHeaderSize:])
			if err != nil {
				if err != io.EOF {
					logger.Debug("readLoop %v err:%v", c.GetConnectionId(), err.Error())
				}
				break
			}
			if readDataSize != packetDataLen {
				break
			}
		}
		newPacket, decodeError := codec.Decode(c, fullPacketData)
		if decodeError != nil {
			logger.Error("%v decodeError:%v", c.GetConnectionId(), decodeError.Error())
			return
		}
		if newPacket == nil {
			break
		}
		// 最近收到完整数据包的时间
		atomic.StoreInt64(&c.lastRecvPacketTick, GetCurrentTimeStamp())
		if c.handler != nil {
			if c.rpcCalls.putReply(newPacket) {
				continue
			}
			c.handler.OnRecvPacket(c, newPacket)
		}
	}
	//logger.Debug("readLoop end %v IsConnector:%v", c.GetConnectionId(), c.IsConnector())
}

// write goroutine
func (c *TcpConnectionSimple) writeLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("writeLoop fatal %v: %v", c.GetConnectionId(), err)
			LogStack()
		}
		logger.Debug("writeLoop end %v IsConnector:%v", c.GetConnectionId(), c.IsConnector())
	}()

	logger.Debug("writeLoop begin %v isConnector:%v", c.GetConnectionId(), c.IsConnector())
	// 收包超时计时,用于检测掉线
	recvTimeoutTimer := time.NewTimer(time.Second * time.Duration(c.config.RecvTimeout))
	defer recvTimeoutTimer.Stop()
	// 心跳包计时
	heartBeatTimer := time.NewTimer(time.Second * time.Duration(c.config.HeartBeatInterval))
	defer heartBeatTimer.Stop()
	for c.IsConnected() {
		select {
		case packet := <-c.sendPacketCache:
			if packet == nil {
				if c.IsConnected() {
					logger.Error("packet==nil %v", c.GetConnectionId())
				}
				return
			}
			if !c.writePacket(packet) {
				return
			}

		case <-recvTimeoutTimer.C:
			if !c.checkRecvTimeout(recvTimeoutTimer) {
				return
			}

		case <-heartBeatTimer.C:
			if !c.onHeartBeatTimeUp(heartBeatTimer) {
				return
			}

		case <-c.readStopNotifyChan:
			logger.Debug("recv readStopNotify %v", c.GetConnectionId())
			return

		case <-ctx.Done():
			// 收到外部的关闭通知
			logger.Debug("recv closeNotify %v", c.GetConnectionId())
			return
		}
	}
	//logger.Debug("writeLoop end %v isConnector:%v", c.GetConnectionId(), c.IsConnector())
}

func (c *TcpConnectionSimple) writePacket(packet Packet) bool {
	codec := c.GetCodec()
	// 这里编码的是包体,不包含包头
	packetData := codec.Encode(c, packet)
	// 包头数据
	newPacketHeader := codec.CreatePacketHeader(c, packet, packetData)
	headerSize := int(codec.PacketHeaderSize())
	// 合并包头和包体到同一buffer,一次Write发送
	fullData := make([]byte, headerSize+len(packetData))
	newPacketHeader.WriteTo(fullData)
	copy(fullData[headerSize:], packetData)
	if c.config.WriteTimeout > 0 {
		setTimeoutErr := c.conn.SetWriteDeadline(time.Now().Add(time.Duration(c.config.WriteTimeout) * time.Second))
		if setTimeoutErr != nil {
			logger.Debug("%v setTimeoutErr:%v", c.GetConnectionId(), setTimeoutErr.Error())
			return false
		}
	}
	// 一次性发送,减少syscall
	writeCount := 0
	for writeCount < len(fullData) {
		n, err := c.conn.Write(fullData[writeCount:])
		if err != nil {
			logger.Error("%v send error:%v", c.GetConnectionId(), err.Error())
			return false
		}
		writeCount += n
	}
	return true
}

func (c *TcpConnectionSimple) checkRecvTimeout(recvTimeoutTimer *time.Timer) bool {
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
			logger.Debug("recv Timeout %v", c.GetConnectionId())
			return false
		}
	}
	return true
}

func (c *TcpConnectionSimple) onHeartBeatTimeUp(heartBeatTimer *time.Timer) bool {
	// 无论本次是否发送心跳包,都要Reset,否则Timer触发后不Reset将永不再触发
	defer func() {
		if c.config.HeartBeatInterval > 0 {
			heartBeatTimer.Reset(time.Second * time.Duration(c.config.HeartBeatInterval))
		}
	}()
	if c.isConnector && c.config.HeartBeatInterval > 0 && c.handler != nil {
		if heartBeatPacket := c.handler.CreateHeartBeatPacket(c); heartBeatPacket != nil {
			if !c.writePacket(heartBeatPacket) {
				return false
			}
		}
	}
	return true
}

func (c *TcpConnectionSimple) Close() {
	c.closeOnce.Do(func() {
		atomic.StoreInt32(&c.isConnected, 0)
		if c.conn != nil {
			c.conn.Close()
		}
		if c.handler != nil {
			safeCall(func() { c.handler.OnDisconnected(c) })
		}
		if c.onClose != nil {
			safeCall(func() { c.onClose(c) })
		}
		if c.sendPacketCache != nil {
			close(c.sendPacketCache)
		}
	})
}

// LocalAddr returns the local network address.
func (c *TcpConnectionSimple) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (c *TcpConnectionSimple) RemoteAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}
