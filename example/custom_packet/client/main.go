package main

import (
	"context"
	"flag"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/custom_packet/codec"
	"hash/crc32"
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

	clientCodec := &codec.CustomCodec{}
	clientHandler := gnet.NewDefaultConnectionHandler(clientCodec)
	// 客户端作为connector,需要设置心跳包
	clientHandler.RegisterHeartBeat(func() gnet.Packet {
		return codec.NewCustomDataPacket(1, []byte("heartbeat"))
	})
	clientHandler.SetOnConnectedFunc(func(connection gnet.Connection, success bool) {
		if success {
			connection.SendPacket(codec.NewCustomDataPacket(2, []byte("hello")))

			// 模拟一个非常大的数据包
			bigPacketSize := 1024 * 1024 * 30 // 30M
			bigPacket := make([]byte, bigPacketSize)
			for i := 0; i < len(bigPacket); i++ {
				bigPacket[i] = byte(i)
			}
			sum := crc32.ChecksumIEEE(bigPacket)
			connection.SendPacket(codec.NewCustomDataPacket(3, bigPacket))
			logger.Info("%x", sum)
		}
	})
	clientHandler.SetUnRegisterHandler(func(connection gnet.Connection, packet gnet.Packet) {
		customDataPacket := packet.(*codec.CustomDataPacket)
		if len(customDataPacket.GetStreamData()) < 100 {
			logger.Info("cmd:%v str:%v", customDataPacket.Command(), string(customDataPacket.GetStreamData()))
		} else {
			sum := crc32.ChecksumIEEE(customDataPacket.GetStreamData())
			logger.Info("cmd:%v crc:%x len:%v", customDataPacket.Command(), sum, len(customDataPacket.GetStreamData()))
		}
	})

	connectionConfig := gnet.DefaultConnectionConfig
	// CustomCodec支持超出gnet.DefaultPacketHeaderSize大小的包
	connectionConfig.MaxPacketSize = 1024 * 1024 * 32
	connectionConfig.Codec = clientCodec
	connectionConfig.Handler = clientHandler
	if gnet.GetNetMgr().NewConnectorCustom(ctx, *addr, &connectionConfig, nil, func(config *gnet.ConnectionConfig) gnet.Connection {
		// use TcpConnectionSimple
		return gnet.NewTcpConnectionSimple(config)
	}) == nil {
		panic("connect failed")
	}

	gnet.GetNetMgr().Shutdown(true)
}
