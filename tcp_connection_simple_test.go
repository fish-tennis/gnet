package gnet

import (
	"context"
	"github.com/fish-tennis/gnet/example/pb"
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
	codec.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat), new(pb.HeartBeatRes))
	codec.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage), new(pb.TestMessage))
	codec.Register(PacketCommand(10086), nil)

	connectionHandler := NewDefaultConnectionHandler(codec)
	connectionHandler.RegisterHeartBeat(func() Packet {
		return NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatReq{
			Timestamp: GetCurrentTimeStamp(),
		})
	})
	connectionHandler.SetUnRegisterHandler(func(connection Connection, packet Packet) {
		streamStr := ""
		if packet.GetStreamData() != nil {
			streamStr = string(packet.GetStreamData())
		}
		t.Logf("%v %v %v %v", connection.GetConnectionId(), packet.Command(), packet.Message(), streamStr)
	})

	listenerConfig := &ListenerConfig{
		AcceptConfig:    acceptConnectionConfig,
		ListenerHandler: &echoListenerHandler{},
		AcceptConnectionCreator: func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionSimpleAccept(conn, config)
		},
	}
	listenerConfig.AcceptConfig.Codec = codec
	listenerConfig.AcceptConfig.Handler = connectionHandler
	listener := netMgr.NewListener(ctx, listenAddress, listenerConfig)
	time.Sleep(time.Second)

	connectorConnectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		MaxPacketSize:      60,
		RecvTimeout:        3,
		HeartBeatInterval:  2,
		WriteTimeout:       1,
		Codec:              codec,
		Handler:            connectionHandler,
	}
	// test connect failed
	netMgr.NewConnectorCustom(ctx, "127.0.0.1:10086", &connectorConnectionConfig,
		nil, func(config *ConnectionConfig) Connection {
			return NewTcpConnectionSimple(config)
		})
	for i := 0; i < 10; i++ {
		netMgr.NewConnectorCustom(ctx, listenAddress, &connectorConnectionConfig,
			nil, func(config *ConnectionConfig) Connection {
				return NewTcpConnectionSimple(config)
			})
	}

	time.Sleep(time.Second)
	listener.(*TcpListener).RangeConnections(func(conn Connection) bool {
		tcpConnectionSimple := conn.(*TcpConnectionSimple)
		logger.Debug("%v %v %v %v", tcpConnectionSimple.GetConnectionId(), tcpConnectionSimple.LocalAddr(), tcpConnectionSimple.RemoteAddr(), tcpConnectionSimple.GetSendPacketChanLen())
		// test registered stream packet
		conn.TrySendPacket(NewProtoPacketWithData(10086, []byte("try test")), time.Millisecond)
		conn.TrySendPacket(NewProtoPacketWithData(10086, []byte("try test 0")), 0)
		// test registered proto packet
		conn.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "testMessage"})
		return true
	})
	listener.Broadcast(NewProtoPacketWithData(10086, []byte("Broadcast test")))
	// test a wrong packet
	listener.(*TcpListener).RangeConnections(func(conn Connection) bool {
		conn.SendPacket(NewDataPacket([]byte("wrong packet test")))
		return false
	})

	time.Sleep(7 * time.Second)
	listener.GetConnection(1)
	time.Sleep(1 * time.Second)
	listener.Close()

	netMgr.Shutdown(true)
}
