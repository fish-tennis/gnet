package main

import (
	"context"
	"flag"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
)

var (
	addr   = flag.String("addr", "localhost:10001", "the address to connect to")
	logger = gnet.GetLogger()
)

func main() {
	flag.Parse()

	gnet.SetLogLevel(gnet.DebugLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientCodec := gnet.NewProtoCodec(nil)
	clientHandler := gnet.NewDefaultConnectionHandler(clientCodec)

	connectionConfig := gnet.DefaultConnectionConfig
	connectionConfig.Codec = clientCodec
	connectionConfig.Handler = clientHandler
	connector := gnet.GetNetMgr().NewConnector(ctx, *addr, &connectionConfig, nil)
	if connector == nil {
		panic("connect failed")
	}

	request := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_HelloRequest), &pb.HelloRequest{
		Name: "hello",
	})
	reply := new(pb.HelloReply)
	err := connector.Rpc(request, reply)
	if err != nil {
		gnet.GetLogger().Error("RpcCallErr:%v", err)
		return
	}
	logger.Info("reply:%v", reply)
	connector.Close()

	gnet.GetNetMgr().Shutdown(true)
}
