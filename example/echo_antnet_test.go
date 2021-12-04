package example

import (
	"context"
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
	"google.golang.org/protobuf/proto"
	"testing"
	"time"
)

// gnet模拟antnet的协议格式
func TestEchoAntnet(t *testing.T) {
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
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"

	protoMap := make(map[gnet.PacketCommand]gnet.ProtoMessageCreator)
	protoMap[gnet.PacketCommand(1<<8 + 2)] = func() proto.Message {
		return &pb.TestMessage{}
	}
	codec := gnet.NewAntnetCodec(false, protoMap)

	netMgr.NewListener(ctx, listenAddress, connectionConfig, codec, &antnetServerHandler{}, &antnetListenerHandler{})
	time.Sleep(time.Second)

	netMgr.NewConnector(ctx, listenAddress, connectionConfig, codec, &antnetClientHandler{})

	netMgr.Shutdown(true)
}

// 监听接口
type antnetListenerHandler struct {

}

func (e *antnetListenerHandler) OnConnectionConnected(listener gnet.Listener, acceptedConnection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionConnected %v", acceptedConnection.GetConnectionId()))
}

func (e *antnetListenerHandler) OnConnectionDisconnect(listener gnet.Listener, connection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionDisconnect %v", connection.GetConnectionId()))
}

// 服务端监听到的连接接口
type antnetServerHandler struct {
}

func (e *antnetServerHandler) CreateHeartBeatPacket(connection gnet.Connection) gnet.Packet {
	return nil
}

func (e *antnetServerHandler) OnConnected(connection gnet.Connection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Server OnConnected %v %v", connection.GetConnectionId(), success))
	if success {
		// 开一个协程,服务器自动给客户端发消息
		serialId := 0
		// 先连发10个数据包
		for i := 0; i < 10; i++ {
			serialId++
			packet := gnet.NewAntnetPacket(1, 2, 0,
				&pb.TestMessage{
					Name: fmt.Sprintf("hello client %v", serialId),
					I32: int32(serialId),
				})
			connection.SendPacket(packet)
		}
		go func() {
			autoSendTimer := time.NewTimer(time.Second)
			for connection.IsConnected() {
				select {
				case <-autoSendTimer.C:
					serialId++
					packet := gnet.NewAntnetPacket(1, 2, 0,
						&pb.TestMessage{
							Name: fmt.Sprintf("hello client %v", serialId),
							I32: int32(serialId),
						})
					connection.SendPacket(packet)
					autoSendTimer.Reset(time.Second)
				}
			}
		}()
	}
}

func (e *antnetServerHandler) OnDisconnected(connection gnet.Connection ) {
	gnet.LogDebug(fmt.Sprintf("Server OnDisconnected %v", connection.GetConnectionId()))
}

func (e *antnetServerHandler) OnRecvPacket(connection gnet.Connection, packet gnet.Packet) {
	antnetPacket := packet.(*gnet.AntnetPacket)
	recvMessage := antnetPacket.Message().(*pb.TestMessage)
	gnet.LogDebug(fmt.Sprintf("Server OnRecvPacket %v: cmd:%v act:%v msg:%v", connection.GetConnectionId(),
		antnetPacket.Cmd(), antnetPacket.Act(), recvMessage))
}


// 客户端连接接口
type antnetClientHandler struct {
	echoCount int
}

func (e *antnetClientHandler) CreateHeartBeatPacket(connection gnet.Connection) gnet.Packet {
	return nil
}

func (e *antnetClientHandler) OnConnected(connection gnet.Connection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Client OnConnected %v %v", connection.GetConnectionId(), success))
}

func (e *antnetClientHandler) OnDisconnected(connection gnet.Connection ) {
	gnet.LogDebug(fmt.Sprintf("Client OnDisconnected %v", connection.GetConnectionId()))
}

func (e *antnetClientHandler) OnRecvPacket(connection gnet.Connection, packet gnet.Packet) {
	antnetPacket := packet.(*gnet.AntnetPacket)
	recvMessage := antnetPacket.Message().(*pb.TestMessage)
	gnet.LogDebug(fmt.Sprintf("Client OnRecvPacket %v: cmd:%v act:%v msg:%v", connection.GetConnectionId(),
		antnetPacket.Cmd(), antnetPacket.Act(), recvMessage))
	e.echoCount++
	echoPacket := gnet.NewAntnetPacket(1, 2, 0,
		&pb.TestMessage{
			Name: fmt.Sprintf("hello server %v", e.echoCount),
			I32: int32(e.echoCount),
		})
	connection.SendPacket(echoPacket)
}
