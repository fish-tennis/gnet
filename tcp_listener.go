package gnet

import (
	"context"
	"net"
	"sync"
	"syscall"
	"time"
)

// TCP监听
type TcpListener struct {
	baseListener

	netListener net.Listener
	acceptConnectionConfig ConnectionConfig
	acceptConnectionCodec Codec
	acceptConnectionHandler ConnectionHandler

	// 连接表
	connectionMap map[uint32]Connection
	connectionMapLock sync.RWMutex

	isRunning bool
	// 防止执行多次关闭操作
	closeOnce sync.Once
	// 关闭回调
	onClose func(listener Listener)

	acceptConnectionCreator AcceptConnectionCreator

	// 外部传进来的WaitGroup
	netMgrWg *sync.WaitGroup
}

func NewTcpListener(acceptConnectionConfig ConnectionConfig, acceptConnectionCodec Codec, acceptConnectionHandler ConnectionHandler, listenerHandler ListenerHandler) *TcpListener {
	return &TcpListener{
		baseListener: baseListener{
			listenerId: newListenerId(),
			handler: listenerHandler,
		},
		acceptConnectionConfig: acceptConnectionConfig,
		acceptConnectionCodec: acceptConnectionCodec,
		acceptConnectionHandler: acceptConnectionHandler,
		connectionMap: make(map[uint32]Connection),
	}
}

func (this *TcpListener) GetConnection(connectionId uint32) Connection {
	this.connectionMapLock.RLock()
	conn := this.connectionMap[connectionId]
	this.connectionMapLock.RUnlock()
	return conn
}

// 广播消息
func (this *TcpListener) Broadcast(packet Packet)  {
	this.connectionMapLock.RLock()
	for _,conn := range this.connectionMap {
		if conn.IsConnected() {
			conn.SendPacket(packet.Clone())
		}
	}
	this.connectionMapLock.RUnlock()
}

// 开启监听
func (this *TcpListener) Start(ctx context.Context, listenAddress string) bool {
	var err error
	this.netListener,err = net.Listen("tcp", listenAddress)
	if err != nil {
		logger.Error("Listen Failed %v: %v", this.GetListenerId(), err)
		return false
	}

	// 监听协程
	this.isRunning = true
	this.netMgrWg.Add(1)
	go func(ctx context.Context) {
		defer this.netMgrWg.Done()
		this.acceptLoop(ctx)
	}(ctx)

	// 关闭响应协程
	this.netMgrWg.Add(1)
	go func() {
		defer this.netMgrWg.Done()
		select {
		// 关闭通知
		case <-ctx.Done():
			logger.Debug("recv closeNotify %v", this.GetListenerId())
			this.Close()
		}
	}()

	return true
}

// 关闭监听,并关闭管理的连接
func (this *TcpListener) Close() {
	this.closeOnce.Do(func() {
		this.isRunning = false
		if this.netListener != nil {
			this.netListener.Close()
		}
		connMap := make(map[uint32]Connection)
		this.connectionMapLock.RLock()
		for k,v := range this.connectionMap {
			connMap[k] = v
		}
		this.connectionMapLock.RUnlock()
		// 关闭管理的连接
		for _,conn := range connMap {
			conn.Close()
		}
		if this.onClose != nil {
			this.onClose(this)
		}
	})
}

// accept协程
func (this *TcpListener) acceptLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("acceptLoop fatal %v: %v", this.GetListenerId(), err.(error))
			LogStack()
		}
	}()

	for this.isRunning {
		// 阻塞accept,当netListener关闭时,会返回err
		newConn,err := this.netListener.Accept()
		if err != nil {
			logger.Error("%v accept err:%v", this.GetListenerId(), err)
			if netError,ok := err.(net.Error); ok && netError.Temporary() {
				logger.Error("accept temporary err:%v", this.GetListenerId())
				time.Sleep(100*time.Millisecond)
				continue
			}
			// 有可能是因为open file数量限制 而导致的accept失败
			if err == syscall.EMFILE {
				logger.Error("accept failed id:%v syscall.EMFILE", this.GetListenerId())
				// 这个错误只是导致新连接暂时无法连接,不应该退出监听,当有连接释放后,新连接又可以连接上
				time.Sleep(100*time.Millisecond)
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
			newTcpConn := this.acceptConnectionCreator(newConn, &this.acceptConnectionConfig, this.acceptConnectionCodec, this.acceptConnectionHandler)
			if newTcpConn.GetHandler() != nil {
				newTcpConn.GetHandler().OnConnected(newTcpConn,true)
			}
			this.connectionMapLock.Lock()
			this.connectionMap[newTcpConn.GetConnectionId()] = newTcpConn
			this.connectionMapLock.Unlock()
			newTcpConn.Start(ctx, this.netMgrWg, func(connection Connection) {
				if this.handler != nil {
					this.handler.OnConnectionDisconnect(this, connection)
				}
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