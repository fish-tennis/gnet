package main

import (
	"context"
	"flag"
	"github.com/fish-tennis/gnet"
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

	clientCodec := gnet.NewDefaultCodec()
	clientHandler := gnet.NewDefaultConnectionHandler(clientCodec)
	clientHandler.SetOnConnectedFunc(func(connection gnet.Connection, success bool) {
		if success {
			connection.SendPacket(gnet.NewDataPacket([]byte("hello")))
		}
	})
	// 注册心跳包
	clientHandler.RegisterHeartBeat(func() gnet.Packet {
		return gnet.NewDataPacket([]byte("heartbeat"))
	})
	clientHandler.SetUnRegisterHandler(func(connection gnet.Connection, packet gnet.Packet) {
		logger.Info("receive:%v", string(packet.GetStreamData()))
	})

	connectionConfig := gnet.DefaultConnectionConfig
	connectionConfig.Codec = clientCodec
	connectionConfig.Handler = clientHandler
	if gnet.GetNetMgr().NewConnector(ctx, *addr, &connectionConfig, nil) == nil {
		panic("connect failed")
	}

	gnet.GetNetMgr().Shutdown(true)
}
