package gnet

import (
	"context"
	"crypto/tls"
	"github.com/gorilla/websocket"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// WsConnection WebSocket
type WsConnection struct {
	baseConnection
	conn *websocket.Conn
	// 读协程结束标记
	// notify chan for read goroutine end
	readStopNotifyChan chan struct{}
	closeOnce          sync.Once
	// 保证回调只执行一次,与closeOnce分离,避免回调中重入Close()导致死锁
	callbackOnce       sync.Once
	// close callback
	onClose func(connection Connection)
	// 最近收到完整数据包的时间(时间戳:秒)
	lastRecvPacketTick int64
}

func (c *WsConnection) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

func (c *WsConnection) RemoteAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}

func (c *WsConnection) Close() {
	// closeOnce只做资源关闭,不执行回调,避免回调中重入Close()导致sync.Once死锁
	c.closeOnce.Do(func() {
		atomic.StoreInt32(&c.isConnected, 0)
		if c.conn != nil {
			_ = c.conn.Close()
		}
		if c.sendPacketCache != nil {
			close(c.sendPacketCache)
		}
	})
	// 回调在closeOnce外部执行,即使回调中重入Close(),closeOnce.Do直接跳过,不会死锁
	c.callbackOnce.Do(func() {
		if c.handler != nil {
			safeCall(func() { c.handler.OnDisconnected(c) })
		}
		if c.onClose != nil {
			safeCall(func() { c.onClose(c) })
		}
	})
}

func (c *WsConnection) Connect(address string) bool {
	u := url.URL{Scheme: c.config.Scheme, Host: address, Path: c.config.Path}
	dialer := *websocket.DefaultDialer
	if c.config.Scheme == "wss" {
		dialer.TLSClientConfig = &tls.Config{RootCAs: nil, InsecureSkipVerify: c.config.InsecureSkipVerify}
	}
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
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

func (c *WsConnection) Start(ctx context.Context, netMgrWg *sync.WaitGroup, onClose func(connection Connection)) {
	c.onClose = onClose
	// 先通知业务层连接已建立,避免goroutine中的OnDisconnected先于OnConnected触发
	if c.handler != nil {
		c.handler.OnConnected(c, true)
	}
	// 初始化最近收包时间,避免首个RecvTimeout周期内未收到包就被误判超时
	atomic.StoreInt64(&c.lastRecvPacketTick, GetCurrentTimeStamp())
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
		close(c.readStopNotifyChan)
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

func (c *WsConnection) readLoop() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("readLoop fatal %v: %v", c.GetConnectionId(), err)
			LogStack()
		}
	}()

	logger.Debug("readLoop begin %v isConnector:%v", c.GetConnectionId(), c.IsConnector())
	codec := c.GetCodec()
	if c.config.MaxPacketSize > 0 {
		c.conn.SetReadLimit(int64(c.config.MaxPacketSize))
	}
	// 收到ping/pong时更新最近收包时间,避免只靠数据消息保活时误判超时
	c.conn.SetPongHandler(func(string) error {
		atomic.StoreInt64(&c.lastRecvPacketTick, GetCurrentTimeStamp())
		return nil
	})
	defaultPingHandler := c.conn.PingHandler()
	c.conn.SetPingHandler(func(appData string) error {
		atomic.StoreInt64(&c.lastRecvPacketTick, GetCurrentTimeStamp())
		return defaultPingHandler(appData)
	})
	for c.IsConnected() {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error("readLoopErr %v err:%v", c.GetConnectionId(), err.Error())
			}
			break
		}
		if messageType != websocket.BinaryMessage {
			logger.Debug("messageTypeErr %v messageType:%v", c.GetConnectionId(), messageType)
			break
		}
		if len(data) < int(codec.PacketHeaderSize()) {
			logger.Debug("messageType Err %v messageType:%v", c.GetConnectionId(), messageType)
			break
		}
		newPacket, decodeError := codec.Decode(c, data)
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
}

