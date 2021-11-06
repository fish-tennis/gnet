package gnet

import (
	"net"
	"sync"
)

// TCP监听
type TcpListener struct {
	Listener

	netListener net.Listener
	acceptConnectionConfig ConnectionConfig
	acceptConnectionHandler ConnectionHandler

	// 连接表
	connectionMap map[uint32]*TcpConnection
	connectionMapLock sync.RWMutex

	isRunning bool
}

func NewTcpListener(acceptConnectionConfig ConnectionConfig, acceptConnectionHandler ConnectionHandler) *TcpListener {
	return &TcpListener{
		Listener:Listener{
			listenerId: newListenerId(),
		},
		acceptConnectionConfig: acceptConnectionConfig,
		acceptConnectionHandler: acceptConnectionHandler,
		connectionMap: make(map[uint32]*TcpConnection),
	}
}

// 开启监听
func (this *TcpListener) Start(listenAddress string, closeNotify chan struct{}) bool {
	var err error
	this.netListener,err = net.Listen("tcp", listenAddress)
	if err != nil {
		LogDebug("Listen Failed %v: %v", this.GetListenerId(), err)
		return false
	}

	// 监听协程
	this.isRunning = true
	go func() {
		this.acceptLoop(closeNotify)
	}()

	// 关闭响应协程
	go func() {
		select {
		case <-closeNotify:
			LogDebug("recv closeNotify %v", this.GetListenerId())
			this.Close()
		}
	}()

	return true
}

func (this *TcpListener) Close() {
	this.isRunning = false
	this.netListener.Close()
}

// accept协程
func (this *TcpListener) acceptLoop(closeNotify chan struct{}) {
	for this.isRunning {
		// 阻塞accept,当netListener关闭时,会返回err
		newConn,err := this.netListener.Accept()
		if err != nil {
			// TODO:检查哪些err 不需要break
			LogDebug("%v accept err:%v", this.GetListenerId(), err)
			break
		}
		go func() {
			newTcpConn := NewTcpConnectionAccept(newConn, this.acceptConnectionConfig, this.acceptConnectionHandler)
			newTcpConn.isConnected = true
			if newTcpConn.handler != nil {
				newTcpConn.handler.OnConnected(newTcpConn,true)
			}
			this.connectionMapLock.Lock()
			this.connectionMap[newTcpConn.GetConnectionId()] = newTcpConn
			this.connectionMapLock.Unlock()
			newTcpConn.Start(closeNotify)
		}()
	}
}

