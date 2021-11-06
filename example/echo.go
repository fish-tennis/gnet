package main

import (
	"fmt"
	"github.com/gnet"
	"sync"
	"time"
)

func main() {
	netMgr := gnet.GetNetMgr()
	connectionConfig := gnet.ConnectionConfig{
		SendBufferSize: 100,
		RecvTimeout: 5,
		WriteTimeout: 1,
	}
	listenAddress := "127.0.0.1:10002"
	echoServerHandler := &EchoServerHandler{}
	netMgr.NewListener(listenAddress, connectionConfig, echoServerHandler)
	time.Sleep(time.Second)

	echoClientHandler := &EchoClientHandler{}
	netMgr.NewConnector(listenAddress, connectionConfig, echoClientHandler)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	exitTimer := time.NewTimer(10*time.Second)
	select {
	case <-exitTimer.C:
		gnet.LogDebug("test timeout")
		wg.Done()
	}
	wg.Wait()
	netMgr.Shutdown()
}

type EchoServerHandler struct {
}

func (e EchoServerHandler) OnConnected(connection gnet.IConnection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Server OnConnected %v %v", connection.GetConnectionId(), success))
	if success {
		// 开一个线程,服务器自动给客户端发消息
		go func() {
			autoSendTimer := time.NewTimer(time.Second)
			serialId := 0
			for connection.IsConnected() {
				select {
				case <-autoSendTimer.C:
					serialId++
					sendData := fmt.Sprintf("hello client %v", serialId)
					connection.Send([]byte(sendData))
					autoSendTimer.Reset(time.Second)
				}
			}
		}()
	}
}

func (e EchoServerHandler) OnDisconnected(connection gnet.IConnection, ) {
	gnet.LogDebug(fmt.Sprintf("Server OnDisconnected %v", connection.GetConnectionId()))
}

func (e EchoServerHandler) OnRecvMessage(connection gnet.IConnection, data []byte) {
	gnet.LogDebug(fmt.Sprintf("Server OnRecvMessage %v: %v", connection.GetConnectionId(), string(data)))
}

type EchoClientHandler struct {
}

func (e EchoClientHandler) OnConnected(connection gnet.IConnection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Client OnConnected %v %v", connection.GetConnectionId(), success))
}

func (e EchoClientHandler) OnDisconnected(connection gnet.IConnection, ) {
	gnet.LogDebug(fmt.Sprintf("Client OnDisconnected %v", connection.GetConnectionId()))
}

func (e EchoClientHandler) OnRecvMessage(connection gnet.IConnection, data []byte) {
	gnet.LogDebug(fmt.Sprintf("Client OnRecvMessage %v: %v", connection.GetConnectionId(), string(data)))
	sendData := "hello server"
	connection.Send([]byte(sendData))
}
