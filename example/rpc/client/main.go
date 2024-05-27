package main

import (
	"context"
	"flag"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
	"time"
)

var (
	addr = flag.String("addr", "localhost:10001", "the address to connect to")
)

func main() {
	flag.Parse()

	gnet.SetLogLevel(gnet.DebugLevel)
	// 10秒后触发关闭通知,所有监听<-ctx.Done()的地方会收到通知
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	clientCodec := gnet.NewProtoCodec(nil)
	clientHandler := gnet.NewDefaultConnectionHandler(clientCodec)
	//clientCodec.Register(gnet.PacketCommand(pb.CmdTest_Cmd_HelloReply), new(pb.HelloReply))
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
			gnet.GetLogger().Info("reply:%v", reply)
			connection.Close()
		}
	})

	connectionConfig := gnet.ConnectionConfig{
		SendPacketCacheCap: 100,
		SendBufferSize:     60, // 设置的比较小,便于测试缓存写满的情况
		RecvBufferSize:     60,
		MaxPacketSize:      60,
		RecvTimeout:        0,
		HeartBeatInterval:  3,
		WriteTimeout:       0,
	}
	connectionConfig.Codec = clientCodec
	connectionConfig.Handler = clientHandler
	if gnet.GetNetMgr().NewConnector(ctx, *addr, &connectionConfig, nil) == nil {
		panic("connect failed")
	}

	gnet.GetNetMgr().Shutdown(true)
}
