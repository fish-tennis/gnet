package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fish-tennis/gnet"
	"strings"
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

	serverCodec := gnet.NewDefaultCodec()
	serverHandler := gnet.NewDefaultConnectionHandler(serverCodec)
	// 注册服务器的消息回调
	serverHandler.SetUnRegisterHandler(func(connection gnet.Connection, packet gnet.Packet) {
		str := string(packet.GetStreamData())
		logger.Info("receive:%v", str)
		if strings.HasPrefix(str, "hello") {
			connection.SendPacket(gnet.NewDataPacket([]byte(string(packet.GetStreamData()) + " world")))
		} else {
			// echo
			connection.SendPacket(gnet.NewDataPacket([]byte(str + " from server")))
		}
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
