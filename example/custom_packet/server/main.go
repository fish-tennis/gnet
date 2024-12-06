package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/custom_packet/codec"
	"hash/crc32"
	"net"
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

	serverCodec := &codec.CustomCodec{}
	serverHandler := gnet.NewDefaultConnectionHandler(serverCodec)
	// 注册服务器的消息回调
	serverHandler.SetUnRegisterHandler(func(connection gnet.Connection, packet gnet.Packet) {
		customDataPacket := packet.(*codec.CustomDataPacket)
		if len(customDataPacket.GetStreamData()) < 100 {
			logger.Info("cmd:%v str:%v", customDataPacket.Command(), string(customDataPacket.GetStreamData()))
		} else {
			sum := crc32.ChecksumIEEE(customDataPacket.GetStreamData())
			logger.Info("cmd:%v crc:%x len:%v", customDataPacket.Command(), sum, len(customDataPacket.GetStreamData()))
		}
		// echo
		connection.SendPacket(customDataPacket)

		// 模拟一个带错误码的消息
		connection.SendPacket(codec.NewCustomDataPacket(uint16(packet.Command()), nil).SetErrorCode(12345))
	})

	listenerConfig := &gnet.ListenerConfig{
		AcceptConfig: gnet.DefaultConnectionConfig,
		AcceptConnectionCreator: func(conn net.Conn, config *gnet.ConnectionConfig) gnet.Connection {
			// use TcpConnectionSimple
			return gnet.NewTcpConnectionSimpleAccept(conn, config)
		},
	}
	listenerConfig.AcceptConfig.Codec = serverCodec
	listenerConfig.AcceptConfig.Handler = serverHandler
	if netMgr.NewListener(ctx, fmt.Sprintf(":%v", *port), listenerConfig) == nil {
		panic("listen failed")
	}

	netMgr.Shutdown(true)
}
