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
	clientHandler.SetOnConnectedFunc(func(connection gnet.Connection, success bool) {
		if success {
			request := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_HelloRequest), &pb.HelloRequest{
				Name: "hello",
			})
			reply := new(pb.HelloReply)
			err := connection.Rpc(request, reply)
			if err != nil {
				gnet.GetLogger().Error("RpcCallErr:%v", err)
				return
			}
			logger.Info("reply:%v", reply)
			connection.Close()
		}
	})

	connectionConfig := gnet.DefaultConnectionConfig
	connectionConfig.Codec = clientCodec
	connectionConfig.Handler = clientHandler
	if gnet.GetNetMgr().NewConnector(ctx, *addr, &connectionConfig, nil) == nil {
		panic("connect failed")
	}

	gnet.GetNetMgr().Shutdown(true)
}
