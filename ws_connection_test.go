package gnet

import (
	"context"
	"github.com/fish-tennis/gnet/example/pb"
	"net"
	"testing"
	"time"
)

func TestWsConnection(t *testing.T) {
	testWebSocket(t, "ws", "", "")
}

func TestWssConnection(t *testing.T) {
	testWebSocket(t, "wss", "example/cert.pem", "example/key.pem")
}

func testWebSocket(t *testing.T, protocolName, certFile, keyFile string) {
	defer func() {
		if err := recover(); err != nil {
			logger.Debug("fatal %v", err.(error))
			LogStack()
		}
	}()
	SetLogLevel(DebugLevel)

	// 1分钟后触发关闭通知,所有监听<-ctx.Done()的地方会收到通知
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*1)
	defer cancel()

	netMgr := GetNetMgr()
	acceptConnectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		MaxPacketSize:      60,
		RecvTimeout:        5,
		WriteTimeout:       1,
		HeartBeatInterval:  2,
	}
	listenAddress := "localhost:10002"
	codec := NewSimpleProtoCodec()
	codec.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat), new(pb.HeartBeatRes))
	codec.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage), new(pb.TestMessage))
	codec.Register(PacketCommand(10086), nil)

	connectionHandler := NewDefaultConnectionHandler(codec)
	connectionHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat), onHeartBeatReq, new(pb.HeartBeatReq))
	connectionHandler.SetUnRegisterHandler(func(connection Connection, packet Packet) {
		streamStr := ""
		if packet.GetStreamData() != nil {
			streamStr = string(packet.GetStreamData())
		}
		t.Logf("%v %v %v %v", connection.GetConnectionId(), packet.Command(), packet.Message(), streamStr)
		connection.SendPacket(packet.Clone())
	})
	acceptConnectionConfig.Codec = codec
	acceptConnectionConfig.Handler = connectionHandler

	listenerConfig := &ListenerConfig{
		AcceptConfig:    acceptConnectionConfig,
		ListenerHandler: &echoListenerHandler{},
		AcceptConnectionCreator: func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionSimpleAccept(conn, config)
		},
		Path:     "/" + protocolName,
		CertFile: certFile,
		KeyFile:  keyFile,
	}
	listener := netMgr.NewWsListener(ctx, listenAddress, listenerConfig)
	time.Sleep(time.Second)

	connectorHandler := NewDefaultConnectionHandler(codec)
	connectorHandler.RegisterHeartBeat(func() Packet {
		return NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatReq{
			Timestamp: GetCurrentTimeStamp(),
		})
	})
	connectorHandler.SetUnRegisterHandler(func(connection Connection, packet Packet) {
		streamStr := ""
		if packet.GetStreamData() != nil {
			streamStr = string(packet.GetStreamData())
		}
		t.Logf("%v %v %v %v", connection.GetConnectionId(), packet.Command(), packet.Message(), streamStr)
	})
	connectorConnectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		MaxPacketSize:      60,
		RecvTimeout:        3,
		HeartBeatInterval:  2,
		WriteTimeout:       1,
		Codec:              codec,
		Handler:            connectorHandler,
		Path:               "/" + protocolName,
		Scheme:             protocolName,
	}
	//// test connect failed
	//netMgr.NewWsConnector(ctx, "127.0.0.1:10086", &connectorConnectionConfig, codec,
	//	connectionHandler, nil)
	for i := 0; i < 1; i++ {
		netMgr.NewWsConnector(ctx, listenAddress, &connectorConnectionConfig, nil)
	}

	time.Sleep(time.Second)
	listener.(*WsListener).RangeConnections(func(conn Connection) bool {
		wsConnection := conn.(*WsConnection)
		logger.Debug("%v %v %v %v", wsConnection.GetConnectionId(), wsConnection.LocalAddr(), wsConnection.RemoteAddr(), wsConnection.GetSendPacketChanLen())
		// test registered stream packet
		conn.TrySendPacket(NewProtoPacketWithData(10086, []byte("try test")), time.Millisecond)
		conn.TrySendPacket(NewProtoPacketWithData(10086, []byte("try test 0")), 0)
		// test registered proto packet
		conn.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "testMessage"})
		return true
	})
	listener.Broadcast(NewProtoPacketWithData(10086, []byte("Broadcast test")))
	//// test a wrong packet
	//listener.(*WsListener).RangeConnections(func(conn Connection) bool {
	//	conn.SendPacket(NewDataPacket([]byte("wrong packet test")))
	//	return false
	//})

	time.Sleep(7 * time.Second)
	listener.GetConnection(1)
	time.Sleep(1 * time.Second)
	listener.Close()

	netMgr.Shutdown(true)
}
