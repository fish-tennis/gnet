package gnet

import (
	"context"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type WsListener struct {
	baseListener
	upgrader websocket.Upgrader

	acceptConnectionConfig  ConnectionConfig
	acceptConnectionCodec   Codec
	acceptConnectionHandler ConnectionHandler

	// manage the accepted connections
	connectionMap     map[uint32]Connection
	connectionMapLock sync.RWMutex

	isRunning int32
	closeOnce sync.Once
	// close callback
	onClose func(listener Listener)

	// 外部传进来的WaitGroup
	netMgrWg *sync.WaitGroup
}

func (this *WsListener) GetConnection(connectionId uint32) Connection {
	this.connectionMapLock.RLock()
	conn := this.connectionMap[connectionId]
	this.connectionMapLock.RUnlock()
	return conn
}

// range for accepted connections
func (this *WsListener) RangeConnections(f func(conn Connection) bool) {
	this.connectionMapLock.RLock()
	defer this.connectionMapLock.RUnlock()
	for _, conn := range this.connectionMap {
		if conn.IsConnected() {
			if !f(conn) {
				return
			}
		}
	}
}

func (this *WsListener) Broadcast(packet Packet) {
	this.connectionMapLock.RLock()
	defer this.connectionMapLock.RUnlock()
	for _, conn := range this.connectionMap {
		if conn.IsConnected() {
			conn.SendPacket(packet.Clone())
		}
	}
}

func (this *WsListener) Addr() net.Addr {
	return nil
}

func (this *WsListener) Close() {
}

func (this *WsListener) IsRunning() bool {
	return atomic.LoadInt32(&this.isRunning) > 0
}

func (this *WsListener) Start(ctx context.Context, listenAddress string) bool {
	http.HandleFunc(this.config.Path, func(w http.ResponseWriter, r *http.Request) {
		this.serve(ctx, w, r)
	})
	this.upgrader = websocket.Upgrader{
		ReadBufferSize:  int(this.acceptConnectionConfig.RecvBufferSize),
		WriteBufferSize: int(this.acceptConnectionConfig.SendBufferSize),
	}
	// 监听协程
	atomic.StoreInt32(&this.isRunning, 1)
	go func() {
		var err error
		if this.config.CertFile != "" {
			err = http.ListenAndServeTLS(listenAddress, this.config.CertFile, this.config.KeyFile, nil)
		} else {
			err = http.ListenAndServe(listenAddress, nil)
		}
		if err != nil {
			atomic.StoreInt32(&this.isRunning, 0)
			return
		}
	}()
	// wait for ListenAndServe err
	time.Sleep(time.Second)
	if !this.IsRunning() {
		logger.Error("Listen Failed %v", this.GetListenerId())
		return false
	}
	logger.Debug("WsListener Start %v", this.GetListenerId())

	// 关闭响应协程
	this.netMgrWg.Add(1)
	go func() {
		defer this.netMgrWg.Done()
		for this.IsRunning() {
			select {
			// 关闭通知
			case <-ctx.Done():
				logger.Debug("recv closeNotify %v", this.GetListenerId())
				this.Close()
				return
			}
		}
	}()

	return true
}

func (this *WsListener) serve(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	conn, err := this.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("serveErr %v: %v", this.GetListenerId(), err.(error))
		return
	}
	newTcpConn := NewWsConnectionAccept(conn, &this.acceptConnectionConfig, this.acceptConnectionCodec, this.acceptConnectionHandler)
	this.connectionMapLock.Lock()
	this.connectionMap[newTcpConn.GetConnectionId()] = newTcpConn
	this.connectionMapLock.Unlock()
	newTcpConn.Start(ctx, this.netMgrWg, func(connection Connection) {
		if this.handler != nil {
			this.handler.OnConnectionDisconnect(this, connection)
		}
		this.connectionMapLock.Lock()
		delete(this.connectionMap, connection.GetConnectionId())
		this.connectionMapLock.Unlock()
	})
	if this.handler != nil {
		this.handler.OnConnectionConnected(this, newTcpConn)
	}
}

func NewWsListener(listenerConfig *ListenerConfig) *WsListener {
	return &WsListener{
		baseListener: baseListener{
			listenerId: newListenerId(),
			config:     listenerConfig,
			handler:    listenerConfig.ListenerHandler,
		},
		acceptConnectionConfig:  listenerConfig.AcceptConfig,
		acceptConnectionCodec:   listenerConfig.AcceptConfig.Codec,
		acceptConnectionHandler: listenerConfig.AcceptConfig.Handler,
		connectionMap:           make(map[uint32]Connection),
	}
}
