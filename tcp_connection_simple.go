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
			connectionId:    NewConnectionId(),
			config:          config,
			codec:           config.Codec,
			handler:         config.Handler,
			sendPacketCache: make(chan Packet, config.SendPacketCacheCap),
			rpcCalls:        newRpcCalls(),
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
	c.conn = conn
	atomic.StoreInt32(&c.isConnected, 1)
	return true
}

// start read&write goroutine
func (c *TcpConnectionSimple) Start(ctx context.Context, netMgrWg *sync.WaitGroup, onClose func(connection Connection)) {
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
	}(ctx)

	if c.handler != nil {
		c.handler.OnConnected(c, true)
	}
}

// read goroutine
func (c *TcpConnectionSimple) readLoop() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("readLoop fatal %v: %v", c.GetConnectionId(), err.(error))
			LogStack()
		}
	}()

	logger.Debug("readLoop begin %v isConnector:%v", c.GetConnectionId(), c.IsConnector())
	for c.IsConnected() {
		// 先读取消息头
		// read packet header first
		messageHeaderData := make([]byte, c.codec.PacketHeaderSize())
		readHeaderSize, err := io.ReadFull(c.conn, messageHeaderData)
		if err != nil {
			if err != io.EOF {
				logger.Debug("readLoop %v err:%v", c.GetConnectionId(), err.Error())
			}
			break
		}
		if readHeaderSize != len(messageHeaderData) {
			break
		}
		newPacketHeader := c.codec.CreatePacketHeader(c, nil, nil)
		newPacketHeader.ReadFrom(messageHeaderData)
		packetDataLen := int(newPacketHeader.Len())
		fullPacketData := make([]byte, len(messageHeaderData)+packetDataLen)
		copy(fullPacketData, messageHeaderData)
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
		newPacket, decodeError := c.codec.Decode(c, fullPacketData)
		if decodeError != nil {
			logger.Error("%v decodeError:%v", c.GetConnectionId(), decodeError.Error())
			return
		}
		if newPacket == nil {
			break
		}
		// 最近收到完整数据包的时间
		c.lastRecvPacketTick = GetCurrentTimeStamp()
		if c.handler != nil {
			c.handler.OnRecvPacket(c, newPacket)
		}
	}
	//logger.Debug("readLoop end %v IsConnector:%v", c.GetConnectionId(), c.IsConnector())
}

// write goroutine
func (c *TcpConnectionSimple) writeLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("writeLoop fatal %v: %v", c.GetConnectionId(), err.(error))
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
				logger.Error("packet==nil %v", c.GetConnectionId())
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
	// 这里编码的是包体,不包含包头
	packetData := c.codec.Encode(c, packet)
	// 包头数据
	newPacketHeader := c.codec.CreatePacketHeader(c, packet, packetData)
	packetHeaderData := make([]byte, c.codec.PacketHeaderSize())
	newPacketHeader.WriteTo(packetHeaderData)
	writeCount := 0
	// 先发送包头数据
	for writeCount < len(packetHeaderData) {
		if c.config.WriteTimeout > 0 {
			setTimeoutErr := c.conn.SetWriteDeadline(time.Now().Add(time.Duration(c.config.WriteTimeout) * time.Second))
			// Q:什么情况会导致SetWriteDeadline返回err?
			if setTimeoutErr != nil {
				// ...
				logger.Debug("%v setTimeoutErr:%v", c.GetConnectionId(), setTimeoutErr.Error())
				return false
			}
		}
		n, err := c.conn.Write(packetHeaderData[writeCount:])
		if err != nil {
			logger.Error("%v send error:%v", c.GetConnectionId(), err.Error())
			return false
		}
		writeCount += n
	}

	writeCount = 0
	// 再发送包体数据
	for writeCount < len(packetData) {
		if c.config.WriteTimeout > 0 {
			setTimeoutErr := c.conn.SetWriteDeadline(time.Now().Add(time.Duration(c.config.WriteTimeout) * time.Second))
			// Q:什么情况会导致SetWriteDeadline返回err?
			if setTimeoutErr != nil {
				// ...
				logger.Debug("%v setTimeoutErr:%v", c.GetConnectionId(), setTimeoutErr.Error())
				return false
			}
		}
		n, err := c.conn.Write(packetData[writeCount:])
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
		nextTimeoutTime := int64(c.config.RecvTimeout) + c.lastRecvPacketTick - GetCurrentTimeStamp()
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
	if c.isConnector && c.config.HeartBeatInterval > 0 && c.handler != nil {
		if heartBeatPacket := c.handler.CreateHeartBeatPacket(c); heartBeatPacket != nil {
			if !c.writePacket(heartBeatPacket) {
				return false
			}
			heartBeatTimer.Reset(time.Second * time.Duration(c.config.HeartBeatInterval))
		}
	}
	return true
}

func (c *TcpConnectionSimple) Close() {
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
