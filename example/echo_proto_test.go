package example

import (
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
	"google.golang.org/protobuf/proto"
	"sync"
	"testing"
	"time"
)

func TestEchoProto(t *testing.T) {
	defer func() {
		if err := recover(); err != nil {
			gnet.LogDebug("fatal %v", err.(error))
			gnet.LogStack()
		}
	}()

	netMgr := gnet.GetNetMgr()
	connectionConfig := gnet.ConnectionConfig{
		SendPacketCacheCap: 100,
		SendBufferSize:     60, // 设置的比较小,便于测试缓存写满的情况
		RecvBufferSize:     60,
		MaxPacketSize:      1024,
		RecvTimeout:        0,
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"

	protoMap := make(map[gnet.PacketCommand]gnet.ProtoMessageCreator)
	protoMap[gnet.PacketCommand(123)] = func() proto.Message {
		return &pb.TestMessage{}
	}
	codec := gnet.NewProtoCodec(protoMap)
	netMgr.NewListener(listenAddress, connectionConfig, codec, &EchoProtoServerHandler{}, &EchoListenerHandler{})
	time.Sleep(time.Second)

	netMgr.NewConnector(listenAddress, connectionConfig, codec, &EchoProtoClientHandler{})

	wg := &sync.WaitGroup{}
	wg.Add(1)
	exitTimer := time.NewTimer(10*time.Second)
	select {
	case <-exitTimer.C:
		gnet.LogDebug("test timeout")
		wg.Done()
	}
	wg.Wait()
	netMgr.Shutdown(true)
}

// 监听接口
type EchoProtoListenerHandler struct {
	
}

func (e *EchoProtoListenerHandler) OnConnectionConnected(connection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionConnected %v", connection.GetConnectionId()))
}

func (e *EchoProtoListenerHandler) OnConnectionDisconnect(connection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionDisconnect %v", connection.GetConnectionId()))
}

// 服务端监听到的连接接口
type EchoProtoServerHandler struct {
}

func (e *EchoProtoServerHandler) OnConnected(connection gnet.Connection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Server OnConnected %v %v", connection.GetConnectionId(), success))
	if success {
		// 开一个协程,服务器自动给客户端发消息
		serialId := 0
		// 先连发10个数据包
		for i := 0; i < 10; i++ {
			serialId++
			//packet := gnet.NewDataPacket([]byte(fmt.Sprintf("hello client %v", serialId)))
			//connection.Send(packet)
			packet := gnet.NewProtoPacket(gnet.PacketCommand(123),
				&pb.TestMessage{
				Name: fmt.Sprintf("hello client %v", serialId),
				I32: int32(serialId),
				})
			connection.SendProto(packet)
		}
		go func() {
			autoSendTimer := time.NewTimer(time.Second)
			for connection.IsConnected() {
				select {
				case <-autoSendTimer.C:
					serialId++
					//packet := gnet.NewDataPacket([]byte(fmt.Sprintf("hello client %v", serialId)))
					//connection.Send(packet)
					packet := gnet.NewProtoPacket(gnet.PacketCommand(123),
						&pb.TestMessage{
							Name: fmt.Sprintf("hello client %v", serialId),
							I32: int32(serialId),
						})
					connection.SendProto(packet)
					autoSendTimer.Reset(time.Second)
				}
			}
		}()
	}
}

func (e *EchoProtoServerHandler) OnDisconnected(connection gnet.Connection ) {
	gnet.LogDebug(fmt.Sprintf("Server OnDisconnected %v", connection.GetConnectionId()))
}

func (e *EchoProtoServerHandler) OnRecvPacket(connection gnet.Connection, packet gnet.Packet) {
	protoPacket := packet.(*gnet.ProtoPacket)
	recvMessage := protoPacket.Message().(*pb.TestMessage)
	gnet.LogDebug(fmt.Sprintf("Server OnRecvPacket %v: %v", connection.GetConnectionId(), recvMessage.GetName()))
}


// 客户端连接接口
type EchoProtoClientHandler struct {
	echoCount int
}

func (e *EchoProtoClientHandler) OnConnected(connection gnet.Connection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Client OnConnected %v %v", connection.GetConnectionId(), success))
}

func (e *EchoProtoClientHandler) OnDisconnected(connection gnet.Connection ) {
	gnet.LogDebug(fmt.Sprintf("Client OnDisconnected %v", connection.GetConnectionId()))
}

func (e *EchoProtoClientHandler) OnRecvPacket(connection gnet.Connection, packet gnet.Packet) {
	protoPacket := packet.(*gnet.ProtoPacket)
	recvMessage := protoPacket.Message().(*pb.TestMessage)
	gnet.LogDebug(fmt.Sprintf("Client OnRecvPacket %v: %v", connection.GetConnectionId(), recvMessage.GetName()))
	e.echoCount++
	//echoPacket := gnet.NewDataPacket([]byte(fmt.Sprintf("hello server %v", e.echoCount)))
	//connection.Send(echoPacket)
	echoPacket := gnet.NewProtoPacket(gnet.PacketCommand(123),
		&pb.TestMessage{
		Name: fmt.Sprintf("hello server %v", e.echoCount),
		I32: int32(e.echoCount),
		})
	connection.SendProto(echoPacket)
}
