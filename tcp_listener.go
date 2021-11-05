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
	connectionMap map[uint]*TcpConnection
	connectionMapLock sync.RWMutex

	isRunning bool
}

func NewTcpListener(acceptConnectionConfig ConnectionConfig, acceptConnectionHandler ConnectionHandler) *TcpListener {
	return &TcpListener{
		acceptConnectionConfig: acceptConnectionConfig,
		acceptConnectionHandler: acceptConnectionHandler,
		connectionMap: make(map[uint]*TcpConnection),
	}
}

// 开启监听
func (this *TcpListener) Start(listenAddress string, closeNotify chan struct{}) bool {
	var err error
	this.netListener,err = net.Listen("tcp", listenAddress)
	if err != nil {
		return false
	}

	// 监听协程
	this.isRunning = true
	go func() {
		this.acceptLoop()
	}()

	// 关闭响应协程
	go func() {
		select {
		case <-closeNotify:
			this.Close()
		}
	}()

	return true
}

// accept协程
func (this *TcpListener) acceptLoop() {
	for this.isRunning {
		newConn,err := this.netListener.Accept()
		if err != nil {
			continue
		}
		go func() {
			newTcpConn := NewTcpConnectionAccept(newConn, this.acceptConnectionConfig, this.acceptConnectionHandler)
			newTcpConn.isConnected = true
			if newTcpConn.handler != nil {
				newTcpConn.handler.OnConnected(true)
			}
			this.connectionMapLock.Lock()
			this.connectionMap[newTcpConn.GetConnectionId()] = newTcpConn
			this.connectionMapLock.Unlock()
		}()
	}
}

func (this *TcpListener) Close() {
	this.isRunning = false
	this.netListener.Close()
}
