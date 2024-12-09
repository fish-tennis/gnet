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

func (l *WsListener) GetConnection(connectionId uint32) Connection {
	l.connectionMapLock.RLock()
	conn := l.connectionMap[connectionId]
	l.connectionMapLock.RUnlock()
	return conn
}

// range for accepted connections
func (l *WsListener) RangeConnections(f func(conn Connection) bool) {
	l.connectionMapLock.RLock()
	defer l.connectionMapLock.RUnlock()
	for _, conn := range l.connectionMap {
		if conn.IsConnected() {
			if !f(conn) {
				return
			}
		}
	}
}

func (l *WsListener) Broadcast(packet Packet) {
	l.connectionMapLock.RLock()
	defer l.connectionMapLock.RUnlock()
	for _, conn := range l.connectionMap {
		if conn.IsConnected() {
			conn.SendPacket(packet.Clone())
		}
	}
}

func (l *WsListener) Addr() net.Addr {
	return nil
}

func (l *WsListener) Close() {
}

func (l *WsListener) IsRunning() bool {
	return atomic.LoadInt32(&l.isRunning) > 0
}

func (l *WsListener) Start(ctx context.Context, listenAddress string) bool {
	http.HandleFunc(l.config.Path, func(w http.ResponseWriter, r *http.Request) {
		l.serve(ctx, w, r)
	})
	l.upgrader = websocket.Upgrader{
		ReadBufferSize:  int(l.acceptConnectionConfig.RecvBufferSize),
		WriteBufferSize: int(l.acceptConnectionConfig.SendBufferSize),
	}
	// 监听协程
	atomic.StoreInt32(&l.isRunning, 1)
	go func() {
		var err error
		if l.config.CertFile != "" {
			err = http.ListenAndServeTLS(listenAddress, l.config.CertFile, l.config.KeyFile, nil)
		} else {
			err = http.ListenAndServe(listenAddress, nil)
		}
		if err != nil {
			atomic.StoreInt32(&l.isRunning, 0)
			return
		}
	}()
	// wait for ListenAndServe err
	time.Sleep(time.Second)
	if !l.IsRunning() {
		logger.Error("Listen Failed %v", l.GetListenerId())
		return false
	}
	logger.Debug("WsListener Start %v", l.GetListenerId())

	// 关闭响应协程
	l.netMgrWg.Add(1)
	go func() {
		defer l.netMgrWg.Done()
		for l.IsRunning() {
			select {
			// 关闭通知
			case <-ctx.Done():
				logger.Debug("recv closeNotify %v", l.GetListenerId())
				l.Close()
				return
			}
		}
	}()

	return true
}

func (l *WsListener) serve(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	conn, err := l.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("serveErr %v: %v", l.GetListenerId(), err.(error))
		return
	}
	newTcpConn := NewWsConnectionAccept(conn, &l.acceptConnectionConfig, l.acceptConnectionCodec, l.acceptConnectionHandler)
	l.connectionMapLock.Lock()
	l.connectionMap[newTcpConn.GetConnectionId()] = newTcpConn
	l.connectionMapLock.Unlock()
	newTcpConn.Start(ctx, l.netMgrWg, func(connection Connection) {
		if l.handler != nil {
			l.handler.OnConnectionDisconnect(l, connection)
		}
		l.connectionMapLock.Lock()
		delete(l.connectionMap, connection.GetConnectionId())
		l.connectionMapLock.Unlock()
	})
	if l.handler != nil {
		l.handler.OnConnectionConnected(l, newTcpConn)
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
