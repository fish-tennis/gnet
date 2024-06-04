package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
)

var (
	port   = flag.Int("port", 10001, "The server port")
	logger = gnet.GetLogger()
)

func main() {
	flag.Parse()

	gnet.SetLogLevel(gnet.DebugLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := gnet.NewProtoCodec(nil)
	serverHandler := gnet.NewDefaultConnectionHandler(serverCodec)
	// 注册服务器的消息回调
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_HelloRequest), onHelloRequest, new(pb.HelloRequest))

	listenerConfig := &gnet.ListenerConfig{
		AcceptConfig: gnet.DefaultConnectionConfig,
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
	logger.Info("request:%v", request)
	replyPacket := gnet.NewProtoPacketEx(pb.CmdTest_Cmd_HelloReply, &pb.HelloReply{
		Message: request.Name + " from server's reply",
	}).WithRpc(packet)
	connection.SendPacket(replyPacket)
}