func (c *WsConnection) writeLoop(ctx context.Context) {
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
			logger.Debug("recv closeNotify %v isConnector:%v", c.GetConnectionId(), c.isConnector)
			if c.isConnector {
				if c.config.WriteTimeout > 0 {
					c.conn.SetWriteDeadline(time.Now().Add(time.Duration(c.config.WriteTimeout) * time.Second))
				}
				// Cleanly close the connection by sending a close message and then
				// waiting (with Timeout) for the server to close the connection.
				err := c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				if err != nil {
					logger.Debug("writeCloseMessageErr %v err:%v", c.GetConnectionId(), err.Error())
					return
				}
				closeTimer := time.NewTimer(time.Second)
				select {
				case <-c.readStopNotifyChan:
				case <-closeTimer.C:
				}
				closeTimer.Stop()
			}
			return
		}
	}
}

func (c *WsConnection) writePacket(packet Packet) bool {
	codec := c.GetCodec()
	// 这里编码的是包体,不包含包头
	packetData := codec.Encode(c, packet)
	// 包头数据
	newPacketHeader := codec.CreatePacketHeader(c, packet, packetData)
	headerSize := int(codec.PacketHeaderSize())
	fullData := make([]byte, headerSize+len(packetData))
	newPacketHeader.WriteTo(fullData)
	copy(fullData[headerSize:], packetData)
	if c.config.WriteTimeout > 0 {
		c.conn.SetWriteDeadline(time.Now().Add(time.Duration(c.config.WriteTimeout) * time.Second))
	}
	err := c.conn.WriteMessage(websocket.BinaryMessage, fullData)
	if err != nil {
		logger.Debug("writePacketErr %v %v", c.GetConnectionId(), err.Error())
		return false
	}
	return true
}

func (c *WsConnection) checkRecvTimeout(recvTimeoutTimer *time.Timer) bool {
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

func (c *WsConnection) onHeartBeatTimeUp(heartBeatTimer *time.Timer) bool {
	// 无论本次是否发送心跳包,都要Reset,否则Timer触发后不Reset将永不再触发
	defer func() {
		if c.config.HeartBeatInterval > 0 {
			heartBeatTimer.Reset(time.Second * time.Duration(c.config.HeartBeatInterval))
		}
	}()
	if c.isConnector && c.config.HeartBeatInterval > 0 && c.handler != nil {
		if c.config.WriteTimeout > 0 {
			c.conn.SetWriteDeadline(time.Now().Add(time.Duration(c.config.WriteTimeout) * time.Second))
		}
		// WebSocket的内部协议
		if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			logger.Debug("PingMessageErr %v err:%v", c.GetConnectionId(), err.Error())
			return false
		}
		// 自定义的心跳协议
		if heartBeatPacket := c.handler.CreateHeartBeatPacket(c); heartBeatPacket != nil {
			if !c.writePacket(heartBeatPacket) {
				return false
			}
		}
	}
	return true
}

func (c *WsConnection) GetConn() *websocket.Conn {
	return c.conn
}

func createWsConnection(config *ConnectionConfig) *WsConnection {
	newConnection := &WsConnection{
		baseConnection: baseConnection{
			connectionId:        NewConnectionId(),
			config:              config,
			codec:               config.Codec,
			handler:             config.Handler,
			sendPacketCache:     make(chan Packet, config.SendPacketCacheCap),
			writeStopNotifyChan: make(chan struct{}),
			rpcCalls:            newRpcCalls(),
		},
		readStopNotifyChan: make(chan struct{}),
	}
	return newConnection
}

func NewWsConnection(config *ConnectionConfig) *WsConnection {
	newConnection := createWsConnection(config)
	newConnection.isConnector = true
	return newConnection
}

func NewWsConnectionAccept(conn *websocket.Conn, config *ConnectionConfig, codec Codec, handler ConnectionHandler) *WsConnection {
	newConnection := createWsConnection(config)
	newConnection.isConnector = false
	atomic.StoreInt32(&newConnection.isConnected, 1)
	newConnection.conn = conn
	return newConnection
}
