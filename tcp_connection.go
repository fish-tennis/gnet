package gnet

import (
	"net"
	"time"
)

type TcpConnection struct {
	Connection
	conn net.Conn
}

//// 发送数据
//func (this* TcpConnection) Send(data []byte) bool {
//	return false
//}

// 连接
func (this *TcpConnection) Connect(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err == nil {
		this.isConnected = false
		if this.handler != nil {
			this.handler.OnConnected(false)
		}
		return false
	}
	this.conn = conn
	this.isConnected = true
	if this.handler != nil {
		this.handler.OnConnected(true)
	}
	return true
}

// 开启读写协程
func (this *TcpConnection) Start() {
	go func() {
		this.readLoop()
	}()

	go func() {
		this.writeLoop()
	}()
}

func (this *TcpConnection) readLoop() {

}

func (this *TcpConnection) writeLoop() {

}
