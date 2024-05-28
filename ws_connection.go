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
	// close callback
	onClose func(connection Connection)
	// 最近收到完整数据包的时间(时间戳:秒)
	lastRecvPacketTick int64
}

func (this *WsConnection) LocalAddr() net.Addr {
	return this.conn.LocalAddr()
}

func (this *WsConnection) RemoteAddr() net.Addr {
	return this.conn.RemoteAddr()
}

func (this *WsConnection) Close() {
	this.closeOnce.Do(func() {
		atomic.StoreInt32(&this.isConnected, 0)
		if this.conn != nil {
			_ = this.conn.Close()
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

func (this *WsConnection) Connect(address string) bool {
	u := url.URL{Scheme: this.config.Scheme, Host: address, Path: this.config.Path}
	dialer := *websocket.DefaultDialer
	if this.config.Scheme == "wss" {
		dialer.TLSClientConfig = &tls.Config{RootCAs: nil, InsecureSkipVerify: true}
	}
	conn, _, err := dialer.Dial(u.String(), nil)
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

func (this *WsConnection) Start(ctx context.Context, netMgrWg *sync.WaitGroup, onClose func(connection Connection)) {
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
		close(this.readStopNotifyChan)
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

func (this *WsConnection) readLoop() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("readLoop fatal %v: %v", this.GetConnectionId(), err.(error))
			LogStack()
		}
	}()

	logger.Debug("readLoop begin %v isConnector:%v", this.GetConnectionId(), this.IsConnector())
	if this.config.MaxPacketSize > 0 {
		this.conn.SetReadLimit(int64(this.config.MaxPacketSize))
	}
	for this.IsConnected() {
		if this.config.RecvTimeout > 0 {
			this.conn.SetReadDeadline(time.Now().Add(time.Duration(this.config.RecvTimeout) * time.Second))
		}
		messageType, data, err := this.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error("readLoopErr %v err:%v", this.GetConnectionId(), err.Error())
			}
			break
		}
		if messageType != websocket.BinaryMessage {
			logger.Debug("messageTypeErr %v messageType:%v", this.GetConnectionId(), messageType)
			break
		}
		if len(data) < int(this.codec.PacketHeaderSize()) {
			logger.Debug("messageType Err %v messageType:%v", this.GetConnectionId(), messageType)
			break
		}
		newPacket, decodeError := this.codec.Decode(this, data)
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
}

func (this *WsConnection) writeLoop(ctx context.Context) {
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
			logger.Debug("recv closeNotify %v isConnector:%v", this.GetConnectionId(), this.isConnector)
			if this.isConnector {
				if this.config.WriteTimeout > 0 {
					this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout) * time.Second))
				}
				// Cleanly close the connection by sending a close message and then
				// waiting (with timeout) for the server to close the connection.
				err := this.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				if err != nil {
					logger.Debug("writeCloseMessageErr %v err:%v", this.GetConnectionId(), err.Error())
					return
				}
				select {
				case <-this.readStopNotifyChan:
				case <-time.After(time.Second):
				}
			}
			return
		}
	}
}

func (this *WsConnection) writePacket(packet Packet) bool {
	// 这里编码的是包体,不包含包头
	packetData := this.codec.Encode(this, packet)
	// 包头数据
	newPacketHeader := this.codec.CreatePacketHeader(this, packet, packetData)
	fullData := make([]byte, int(this.codec.PacketHeaderSize())+len(packetData))
	newPacketHeader.WriteTo(fullData)
	copy(fullData[this.codec.PacketHeaderSize():], packetData)
	if this.config.WriteTimeout > 0 {
		this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout) * time.Second))
	}
	err := this.conn.WriteMessage(websocket.BinaryMessage, fullData)
	if err != nil {
		logger.Debug("writePacketErr %v %v", this.GetConnectionId(), err.Error())
		return false
	}
	return true
}

func (this *WsConnection) checkRecvTimeout(recvTimeoutTimer *time.Timer) bool {
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

func (this *WsConnection) onHeartBeatTimeUp(heartBeatTimer *time.Timer) bool {
	if this.isConnector && this.config.HeartBeatInterval > 0 && this.handler != nil {
		if this.config.WriteTimeout > 0 {
			this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout) * time.Second))
		}
		// WebSocket的内部协议
		if err := this.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			logger.Debug("PingMessageErr %v err:%v", this.GetConnectionId(), err.Error())
			return false
		}
		// 自定义的心跳协议
		if heartBeatPacket := this.handler.CreateHeartBeatPacket(this); heartBeatPacket != nil {
			if !this.writePacket(heartBeatPacket) {
				return false
			}
		}
		heartBeatTimer.Reset(time.Second * time.Duration(this.config.HeartBeatInterval))
	}
	return true
}

func (this *WsConnection) GetConn() *websocket.Conn {
	return this.conn
}

func createWsConnection(config *ConnectionConfig) *WsConnection {
	newConnection := &WsConnection{
		baseConnection: baseConnection{
			connectionId:    NewConnectionId(),
			config:          config,
			codec:           config.Codec,
			handler:         config.Handler,
			sendPacketCache: make(chan Packet, config.SendPacketCacheCap),
			rpcCalls:        newRpcCalls(),
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
