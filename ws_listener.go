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
	// http server,用于优雅关闭
	httpServer *http.Server

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
	// 先快照连接列表,释放锁后再发送,避免慢消费连接阻塞其他需要写锁的操作
	conns := make([]Connection, 0, len(l.connectionMap))
	l.connectionMapLock.RLock()
	for _, conn := range l.connectionMap {
		if conn.IsConnected() {
			conns = append(conns, conn)
		}
	}
	l.connectionMapLock.RUnlock()
	for _, conn := range conns {
		conn.SendPacket(packet.Clone())
	}
}

func (l *WsListener) Addr() net.Addr {
	return nil
}

// 关闭监听,并关闭管理的连接
//
//	close listen, close the accepted connections
func (l *WsListener) Close() {
	l.closeOnce.Do(func() {
		atomic.StoreInt32(&l.isRunning, 0)
		// 关闭HTTP server,停止接受新的WebSocket连接
		if l.httpServer != nil {
			_ = l.httpServer.Close()
		}
		// 快照并关闭所有已建立的连接
		connMap := make(map[uint32]Connection)
		l.connectionMapLock.RLock()
		for k, v := range l.connectionMap {
			connMap[k] = v
		}
		l.connectionMapLock.RUnlock()
		for _, conn := range connMap {
			conn.Close()
		}
		if l.onClose != nil {
			l.onClose(l)
		}
	})
}

func (l *WsListener) IsRunning() bool {
	return atomic.LoadInt32(&l.isRunning) > 0
}

func (l *WsListener) Start(ctx context.Context, listenAddress string, checkOrigin func(r *http.Request) bool) bool {
	http.HandleFunc(l.config.Path, func(w http.ResponseWriter, r *http.Request) {
		l.serve(ctx, w, r)
	})
	l.upgrader = websocket.Upgrader{
		ReadBufferSize:  int(l.acceptConnectionConfig.RecvBufferSize),
		WriteBufferSize: int(l.acceptConnectionConfig.SendBufferSize),
		CheckOrigin: checkOrigin,
	}
	// 监听协程
	atomic.StoreInt32(&l.isRunning, 1)
	l.httpServer = &http.Server{Addr: listenAddress}
	go func() {
		var err error
		if l.config.CertFile != "" {
			err = l.httpServer.ListenAndServeTLS(l.config.CertFile, l.config.KeyFile)
		} else {
			err = l.httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			atomic.StoreInt32(&l.isRunning, 0)
			logger.Error("ListenAndServe failed %v: %v", l.GetListenerId(), err.Error())
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
		logger.Error("serveErr %v: %v", l.GetListenerId(), err)
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
