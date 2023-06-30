package gnet

import (
	"context"
	"github.com/fish-tennis/gnet/example/pb"
	_ "net/http/pprof"
	"testing"
	"time"
)

func TestTimeout(t *testing.T) {
	defer func() {
		if err := recover(); err != nil {
			logger.Debug("fatal %v", err.(error))
			LogStack()
		}
	}()
	SetLogLevel(DebugLevel)

	// 1小时后触发关闭通知,所有监听<-ctx.Done()的地方会收到通知
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*1)
	defer cancel()

	netMgr := GetNetMgr()
	acceptConnectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		MaxPacketSize:      1024,
		SendBufferSize:     60,
		RecvBufferSize:     60,
		RecvTimeout:        5,
		HeartBeatInterval:  0,
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"
	codec := NewDefaultCodec()
	listener := netMgr.NewListener(ctx, listenAddress, acceptConnectionConfig, codec, &DefaultConnectionHandler{}, &echoListenerHandler{})
	time.Sleep(time.Second)
	logger.Debug("%v", listener.Addr())

	connectorConnectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		RecvTimeout:        3,
		HeartBeatInterval:  0,
		WriteTimeout:       1,
	}
	// test connect failed
	netMgr.NewConnector(ctx, "127.0.0.1:10086", &connectorConnectionConfig, codec,
		&DefaultConnectionHandler{}, nil)
	for i := 0; i < 10; i++ {
		netMgr.NewConnector(ctx, listenAddress, &connectorConnectionConfig, codec,
			&DefaultConnectionHandler{}, nil)
	}

	time.Sleep(time.Second)
	listener.(*TcpListener).RangeConnections(func(conn Connection) bool {
		conn.TrySendPacket(NewDataPacket([]byte("try test")), time.Millisecond)
		conn.TrySendPacket(NewDataPacket([]byte("try test 0")), 0)
		return true
	})
	listener.Broadcast(NewDataPacket([]byte("test")))

	time.Sleep(7 * time.Second)
	listener.GetConnection(1)
	// test a wrong packet
	listener.(*TcpListener).RangeConnections(func(conn Connection) bool {
		conn.SendPacket(NewProtoPacket(10086, new(pb.TestMessage)))
		return false
	})
	time.Sleep(1 * time.Second)
	listener.Close()

	netMgr.Shutdown(true)
}
