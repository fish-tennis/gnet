package gnet

import (
	"context"
	"fmt"
	"github.com/fish-tennis/gnet/example/pb"
	"testing"
	"time"
)

// show how to use protobuf
//
//	测试protobuf
func TestEchoProto(t *testing.T) {
	defer func() {
		if err := recover(); err != nil {
			logger.Debug("fatal %v", err.(error))
			LogStack()
		}
	}()

	SetLogLevel(DebugLevel)
	// 10秒后触发关闭通知,所有监听<-ctx.Done()的地方会收到通知
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	netMgr := GetNetMgr()
	connectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		SendBufferSize:     60, // 设置的比较小,便于测试缓存写满的情况
		RecvBufferSize:     60,
		MaxPacketSize:      60,
		RecvTimeout:        0,
		HeartBeatInterval:  3,
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.SetOnConnectedFunc(echoProtoOnConnected)
	serverHandler.SetOnDisconnectedFunc(func(connection Connection) {
		logger.Debug("%v %v %v", connection.LocalAddr(), connection.RemoteAddr(), connection.(*TcpConnection).GetSendPacketChanLen())
	})
	// 注册服务器的消息回调
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat), onHeartBeatReq, new(pb.HeartBeatReq))
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessageServer, new(pb.TestMessage))
	serverHandler.SetUnRegisterHandler(func(connection Connection, packet Packet) {
		logger.Warn("%v", packet)
	})
	serverHandler.GetPacketHandler(PacketCommand(pb.CmdTest_Cmd_TestMessage))
	if netMgr.NewListener(ctx, listenAddress, connectionConfig, serverCodec, serverHandler, nil) == nil {
		panic("listen failed")
	}
	time.Sleep(time.Millisecond)

	clientCodec := NewProtoCodec(nil)
	//clientCodec := NewXorProtoCodec([]byte("xor_test_key"), nil)
	clientHandler := &echoProtoClientHandler{
		DefaultConnectionHandler: *NewDefaultConnectionHandler(clientCodec),
	}
	// 客户端作为connector,需要设置心跳包
	clientHandler.RegisterHeartBeat(func() Packet {
		return NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_HeartBeat),&pb.HeartBeatReq{})
	})
	// 注册客户端的消息回调
	clientHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat), clientHandler.onHeartBeatRes, new(pb.HeartBeatRes))
	clientHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage), clientHandler.onTestMessage, new(pb.TestMessage))
	// 测试没有注册proto.Message的消息
	clientHandler.Register(PacketCommand(100), clientHandler.onTestDataMessage, nil)
	if netMgr.NewConnector(ctx, listenAddress, &connectionConfig, clientCodec, clientHandler, nil) == nil {
		panic("connect failed")
	}

	netMgr.Shutdown(true)
}

func echoProtoOnConnected(connection Connection, success bool) {
	logger.Debug(fmt.Sprintf("Server OnConnected %v %v", connection.GetConnectionId(), success))
	if success {
		// 开一个协程,服务器自动给客户端发消息
		serialId := 0
		// 先连发10个数据包
		for i := 0; i < 10; i++ {
			serialId++
			packet := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
				&pb.TestMessage{
					Name: fmt.Sprintf("hello client %v", serialId),
					I32:  int32(serialId),
				})
			connection.TrySendPacket(packet, time.Second)
		}
		serialId++
		// 测试没有注册proto.Message的消息
		packet := NewProtoPacketWithData(PacketCommand(100), []byte("123456789testdata"))
		connection.SendPacket(packet)
		// 每隔1秒 发一个包
		go func() {
			autoSendTimer := time.NewTimer(time.Second)
			for connection.IsConnected() {
				select {
				case <-autoSendTimer.C:
					serialId++
					packet := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
						&pb.TestMessage{
							Name: fmt.Sprintf("hello client %v", serialId),
							I32:  int32(serialId),
						})
					connection.SendPacket(packet)
					autoSendTimer.Reset(time.Second)
				}
			}
		}()
	}
}

// 服务器收到客户端的心跳包
func onHeartBeatReq(connection Connection, packet Packet) {
	req := packet.Message().(*pb.HeartBeatReq)
	logger.Debug(fmt.Sprintf("Server onHeartBeatReq: %v", req))
	connection.Send(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatRes{
		RequestTimestamp:  req.GetTimestamp(),
		ResponseTimestamp: time.Now().UnixNano() / int64(time.Microsecond),
	})
}

// 服务器收到客户端的TestMessage
func onTestMessageServer(connection Connection, packet Packet) {
	req := packet.Message().(*pb.TestMessage)
	logger.Debug(fmt.Sprintf("Server onTestMessage: %v", req))
}

// 客户端连接接口
type echoProtoClientHandler struct {
	DefaultConnectionHandler
	echoCount int
}

// 收到心跳包回复
func (e *echoProtoClientHandler) onHeartBeatRes(connection Connection, packet Packet) {
	res := packet.Message().(*pb.HeartBeatRes)
	logger.Debug(fmt.Sprintf("client onHeartBeatRes: %v", res))
}

func (e *echoProtoClientHandler) onTestMessage(connection Connection, packet Packet) {
	res := packet.Message().(*pb.TestMessage)
	logger.Debug(fmt.Sprintf("client onTestMessage: %v", res))
	e.echoCount++
	connection.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		&pb.TestMessage{
			Name: fmt.Sprintf("hello server %v", e.echoCount),
			I32:  int32(e.echoCount),
		})
}

// 测试没有注册proto.Message的消息
func (e *echoProtoClientHandler) onTestDataMessage(connection Connection, packet Packet) {
	logger.Debug(fmt.Sprintf("client onTestDataMessage: %v", string(packet.GetStreamData())))
	e.echoCount++
	connection.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		&pb.TestMessage{
			Name: fmt.Sprintf("hello server %v", e.echoCount),
			I32:  int32(e.echoCount),
		})
}

func TestListenerError(t *testing.T) {
	config := ConnectionConfig{}
	tcpListener := NewTcpListener(config, nil, nil, nil)
	tcpListener.Addr()
	tcpListener1 := GetNetMgr().NewListener(context.Background(), "127.0.0.1:10001", config, nil, nil, nil)
	tcpListener2 := GetNetMgr().NewListener(context.Background(), "127.0.0.1:10001", config, nil, nil, nil)
	if tcpListener1 != nil {
		tcpListener1.Close()
	}
	if tcpListener2 != nil {
		tcpListener2.Close()
	}
	GetNetMgr().Shutdown(true)
}
