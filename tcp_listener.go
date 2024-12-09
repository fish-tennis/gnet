package gnet

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// tcp Listener
type TcpListener struct {
	baseListener

	netListener             net.Listener
	acceptConnectionConfig  ConnectionConfig
	acceptConnectionCodec   Codec
	acceptConnectionHandler ConnectionHandler
	// accept协程结束标记
	// notify chan for accept goroutine end
	acceptStopNotifyChan chan struct{}

	// manage the accepted connections
	connectionMap     map[uint32]Connection
	connectionMapLock sync.RWMutex

	isRunning int32
	closeOnce sync.Once
	// close callback
	onClose func(listener Listener)

	acceptConnectionCreator AcceptConnectionCreator

	// 外部传进来的WaitGroup
	netMgrWg *sync.WaitGroup
}

func NewTcpListener(listenerConfig *ListenerConfig) *TcpListener {
	return &TcpListener{
		baseListener: baseListener{
			listenerId: newListenerId(),
			config:     listenerConfig,
			handler:    listenerConfig.ListenerHandler,
		},
		acceptConnectionConfig:  listenerConfig.AcceptConfig,
		acceptConnectionCodec:   listenerConfig.AcceptConfig.Codec,
		acceptConnectionHandler: listenerConfig.AcceptConfig.Handler,
		acceptConnectionCreator: listenerConfig.AcceptConnectionCreator,
		connectionMap:           make(map[uint32]Connection),
		acceptStopNotifyChan:    make(chan struct{}, 1),
	}
}

func (l *TcpListener) GetConnection(connectionId uint32) Connection {
	l.connectionMapLock.RLock()
	conn := l.connectionMap[connectionId]
	l.connectionMapLock.RUnlock()
	return conn
}

// 广播消息
//
//	broadcast packet to accepted connections
func (l *TcpListener) Broadcast(packet Packet) {
	l.connectionMapLock.RLock()
	defer l.connectionMapLock.RUnlock()
	for _, conn := range l.connectionMap {
		if conn.IsConnected() {
			conn.SendPacket(packet.Clone())
		}
	}
}

// range for accepted connections
func (l *TcpListener) RangeConnections(f func(conn Connection) bool) {
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

// start goroutine
func (l *TcpListener) Start(ctx context.Context, listenAddress string) bool {
	var err error
	l.netListener, err = net.Listen("tcp", listenAddress)
	if err != nil {
		logger.Error("Listen Failed %v: %v", l.GetListenerId(), err.Error())
		return false
	}
	logger.Debug("TcpListener Start %v", l.GetListenerId())

	// 监听协程
	atomic.StoreInt32(&l.isRunning, 1)
	l.netMgrWg.Add(1)
	go func(ctx context.Context) {
		defer l.netMgrWg.Done()
		l.acceptLoop(ctx)
		l.acceptStopNotifyChan <- struct{}{}
		logger.Debug("acceptLoop end %v", l.GetListenerId())
	}(ctx)

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

			case <-l.acceptStopNotifyChan:
				logger.Debug("recv acceptStopNotify %v", l.GetListenerId())
				l.Close()
				return
			}
		}
	}()

	return true
}

// 关闭监听,并关闭管理的连接
//
//	close listen, close the accepted connections
func (l *TcpListener) Close() {
	l.closeOnce.Do(func() {
		atomic.StoreInt32(&l.isRunning, 0)
		if l.netListener != nil {
			l.netListener.Close()
		}
		connMap := make(map[uint32]Connection)
		l.connectionMapLock.RLock()
		for k, v := range l.connectionMap {
			connMap[k] = v
		}
		l.connectionMapLock.RUnlock()
		// 关闭管理的连接
		for _, conn := range connMap {
			conn.Close()
		}
		if l.onClose != nil {
			l.onClose(l)
		}
	})
}

func (l *TcpListener) IsRunning() bool {
	return atomic.LoadInt32(&l.isRunning) > 0
}

// accept goroutine
func (l *TcpListener) acceptLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("acceptLoop fatal %v: %v", l.GetListenerId(), err.(error))
			LogStack()
		}
	}()

	for l.IsRunning() {
		// 阻塞accept,当netListener关闭时,会返回err
		newConn, err := l.netListener.Accept()
		if err != nil {
			logger.Error("%v accept err:%v", l.GetListenerId(), err.Error())
			var netError net.Error
			if errors.As(err, &netError) && netError.Temporary() {
				logger.Error("accept temporary err:%v", l.GetListenerId())
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// 有可能是因为open file数量限制 而导致的accept失败
			if err == syscall.EMFILE {
				logger.Error("accept failed id:%v syscall.EMFILE", l.GetListenerId())
				// 这个错误只是导致新连接暂时无法连接,不应该退出监听,当有连接释放后,新连接又可以连接上
				time.Sleep(100 * time.Millisecond)
				continue
			}
			break
		}
		l.netMgrWg.Add(1)
		go func() {
			defer func() {
				l.netMgrWg.Done()
				if err := recover(); err != nil {
					logger.Error("acceptLoop fatal %v: %v", l.GetListenerId(), err.(error))
					LogStack()
				}
			}()
			newTcpConn := l.acceptConnectionCreator(newConn, &l.acceptConnectionConfig)
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
		}()
	}
}

// Addr returns the listener's network address.
func (l *TcpListener) Addr() net.Addr {
	if l.netListener == nil {
		return nil
	}
	return l.netListener.Addr()
}
