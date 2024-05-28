package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
	"time"
)

var (
	port   = flag.Int("port", 10001, "The server port")
	useWss = flag.Bool("wss", false, "whether use wss")
	cert   = flag.String("cert", "", "cert.pem file")
	key    = flag.String("key", "", "key.pem file")
	logger = gnet.GetLogger()
)

func main() {
	flag.Parse()

	gnet.SetLogLevel(gnet.DebugLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	netMgr := gnet.GetNetMgr()

	serverCodec := gnet.NewSimpleProtoCodec()
	serverHandler := gnet.NewDefaultConnectionHandler(serverCodec)
	// 注册服务器的消息回调
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_HeartBeat), onHeartBeatReq, new(pb.HeartBeatReq))
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessage, new(pb.TestMessage))
	serverHandler.SetUnRegisterHandler(func(connection gnet.Connection, packet gnet.Packet) {
		logger.Info("UnRegister:%v", packet)
	})

	protocolName := "ws"
	if *useWss {
		protocolName = "wss"
	}
	listenerConfig := &gnet.ListenerConfig{
		AcceptConfig: gnet.DefaultConnectionConfig,
		Path:         "/" + protocolName,
		CertFile:     *cert,
		KeyFile:      *key,
	}
	listenerConfig.AcceptConfig.Codec = serverCodec
	listenerConfig.AcceptConfig.Handler = serverHandler
	if netMgr.NewWsListener(ctx, fmt.Sprintf(":%v", *port), listenerConfig) == nil {
		panic("listen failed")
	}

	netMgr.Shutdown(true)
}

// 服务器收到客户端的心跳包
func onHeartBeatReq(connection gnet.Connection, packet gnet.Packet) {
	req := packet.Message().(*pb.HeartBeatReq)
	logger.Debug(fmt.Sprintf("Server onHeartBeatReq: %v", req))
	// 模拟网络延迟
	time.Sleep(time.Millisecond * 10)
	connection.Send(gnet.PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatRes{
		RequestTimestamp:  req.GetTimestamp(),
		ResponseTimestamp: time.Now().UnixMilli(),
	})
}

// 服务器收到客户端的TestMessage
func onTestMessage(connection gnet.Connection, packet gnet.Packet) {
	req := packet.Message().(*pb.TestMessage)
	logger.Debug(fmt.Sprintf("Server onTestMessage: %v", req))
	// echo
	req.Name += " world from server"
	connection.SendPacket(packet)
}
