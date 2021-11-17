package gnet

import (
	"net"
	"sync"
)

// TCP监听
type TcpListener struct {
	baseListener

	netListener net.Listener
	acceptConnectionConfig ConnectionConfig
	acceptConnectionCodec Codec
	acceptConnectionHandler ConnectionHandler

	// 连接表
	connectionMap map[uint32]*TcpConnection
	connectionMapLock sync.RWMutex

	isRunning bool
	// 防止执行多次关闭操作
	closeOnce sync.Once
	// 关闭回调
	onClose func(listener Listener)

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
		connectionMap: make(map[uint32]*TcpConnection),
	}
}

// 开启监听
func (this *TcpListener) Start(listenAddress string, closeNotify chan struct{}) bool {
	var err error
	this.netListener,err = net.Listen("tcp", listenAddress)
	if err != nil {
		LogError("Listen Failed %v: %v", this.GetListenerId(), err)
		return false
	}

	// 监听协程
	this.isRunning = true
	this.netMgrWg.Add(1)
	go func() {
		defer this.netMgrWg.Done()
		this.acceptLoop(closeNotify)
	}()

	// 关闭响应协程
	this.netMgrWg.Add(1)
	go func() {
		defer this.netMgrWg.Done()
		select {
		case <-closeNotify:
			LogDebug("recv closeNotify %v", this.GetListenerId())
			this.Close()
		}
	}()

	return true
}

func (this *TcpListener) Close() {
	this.closeOnce.Do(func() {
		this.isRunning = false
		if this.netListener != nil {
			this.netListener.Close()
		}
		if this.onClose != nil {
			this.onClose(this)
		}
	})
}

// accept协程
func (this *TcpListener) acceptLoop(closeNotify chan struct{}) {
	defer func() {
		if err := recover(); err != nil {
			LogError("acceptLoop fatal %v: %v", this.GetListenerId(), err.(error))
			LogStack()
		}
	}()

	for this.isRunning {
		// 阻塞accept,当netListener关闭时,会返回err
		newConn,err := this.netListener.Accept()
		if err != nil {
			// TODO:检查哪些err 不需要break
			LogDebug("%v accept err:%v", this.GetListenerId(), err)
			break
		}
		this.netMgrWg.Add(1)
		go func() {
			defer func() {
				this.netMgrWg.Done()
				if err := recover(); err != nil {
					LogError("acceptLoop fatal %v: %v", this.GetListenerId(), err.(error))
					LogStack()
				}
			}()
			newTcpConn := NewTcpConnectionAccept(newConn, this.acceptConnectionConfig, this.acceptConnectionCodec, this.acceptConnectionHandler)
			newTcpConn.isConnected = true
			if newTcpConn.handler != nil {
				newTcpConn.handler.OnConnected(newTcpConn,true)
			}
			this.connectionMapLock.Lock()
			this.connectionMap[newTcpConn.GetConnectionId()] = newTcpConn
			this.connectionMapLock.Unlock()
			newTcpConn.netMgrWg = this.netMgrWg
			newTcpConn.Start(closeNotify)

			if this.handler != nil {
				this.handler.OnConnectionConnected(newTcpConn)
				newTcpConn.onClose = func(connection Connection) {
					this.handler.OnConnectionDisconnect(connection)
				}
			}
		}()
	}
}

