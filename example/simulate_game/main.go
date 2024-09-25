package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
	"net"
	"sync/atomic"
	"time"
)

var (
	port          = flag.Int("port", 10002, "The server port")
	testTime      = flag.Int("time", 10, "the seconds to run")
	clientCount   = flag.Int("client", 100, "client count")
	useRingBuffer = flag.Bool("ringbuffer", true, "whether to use RingBuffer")
)

var (
	// 统计收包数据
	// 因为数据包内容是固定的,所以单位时间内的收包数量就能体现网络性能
	_serverRecvPacketCount int64 = 0
	_clientRecvPacketCount int64 = 0
)

// simulated a game application scenario
//  start a server and N clients
//  server side:
//   1.when a new client connects, 30 data packets are sent to the client.
//   2.when the server receives data packets from the client, it sends four data packets as replies.
//  client side:
//   when receiving a reply packet from the server, send a packet to the server to simulate a client interaction request
//  Performance indicator: The number of packets sent and received by the server and client within the specified time frame

// 模拟的应用场景:
// 开启一个服务器和N个客户端
// 服务器端:
//
//	1.当一个新客户端连接上来时,发送30个数据包给该客户端,模拟的游戏角色登录时,游戏服务器下发大量数据包
//	2.服务器收到客户端的数据包,则下发4个数据包作为回复,模拟服务器处理客户端的消息时,往往要回复多个数据包
//
// 客户端:
//
//	当收到服务器回复的数据包时,向服务器发送一条数据包,模拟一次客户端交互请求
//
// 性能指标:指定的时间内,服务器和客户端的收发包数量

// 在本人的电脑上的一组测试结果
// testTime:10秒 clientCount:100
// useRingBuffer==true时,  serverRecv:67363 clientRecv:272213
// useRingBuffer==false时, serverRecv:11087 clientRecv:47132

func main() {
	flag.Parse()

	gnet.SetLogLevel(gnet.ErrorLevel)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*testTime)*time.Second)
	defer cancel()

	netMgr := gnet.GetNetMgr()

	var serverCodec gnet.Codec
	if *useRingBuffer {
		serverCodec = gnet.NewProtoCodec(nil)
	} else {
		serverCodec = gnet.NewSimpleProtoCodec()
	}
	serverHandler := gnet.NewDefaultConnectionHandler(serverCodec)
	// 注册服务器的消息回调
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessage, new(pb.TestMessage))
	serverHandler.SetOnConnectedFunc(func(connection gnet.Connection, success bool) {
		if success {
			onClientConnected(connection)
		}
	})

	connectionConfig := gnet.ConnectionConfig{
		SendPacketCacheCap: 16,
		// 因为测试的数据包比较小,所以这里也设置的不大
		SendBufferSize: 1024,
		RecvBufferSize: 1024,
		MaxPacketSize:  1024,
	}
	listenerConfig := &gnet.ListenerConfig{
		AcceptConfig: connectionConfig,
	}
	listenerConfig.AcceptConnectionCreator = func(conn net.Conn, config *gnet.ConnectionConfig) gnet.Connection {
		if *useRingBuffer {
			return gnet.NewTcpConnectionAccept(conn, config)
		} else {
			return gnet.NewTcpConnectionSimpleAccept(conn, config)
		}
	}
	listenerConfig.AcceptConfig.Codec = serverCodec
	listenerConfig.AcceptConfig.Handler = serverHandler
	listenAddress := fmt.Sprintf(":%v", *port)
	if netMgr.NewListener(ctx, listenAddress, listenerConfig) == nil {
		panic("listen failed")
	}

	var clientCodec gnet.Codec
	if *useRingBuffer {
		clientCodec = gnet.NewProtoCodec(nil)
	} else {
		clientCodec = gnet.NewSimpleProtoCodec()
	}
	clientHandler := gnet.NewDefaultConnectionHandler(clientCodec)
	clientHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessageClient, new(pb.TestMessage))
	connectorConfig := connectionConfig
	connectorConfig.Codec = clientCodec
	connectorConfig.Handler = clientHandler
	for i := 0; i < *clientCount; i++ {
		netMgr.NewConnectorCustom(ctx, listenAddress, &connectorConfig, nil, func(config *gnet.ConnectionConfig) gnet.Connection {
			if *useRingBuffer {
				return gnet.NewTcpConnector(config)
			} else {
				return gnet.NewTcpConnectionSimple(config)
			}
		})
	}

	netMgr.Shutdown(true)

	println("*********************************************************")
	println(fmt.Sprintf("serverRecv:%v clientRecv:%v", _serverRecvPacketCount, _clientRecvPacketCount))
	println(fmt.Sprintf("New:%v Alloc:%v Free:%v", gnet.NewAllocCount.Load(), gnet.TotalAllocCount.Load(), gnet.TotalFreeCount.Load()))
	println("*********************************************************")
}

func onClientConnected(connection gnet.Connection) {
	// 模拟客户端登录游戏时,会密集收到一堆消息
	for i := 0; i < 30; i++ {
		toPacket := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
			&pb.TestMessage{
				Name: "hello client, this is server",
				I32:  int32(i),
			})
		connection.SendPacket(toPacket)
	}
}

// 服务器收到客户端的TestMessage
func onTestMessage(connection gnet.Connection, packet gnet.Packet) {
	atomic.AddInt64(&_serverRecvPacketCount, 1)
	// 收到客户端的消息,服务器给客户端回4个消息
	// 因为游戏的特点是:服务器下传数据比客户端上传数据要多
	for i := 0; i < 4; i++ {
		toPacket := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
			&pb.TestMessage{
				Name: "hello client, this is server",
				I32:  int32(i),
			})
		connection.SendPacket(toPacket)
	}
}

func onTestMessageClient(connection gnet.Connection, packet gnet.Packet) {
	atomic.AddInt64(&_clientRecvPacketCount, 1)
	protoPacket := packet.(*gnet.ProtoPacket)
	recvMessage := protoPacket.Message().(*pb.TestMessage)
	// 回复某一些消息
	if recvMessage.I32 == 0 {
		toPacket := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
			&pb.TestMessage{
				Name: "hello server, this is client",
				I32:  int32(0),
			})
		connection.SendPacket(toPacket)
	}
}
