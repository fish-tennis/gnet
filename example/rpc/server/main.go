package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
)

var (
	port = flag.Int("port", 10001, "The server port")
)

func main() {
	flag.Parse()

	gnet.SetLogLevel(gnet.DebugLevel)
	// 10秒后触发关闭通知,所有监听<-ctx.Done()的地方会收到通知
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := gnet.NewProtoCodec(nil)
	serverHandler := gnet.NewDefaultConnectionHandler(serverCodec)
	// 注册服务器的消息回调
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_HelloRequest), onHelloRequest, new(pb.HelloRequest))

	connectionConfig := gnet.ConnectionConfig{
		SendPacketCacheCap: 100,
		SendBufferSize:     60, // 设置的比较小,便于测试缓存写满的情况
		RecvBufferSize:     60,
		MaxPacketSize:      60,
		RecvTimeout:        0,
		HeartBeatInterval:  3,
		WriteTimeout:       0,
	}
	listenerConfig := &gnet.ListenerConfig{
		AcceptConfig: connectionConfig,
	}
	listenerConfig.AcceptConfig.Codec = serverCodec
	listenerConfig.AcceptConfig.Handler = serverHandler
	if gnet.GetNetMgr().NewListener(ctx, fmt.Sprintf(":%d", *port), listenerConfig) == nil {
		panic("listen failed")
	}

	gnet.GetNetMgr().Shutdown(true)
}

func onHelloRequest(connection gnet.Connection, packet gnet.Packet) {
	request := packet.Message().(*pb.HelloRequest)
	replyPacket := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_HelloReply), &pb.HelloReply{
		Message: request.Name + " from server's reply",
	})
	replyPacket.SetRpcCallId(packet.(*gnet.ProtoPacket).RpcCallId())
	connection.SendPacket(replyPacket)
}
