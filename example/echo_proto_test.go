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

// 测试protobuf
func TestEchoProto(t *testing.T) {
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
		HeartBeatInterval:  3,
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"

	serverCodec := gnet.NewProtoCodec(nil)
	//serverCodec := gnet.NewXorProtoCodec([]byte("xor_test_key"), nil)
	serverHandler := &echoProtoServerHandler{
		DefaultConnectionHandler: *gnet.NewDefaultConnectionHandler(serverCodec),
	}
	// 注册服务器的消息回调
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_HeartBeat), onHeartBeatReq, func() proto.Message {
		return &pb.HeartBeatReq{}
	})
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessageServer, func() proto.Message {
		return &pb.TestMessage{}
	})
	if netMgr.NewListener(ctx, listenAddress, connectionConfig, serverCodec, serverHandler, nil) == nil {
		panic("listen failed")
	}
	time.Sleep(time.Second)

	clientCodec := gnet.NewProtoCodec(nil)
	//clientCodec := gnet.NewXorProtoCodec([]byte("xor_test_key"), nil)
	clientHandler := &echoProtoClientHandler{
		DefaultConnectionHandler: *gnet.NewDefaultConnectionHandler(clientCodec),
	}
	// 客户端作为connector,需要设置心跳包
	clientHandler.RegisterHeartBeat(gnet.PacketCommand(pb.CmdTest_Cmd_HeartBeat), func() proto.Message {
		return &pb.HeartBeatReq{}
	})
	// 注册客户端的消息回调
	clientHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_HeartBeat), clientHandler.onHeartBeatRes, func() proto.Message {
		return &pb.HeartBeatRes{}
	})
	clientHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), clientHandler.onTestMessage, func() proto.Message {
		return &pb.TestMessage{}
	})
	if netMgr.NewConnector(ctx, listenAddress, connectionConfig, clientCodec, clientHandler, nil) == nil {
		panic("connect failed")
	}

	netMgr.Shutdown(true)
}


// 服务端监听到的连接接口
type echoProtoServerHandler struct {
	gnet.DefaultConnectionHandler
}

func (e *echoProtoServerHandler) OnConnected(connection gnet.Connection, success bool) {
	gnet.LogDebug(fmt.Sprintf("Server OnConnected %v %v", connection.GetConnectionId(), success))
	if success {
		// 开一个协程,服务器自动给客户端发消息
		serialId := 0
		// 先连发10个数据包
		for i := 0; i < 10; i++ {
			serialId++
			packet := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
				&pb.TestMessage{
				Name: fmt.Sprintf("hello client %v", serialId),
				I32: int32(serialId),
				})
			connection.SendPacket(packet)
		}
		// 每隔1秒 发一个包
		go func() {
			autoSendTimer := time.NewTimer(time.Second)
			for connection.IsConnected() {
				select {
				case <-autoSendTimer.C:
					serialId++
					packet := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
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

// 服务器收到客户端的心跳包
func onHeartBeatReq(connection gnet.Connection, packet *gnet.ProtoPacket) {
	req := packet.Message().(*pb.HeartBeatReq)
	gnet.LogDebug(fmt.Sprintf("Server onHeartBeatReq: %v", req))
	connection.Send( gnet.PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatRes{
		RequestTimestamp: req.GetTimestamp(),
		ResponseTimestamp: time.Now().UnixNano()/int64(time.Microsecond),
	} )
}

// 服务器收到客户端的TestMessage
func onTestMessageServer(connection gnet.Connection, packet *gnet.ProtoPacket) {
	req := packet.Message().(*pb.TestMessage)
	gnet.LogDebug(fmt.Sprintf("Server onTestMessage: %v", req))
}


// 客户端连接接口
type echoProtoClientHandler struct {
	gnet.DefaultConnectionHandler
	echoCount int
}

// 收到心跳包回复
func (e *echoProtoClientHandler) onHeartBeatRes(connection gnet.Connection, packet *gnet.ProtoPacket) {
	res := packet.Message().(*pb.HeartBeatRes)
	gnet.LogDebug(fmt.Sprintf("client onHeartBeatRes: %v", res))
}

func (e *echoProtoClientHandler) onTestMessage(connection gnet.Connection, packet *gnet.ProtoPacket) {
	res := packet.Message().(*pb.TestMessage)
	gnet.LogDebug(fmt.Sprintf("client onTestMessage: %v", res))
	e.echoCount++
	connection.Send(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
		&pb.TestMessage{
			Name: fmt.Sprintf("hello server %v", e.echoCount),
			I32: int32(e.echoCount),
		})
}
