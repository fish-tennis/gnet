package main

import (
	"fmt"
	"github.com/gnet"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	// 统计收包数据
	// 因为数据包内容是固定的,所以单位时间内的收包数量就能体现网络性能
	serverRecvPacketCount int64 = 0
	clientRecvPacketCount int64 = 0
)

// 模拟的应用场景:
// 开启一个服务器和N个客户端
// 服务器端:
//   1.当一个新客户端连接上来时,发送30个数据包给该客户端,模拟的游戏角色登录时,游戏服务器下发大量数据包
//   2.服务器收到客户端的数据包,则下发4个数据包作为回复,模拟服务器处理客户端的消息时,往往要回复多个数据包
// 客户端:
//   当收到服务器回复的数据包时,向服务器发送一条数据包,模拟一次客户端交互请求
//
// 性能指标:指定的时间内,服务器和客户端的收发包数量

func TestTestServer(t *testing.T) {
	defer func() {
		if err := recover(); err != nil {
			gnet.LogDebug("fatal %v", err.(error))
			gnet.LogStack()
		}
	}()

	var (
		// 模拟客户端数量
		clientCount = 100
		// 测试程序运行多长时间
		testTime = time.Minute
		// 监听地址
		listenAddress = "127.0.0.1:10002"
	)

	// 关闭日志
	gnet.SetLogWriter(&gnet.NoneLogWriter{})
	netMgr := gnet.GetNetMgr()
	connectionConfig := gnet.ConnectionConfig{
		SendPacketCacheCap:    32,
		// 因为测试的数据包比较小,所以这里也设置的不大
		SendBufferSize: 1024,
		RecvBufferSize: 1024,
		MaxPacketSize:  1024,
		RecvTimeout:    0,
		WriteTimeout:   0,
	}

	codec := gnet.NewDefaultCodec()
	netMgr.NewListener(listenAddress, connectionConfig, codec, &TestServerClientHandler{}, &TestServerListenerHandler{})
	time.Sleep(time.Second)

	for i := 0; i < clientCount; i++ {
		netMgr.NewConnector(listenAddress, connectionConfig, codec, &TestClientHandler{})
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	exitTimer := time.NewTimer(testTime)
	select {
	case <-exitTimer.C:
		gnet.LogDebug("test timeout")
		wg.Done()
	}
	wg.Wait()
	netMgr.Shutdown(true)

	println("*********************************************************")
	// antnet:             	serverRecv:113669 clientRecv:457562
	// gnet 发包 RingBuffer:	serverRecv:199361 clientRecv:800342
	// gnet 收发 RingBuffer:	serverRecv:478501 clientRecv:1916884
	println(fmt.Sprintf("serverRecv:%v clientRecv:%v", serverRecvPacketCount, clientRecvPacketCount))
	println("*********************************************************")
}

// 服务器监听接口
type TestServerListenerHandler struct {
}

func (e *TestServerListenerHandler) OnConnectionConnected(connection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionConnected %v", connection.GetConnectionId()))
}

func (e *TestServerListenerHandler) OnConnectionDisconnect(connection gnet.Connection) {
	gnet.LogDebug(fmt.Sprintf("OnConnectionDisconnect %v", connection.GetConnectionId()))
}

// 服务器端的客户端接口
type TestServerClientHandler struct {

}

func (t *TestServerClientHandler) OnConnected(connection gnet.Connection, success bool) {
	// 模拟客户端登录游戏时,会密集收到一堆消息
	for i := 0; i < 30; i++ {
		toPacket := gnet.NewPacket([]byte("hello client"))
		connection.Send(toPacket)
	}
	toPacket := gnet.NewPacket([]byte("response"))
	connection.Send(toPacket)
}

func (t *TestServerClientHandler) OnDisconnected(connection gnet.Connection) {
}

func (t *TestServerClientHandler) OnRecvPacket(connection gnet.Connection, packet *gnet.Packet) {
	atomic.AddInt64(&serverRecvPacketCount,1)
	// 收到客户端的消息,服务器给客户端回4个消息
	// 因为游戏的特点是:服务器下传数据比客户端上传数据要多
	for i := 0; i < 3; i++ {
		toPacket := gnet.NewPacket([]byte("hello client this is server"))
		connection.Send(toPacket)
	}
	toPacket := gnet.NewPacket([]byte("response"))
	connection.Send(toPacket)
}

// 客户端的网络接口
type TestClientHandler struct {

}

func (t *TestClientHandler) OnConnected(connection gnet.Connection, success bool) {
}

func (t *TestClientHandler) OnDisconnected(connection gnet.Connection) {
}

func (t *TestClientHandler) OnRecvPacket(connection gnet.Connection, packet *gnet.Packet) {
	atomic.AddInt64(&clientRecvPacketCount,1)
	if string(packet.GetData()) == "response" {
		toPacket := gnet.NewPacket([]byte("hello server"))
		connection.Send(toPacket)
	}
}
