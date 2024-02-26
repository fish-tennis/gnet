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

func (this *TcpListener) GetConnection(connectionId uint32) Connection {
	this.connectionMapLock.RLock()
	conn := this.connectionMap[connectionId]
	this.connectionMapLock.RUnlock()
	return conn
}

// 广播消息
//
//	broadcast packet to accepted connections
func (this *TcpListener) Broadcast(packet Packet) {
	this.connectionMapLock.RLock()
	defer this.connectionMapLock.RUnlock()
	for _, conn := range this.connectionMap {
		if conn.IsConnected() {
			conn.SendPacket(packet.Clone())
		}
	}
}

// range for accepted connections
func (this *TcpListener) RangeConnections(f func(conn Connection) bool) {
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

// start goroutine
func (this *TcpListener) Start(ctx context.Context, listenAddress string) bool {
	var err error
	this.netListener, err = net.Listen("tcp", listenAddress)
	if err != nil {
		logger.Error("Listen Failed %v: %v", this.GetListenerId(), err.Error())
		return false
	}
	logger.Debug("TcpListener Start %v", this.GetListenerId())

	// 监听协程
	atomic.StoreInt32(&this.isRunning, 1)
	this.netMgrWg.Add(1)
	go func(ctx context.Context) {
		defer this.netMgrWg.Done()
		this.acceptLoop(ctx)
		this.acceptStopNotifyChan <- struct{}{}
		logger.Debug("acceptLoop end %v", this.GetListenerId())
	}(ctx)

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

			case <-this.acceptStopNotifyChan:
				logger.Debug("recv acceptStopNotify %v", this.GetListenerId())
				this.Close()
				return
			}
		}
	}()

	return true
}

// 关闭监听,并关闭管理的连接
//
//	close listen, close the accepted connections
func (this *TcpListener) Close() {
	this.closeOnce.Do(func() {
		atomic.StoreInt32(&this.isRunning, 0)
		if this.netListener != nil {
			this.netListener.Close()
		}
		connMap := make(map[uint32]Connection)
		this.connectionMapLock.RLock()
		for k, v := range this.connectionMap {
			connMap[k] = v
		}
		this.connectionMapLock.RUnlock()
		// 关闭管理的连接
		for _, conn := range connMap {
			conn.Close()
		}
		if this.onClose != nil {
			this.onClose(this)
		}
	})
}

func (this *TcpListener) IsRunning() bool {
	return atomic.LoadInt32(&this.isRunning) > 0
}

// accept goroutine
func (this *TcpListener) acceptLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("acceptLoop fatal %v: %v", this.GetListenerId(), err.(error))
			LogStack()
		}
	}()

	for this.IsRunning() {
		// 阻塞accept,当netListener关闭时,会返回err
		newConn, err := this.netListener.Accept()
		if err != nil {
			logger.Error("%v accept err:%v", this.GetListenerId(), err.Error())
			var netError net.Error
			if errors.As(err, &netError) && netError.Temporary() {
				logger.Error("accept temporary err:%v", this.GetListenerId())
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// 有可能是因为open file数量限制 而导致的accept失败
			if err == syscall.EMFILE {
				logger.Error("accept failed id:%v syscall.EMFILE", this.GetListenerId())
				// 这个错误只是导致新连接暂时无法连接,不应该退出监听,当有连接释放后,新连接又可以连接上
				time.Sleep(100 * time.Millisecond)
				continue
			}
			break
		}
		this.netMgrWg.Add(1)
		go func() {
			defer func() {
				this.netMgrWg.Done()
				if err := recover(); err != nil {
					logger.Error("acceptLoop fatal %v: %v", this.GetListenerId(), err.(error))
					LogStack()
				}
			}()
			newTcpConn := this.acceptConnectionCreator(newConn, &this.acceptConnectionConfig)
			if newTcpConn.GetHandler() != nil {
				newTcpConn.GetHandler().OnConnected(newTcpConn, true)
			}
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
		}()
	}
}

// Addr returns the listener's network address.
func (this *TcpListener) Addr() net.Addr {
	if this.netListener == nil {
		return nil
	}
	return this.netListener.Addr()
}
