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
	logger = gnet.GetLogger()
)

func main() {
	flag.Parse()

	gnet.SetLogLevel(gnet.DebugLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	netMgr := gnet.GetNetMgr()

	serverCodec := gnet.NewProtoCodec(nil)
	serverHandler := gnet.NewDefaultConnectionHandler(serverCodec)
	// 注册服务器的消息回调
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_HeartBeat), onHeartBeatReq, new(pb.HeartBeatReq))
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessage, new(pb.TestMessage))
	serverHandler.SetOnConnectedFunc(func(connection gnet.Connection, success bool) {
		logger.Info("OnConnected:%v success:%v", connection, success)
		return
	})
	serverHandler.SetUnRegisterHandler(func(connection gnet.Connection, packet gnet.Packet) {
		logger.Info("UnRegister:%v", packet)
	})

	listenerConfig := &gnet.ListenerConfig{
		AcceptConfig: gnet.DefaultConnectionConfig,
	}
	listenerConfig.AcceptConfig.Codec = serverCodec
	listenerConfig.AcceptConfig.Handler = serverHandler
	if netMgr.NewListener(ctx, fmt.Sprintf(":%v", *port), listenerConfig) == nil {
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

	// 再模拟一个返回错误码的消息
	connection.SendPacket(gnet.NewProtoPacketEx(packet.Command()).SetErrorCode(10001))
}
