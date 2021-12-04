package example

import (
	"context"
	"fmt"
	"github.com/fish-tennis/gnet"
	"testing"
	"time"
)

// 不使用protobuf的测试
func TestEchoData(t *testing.T) {
	defer func() {
		if err := recover(); err != nil {
			gnet.LogDebug("fatal %v", err.(error))
			gnet.LogStack()
		}
	}()

	// 10秒后触发关闭通知,所有监听<-ctx.Done()的地方会收到通知
	ctx,cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	netMgr := gnet.GetNetMgr()
	connectionConfig := gnet.ConnectionConfig{
		SendPacketCacheCap: 100,
		SendBufferSize:     60, // 设置的比较小,便于测试缓存写满的情况
		RecvBufferSize:     60,
		MaxPacketSize:      60,
		RecvTimeout:        0,
		HeartBeatInterval:  2,
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"
	//codec := gnet.NewXorCodec([]byte{0,1,2,3,4,5,6})
	codec := gnet.NewDefaultCodec()
	netMgr.NewListener(ctx, listenAddress, connectionConfig, codec, &echoServerHandler{}, &echoListenerHandler{})
	time.Sleep(time.Second)

	netMgr.NewConnector(ctx, listenAddress, connectionConfig, codec, &echoClientHandler{})

	netMgr.Shutdown(true)
}

// 监听接口
type echoListenerHandler struct {
	
}

func (e *echoListenerHandler) OnConnectionConnected(listener gnet.Listener, connection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionConnected %v", connection.GetConnectionId()))
}

func (e *echoListenerHandler) OnConnectionDisconnect(listener gnet.Listener, connection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionDisconnect %v", connection.GetConnectionId()))
}

// 服务端监听到的连接接口
type echoServerHandler struct {
}

func (e *echoServerHandler) OnConnected(connection gnet.Connection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Server OnConnected %v %v", connection.GetConnectionId(), success))
	if success {
		// 开一个协程,服务器自动给客户端发消息
		serialId := 0
		// 先连发10个数据包
		for i := 0; i < 10; i++ {
			serialId++
			packet := gnet.NewDataPacket([]byte(fmt.Sprintf("hello client %v", serialId)))
			connection.SendPacket(packet)
		}
		go func() {
			autoSendTimer := time.NewTimer(time.Second)
			for connection.IsConnected() {
				select {
				case <-autoSendTimer.C:
					serialId++
					packet := gnet.NewDataPacket([]byte(fmt.Sprintf("hello client %v", serialId)))
					connection.SendPacket(packet)
					autoSendTimer.Reset(time.Second)
				}
			}
		}()
	}
}

func (e *echoServerHandler) OnDisconnected(connection gnet.Connection ) {
	gnet.LogDebug(fmt.Sprintf("Server OnDisconnected %v", connection.GetConnectionId()))
}

func (e *echoServerHandler) OnRecvPacket(connection gnet.Connection, packet gnet.Packet) {
	gnet.LogDebug(fmt.Sprintf("Server OnRecvPacket %v: %v", connection.GetConnectionId(), string(packet.GetStreamData())))
}

func (e *echoServerHandler) CreateHeartBeatPacket(connection gnet.Connection, ) gnet.Packet { return nil }


// 客户端连接接口
type echoClientHandler struct {
	echoCount int
}

func (e *echoClientHandler) OnConnected(connection gnet.Connection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Client OnConnected %v %v", connection.GetConnectionId(), success))
}

func (e *echoClientHandler) OnDisconnected(connection gnet.Connection ) {
	gnet.LogDebug(fmt.Sprintf("Client OnDisconnected %v", connection.GetConnectionId()))
}

func (e *echoClientHandler) OnRecvPacket(connection gnet.Connection, packet gnet.Packet) {
	gnet.LogDebug(fmt.Sprintf("Client OnRecvPacket %v: %v", connection.GetConnectionId(), string(packet.GetStreamData())))
	e.echoCount++
	echoPacket := gnet.NewDataPacket([]byte(fmt.Sprintf("hello server %v", e.echoCount)))
	connection.SendPacket(echoPacket)
}

func (e *echoClientHandler) CreateHeartBeatPacket(connection gnet.Connection) gnet.Packet {
	return gnet.NewDataPacket([]byte("heartbeat"))
}
