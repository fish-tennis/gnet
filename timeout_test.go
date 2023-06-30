package gnet

import (
	"context"
	"github.com/fish-tennis/gnet/example/pb"
	"net"
	"net/http"
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

	go http.ListenAndServe(":10009", nil)

	netMgr := GetNetMgr()
	connectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		SendBufferSize:     60, // 设置的比较小,便于测试缓存写满的情况
		RecvBufferSize:     60,
		MaxPacketSize:      60,
		RecvTimeout:        3,
		HeartBeatInterval:  0,
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"
	codec := NewDefaultCodec()
	listener := netMgr.NewListenerCustom(ctx, listenAddress, connectionConfig, codec, &DefaultConnectionHandler{}, &echoListenerHandler{},
		func(conn net.Conn, config *ConnectionConfig, codec Codec, handler ConnectionHandler) Connection {
			return NewTcpConnectionSimple(config, codec, handler)
		})
	time.Sleep(time.Second)
	logger.Debug("%v", listener.Addr())

	connectorConnectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		SendBufferSize:     60,
		RecvBufferSize:     60,
		MaxPacketSize:      60,
		RecvTimeout:        3,
		HeartBeatInterval:  0,
		WriteTimeout:       1,
	}
	for i := 0; i < 10; i++ {
		netMgr.NewConnectorCustom(ctx, listenAddress, &connectorConnectionConfig, codec,
			&DefaultConnectionHandler{}, nil, func(config *ConnectionConfig, codec Codec, handler ConnectionHandler) Connection {
				return NewTcpConnectionSimple(config, codec, handler)
			})
	}

	time.Sleep(time.Second)
	listener.(*TcpListener).RangeConnections(func(conn Connection) bool {
		tcpConnectionSimple := conn.(*TcpConnectionSimple)
		logger.Debug("%v %v %v %v", tcpConnectionSimple.GetConnectionId(), tcpConnectionSimple.LocalAddr(), tcpConnectionSimple.RemoteAddr(), tcpConnectionSimple.GetSendPacketChanLen())
		conn.TrySendPacket(NewDataPacket([]byte("try test")), time.Millisecond)
		conn.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{})
		return true
	})
	listener.Broadcast(NewDataPacket([]byte("test")))

	time.Sleep(5 * time.Second)
	listener.GetConnection(1)
	listener.Close()

	netMgr.Shutdown(true)
}
