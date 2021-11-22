package example

import (
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
	"google.golang.org/protobuf/proto"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gnet写的客户端连接antnet写的server
func TestAntnetConnector(t *testing.T) {
	defer func() {
		if err := recover(); err != nil {
			gnet.LogDebug("fatal %v", err.(error))
			gnet.LogStack()
		}
	}()

	var (
		// 模拟客户端数量
		clientCount = 1
		// 测试程序运行多长时间
		testTime = time.Minute
		// 监听地址
		listenAddress = "127.0.0.1:10002"
	)

	// 关闭日志
	//gnet.SetLogWriter(&gnet.NoneLogWriter{})
	//gnet.SetLogLevel(gnet.ErrorLevel)
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

	protoMap := make(map[gnet.PacketCommand]gnet.ProtoMessageCreator)
	protoMap[gnet.PacketCommand(1<<8 + 2)] = func() proto.Message {
		return &pb.GoTestField{}
	}
	codec := gnet.NewAntnetCodec(true, protoMap)

	for i := 0; i < clientCount; i++ {
		if netMgr.NewConnector(listenAddress, connectionConfig, codec, &antnetConnectorHandler{}) == nil {
			panic("connect error")
		}
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
	println(fmt.Sprintf("clientRecv:%v", clientRecvPacketCount))
	println("*********************************************************")
}

type antnetConnectorHandler struct {

}

func (this *antnetConnectorHandler) CreateHeartBeatPacket(connection gnet.Connection) gnet.Packet {
	return nil
}

func (this *antnetConnectorHandler) OnConnected(connection gnet.Connection, success bool) {
}

func (this *antnetConnectorHandler) OnDisconnected(connection gnet.Connection) {
}

func (this *antnetConnectorHandler) OnRecvPacket(connection gnet.Connection, packet gnet.Packet) {
	atomic.AddInt64(&clientRecvPacketCount,1)
	antnetPacket := packet.(*gnet.AntnetPacket)
	protoMessage := antnetPacket.Message().(*pb.GoTestField)
	gnet.LogDebug("client recv:%v", protoMessage)
	if protoMessage.GetLabel() == "response" {
		toPacket := gnet.NewAntnetPacket(1, 2, 0,
			&pb.GoTestField{
				Label: proto.String("hello server"),
				Type: proto.String(""),
			})
		connection.Send(toPacket)
	}
}

