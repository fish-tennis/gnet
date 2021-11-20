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

// 测试protobuf
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
		MaxPacketSize:      60,
		RecvTimeout:        0,
		HeartBeatInterval:  3,
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"

	protoMap := make(map[gnet.PacketCommand]gnet.ProtoMessageCreator)
	protoMap[gnet.PacketCommand(1)] = func() proto.Message {
		return &pb.HeartBeatRequest{}
	}
	protoMap[gnet.PacketCommand(123)] = func() proto.Message {
		return &pb.TestMessage{}
	}
	//codec := gnet.NewProtoCodec(protoMap)
	codec := gnet.NewXorProtoCodec([]byte("xor_test_key"), protoMap)
	netMgr.NewListener(listenAddress, connectionConfig, codec, &echoProtoServerHandler{}, &echoProtoListenerHandler{})
	time.Sleep(time.Second)

	netMgr.NewConnector(listenAddress, connectionConfig, codec, &echoProtoClientHandler{})

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
type echoProtoListenerHandler struct {
	
}

func (e *echoProtoListenerHandler) OnConnectionConnected(connection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionConnected %v", connection.GetConnectionId()))
}

func (e *echoProtoListenerHandler) OnConnectionDisconnect(connection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionDisconnect %v", connection.GetConnectionId()))
}

// 服务端监听到的连接接口
type echoProtoServerHandler struct {
}

func (e *echoProtoServerHandler) CreateHeartBeatPacket() gnet.Packet {
	return nil
}

func (e *echoProtoServerHandler) OnConnected(connection gnet.Connection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Server OnConnected %v %v", connection.GetConnectionId(), success))
	if success {
		// 开一个协程,服务器自动给客户端发消息
		serialId := 0
		// 先连发10个数据包
		for i := 0; i < 10; i++ {
			serialId++
			packet := gnet.NewProtoPacket(gnet.PacketCommand(123),
				&pb.TestMessage{
				Name: fmt.Sprintf("hello client %v", serialId),
				I32: int32(serialId),
				})
			connection.Send(packet)
		}
		go func() {
			autoSendTimer := time.NewTimer(time.Second)
			for connection.IsConnected() {
				select {
				case <-autoSendTimer.C:
					serialId++
					packet := gnet.NewProtoPacket(gnet.PacketCommand(123),
						&pb.TestMessage{
							Name: fmt.Sprintf("hello client %v", serialId),
							I32: int32(serialId),
						})
					connection.Send(packet)
					autoSendTimer.Reset(time.Second)
				}
			}
		}()
	}
}

func (e *echoProtoServerHandler) OnDisconnected(connection gnet.Connection ) {
	gnet.LogDebug(fmt.Sprintf("Server OnDisconnected %v", connection.GetConnectionId()))
}

func (e *echoProtoServerHandler) OnRecvPacket(connection gnet.Connection, packet gnet.Packet) {
	protoPacket := packet.(*gnet.ProtoPacket)
	if packet.Command() == 123 {
		recvMessage := protoPacket.Message().(*pb.TestMessage)
		gnet.LogDebug(fmt.Sprintf("Server OnRecvPacket %v: %v", connection.GetConnectionId(), recvMessage))
	}
}


// 客户端连接接口
type echoProtoClientHandler struct {
	echoCount int
}

func (e *echoProtoClientHandler) CreateHeartBeatPacket() gnet.Packet {
	return gnet.NewProtoPacket(gnet.PacketCommand(1),
		&pb.HeartBeatRequest{
			Timestamp: time.Now().UnixNano()/int64(time.Microsecond),
		})
}

func (e *echoProtoClientHandler) OnConnected(connection gnet.Connection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Client OnConnected %v %v", connection.GetConnectionId(), success))
}

func (e *echoProtoClientHandler) OnDisconnected(connection gnet.Connection ) {
	gnet.LogDebug(fmt.Sprintf("Client OnDisconnected %v", connection.GetConnectionId()))
}

func (e *echoProtoClientHandler) OnRecvPacket(connection gnet.Connection, packet gnet.Packet) {
	protoPacket := packet.(*gnet.ProtoPacket)
	recvMessage := protoPacket.Message().(*pb.TestMessage)
	gnet.LogDebug(fmt.Sprintf("Client OnRecvPacket %v: %v", connection.GetConnectionId(), recvMessage))
	e.echoCount++
	echoPacket := gnet.NewProtoPacket(gnet.PacketCommand(123),
		&pb.TestMessage{
		Name: fmt.Sprintf("hello server %v", e.echoCount),
		I32: int32(e.echoCount),
		})
	connection.Send(echoPacket)
}
