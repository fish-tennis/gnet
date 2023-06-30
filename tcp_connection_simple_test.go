package gnet

import (
	"context"
	"github.com/fish-tennis/gnet/example/pb"
	"google.golang.org/protobuf/proto"
	"net"
	"testing"
	"time"
)

func TestTcpConnectionSimple(t *testing.T) {
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
		MaxPacketSize:      60,
		RecvTimeout:        5,
		WriteTimeout:       1,
	}
	listenAddress := "127.0.0.1:10002"
	codec := NewSimpleProtoCodec()
	codec.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage), new(pb.TestMessage))
	codec.Register(PacketCommand(10086), nil)

	connectionHandler := NewDefaultConnectionHandler(codec)
	connectionHandler.RegisterHeartBeat(PacketCommand(pb.CmdTest_Cmd_HeartBeat), func() proto.Message {
		return &pb.HeartBeatReq{
			Timestamp: GetCurrentTimeStamp(),
		}
	})

	listener := netMgr.NewListenerCustom(ctx, listenAddress, acceptConnectionConfig, codec, connectionHandler, &echoListenerHandler{},
		func(conn net.Conn, config *ConnectionConfig, codec Codec, handler ConnectionHandler) Connection {
			return NewTcpConnectionSimpleAccept(conn, config, codec, handler)
		})
	time.Sleep(time.Second)

	connectorConnectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		MaxPacketSize:      60,
		RecvTimeout:        5,
		HeartBeatInterval:  2,
		WriteTimeout:       1,
	}
	for i := 0; i < 2; i++ {
		netMgr.NewConnectorCustom(ctx, listenAddress, &connectorConnectionConfig, codec,
			connectionHandler, nil, func(config *ConnectionConfig, codec Codec, handler ConnectionHandler) Connection {
				return NewTcpConnectionSimple(config, codec, handler)
			})
	}

	time.Sleep(time.Second)
	listener.(*TcpListener).RangeConnections(func(conn Connection) bool {
		tcpConnectionSimple := conn.(*TcpConnectionSimple)
		logger.Debug("%v %v %v %v", tcpConnectionSimple.GetConnectionId(), tcpConnectionSimple.LocalAddr(), tcpConnectionSimple.RemoteAddr(), tcpConnectionSimple.GetSendPacketChanLen())
		conn.TrySendPacket(NewProtoPacketWithData(10086, []byte("try test")), time.Millisecond)
		conn.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{})
		return true
	})
	listener.Broadcast(NewDataPacket([]byte("test")))

	time.Sleep(7 * time.Second)
	listener.GetConnection(1)
	listener.Close()

	netMgr.Shutdown(true)
}
