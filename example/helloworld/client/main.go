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
	addr   = flag.String("addr", "localhost:10001", "the address to connect to")
	logger = gnet.GetLogger()
)

func main() {
	flag.Parse()

	gnet.SetLogLevel(gnet.DebugLevel)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	clientCodec := gnet.NewProtoCodec(nil)
	clientHandler := gnet.NewDefaultConnectionHandler(clientCodec)
	// 客户端作为connector,需要设置心跳包
	clientHandler.RegisterHeartBeat(func() gnet.Packet {
		return gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatReq{
			Timestamp: time.Now().UnixMilli(),
		})
	})
	// 注册客户端的消息回调
	clientHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_HeartBeat), onHeartBeatRes, new(pb.HeartBeatRes))
	clientHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessage, new(pb.TestMessage))

	connectionConfig := gnet.DefaultConnectionConfig
	connectionConfig.Codec = clientCodec
	connectionConfig.Handler = clientHandler
	connector := gnet.GetNetMgr().NewConnector(ctx, *addr, &connectionConfig, nil)
	if connector == nil {
		panic("connect failed")
	}
	connector.SendPacket(gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
		&pb.TestMessage{
			Name: "hello",
		}))

	gnet.GetNetMgr().Shutdown(true)
}

// 收到心跳包回复
func onHeartBeatRes(connection gnet.Connection, packet gnet.Packet) {
	res := packet.Message().(*pb.HeartBeatRes)
	ping := res.ResponseTimestamp - res.RequestTimestamp
	logger.Debug(fmt.Sprintf("ping: %v ms", ping))
}

func onTestMessage(connection gnet.Connection, packet gnet.Packet) {
	res := packet.Message().(*pb.TestMessage)
	logger.Info(fmt.Sprintf("client onTestMessage: %v errCode:%v", res, packet.ErrorCode()))
}
