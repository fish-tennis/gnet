package gnet

import (
	"context"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
)

type WsListener struct {
	baseListener
	upgrader websocket.Upgrader
	// http server,用于优雅关闭
	httpServer *http.Server
	// 底层net.Listener,用于获取Addr和关闭
	netListener net.Listener
	// Serve退出通知
	serveDone chan struct{}

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
	// 先快照连接列表,释放锁后再回调,避免回调中调用Close等方法导致死锁
	conns := make([]Connection, 0, len(l.connectionMap))
	l.connectionMapLock.RLock()
	for _, conn := range l.connectionMap {
		if conn.IsConnected() {
			conns = append(conns, conn)
		}
	}
	l.connectionMapLock.RUnlock()
	for _, conn := range conns {
		if !f(conn) {
			return
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
	if l.netListener == nil {
		return nil
	}
	return l.netListener.Addr()
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
	// 使用独立的ServeMux,避免多个WsListener之间使用全局DefaultServeMux冲突
	mux := http.NewServeMux()
	mux.HandleFunc(l.config.Path, func(w http.ResponseWriter, r *http.Request) {
		l.serve(ctx, w, r)
	})
	l.upgrader = websocket.Upgrader{
		ReadBufferSize:  int(l.acceptConnectionConfig.RecvBufferSize),
		WriteBufferSize: int(l.acceptConnectionConfig.SendBufferSize),
		CheckOrigin:     checkOrigin,
	}
	// 先绑定端口,立即得到绑定结果,避免用time.Sleep猜测
	ln, err := net.Listen("tcp", listenAddress)
	if err != nil {
		logger.Error("Listen Failed %v: %v", l.GetListenerId(), err.Error())
		return false
	}
	l.netListener = ln
	logger.Debug("WsListener Start %v", l.GetListenerId())

	// 监听协程
	atomic.StoreInt32(&l.isRunning, 1)
	l.serveDone = make(chan struct{}, 1)
	l.httpServer = &http.Server{Handler: mux}
	go func() {
		var err error
		if l.config.CertFile != "" {
			err = l.httpServer.ServeTLS(ln, l.config.CertFile, l.config.KeyFile)
		} else {
			err = l.httpServer.Serve(ln)
		}
		if err != nil && err != http.ErrServerClosed {
			atomic.StoreInt32(&l.isRunning, 0)
			logger.Error("Serve failed %v: %v", l.GetListenerId(), err.Error())
		}
		l.serveDone <- struct{}{}
	}()

	// 关闭响应协程:同时监听ctx取消和Serve退出
	l.netMgrWg.Add(1)
	go func() {
		defer l.netMgrWg.Done()
		select {
		case <-ctx.Done():
			logger.Debug("recv closeNotify %v", l.GetListenerId())
			l.Close()
		case <-l.serveDone:
			// Serve异常退出,主动关闭
			if l.IsRunning() {
				l.Close()
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
	// 先通知业务层连接已建立,避免Start内部goroutine中的OnDisconnected先于OnConnectionConnected触发
	if l.handler != nil {
		l.handler.OnConnectionConnected(l, newTcpConn)
	}
	newTcpConn.Start(ctx, l.netMgrWg, func(connection Connection) {
		if l.handler != nil {
			l.handler.OnConnectionDisconnect(l, connection)
		}
		l.connectionMapLock.Lock()
		delete(l.connectionMap, connection.GetConnectionId())
		l.connectionMapLock.Unlock()
	})
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
