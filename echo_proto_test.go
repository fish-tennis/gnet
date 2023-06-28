package gnet

import (
	"context"
	"fmt"
	"github.com/fish-tennis/gnet/example/pb"
	"google.golang.org/protobuf/proto"
	"testing"
	"time"
)

// 测试protobuf
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
	//serverCodec := NewXorProtoCodec([]byte("xor_test_key"), nil)
	serverHandler := &echoProtoServerHandler{
		DefaultConnectionHandler: *NewDefaultConnectionHandler(serverCodec),
	}
	// 注册服务器的消息回调
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat), onHeartBeatReq, new(pb.HeartBeatReq))
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessageServer, new(pb.TestMessage))
	if netMgr.NewListener(ctx, listenAddress, connectionConfig, serverCodec, serverHandler, nil) == nil {
		panic("listen failed")
	}
	time.Sleep(time.Second)

	clientCodec := NewProtoCodec(nil)
	//clientCodec := NewXorProtoCodec([]byte("xor_test_key"), nil)
	clientHandler := &echoProtoClientHandler{
		DefaultConnectionHandler: *NewDefaultConnectionHandler(clientCodec),
	}
	// 客户端作为connector,需要设置心跳包
	clientHandler.RegisterHeartBeat(PacketCommand(pb.CmdTest_Cmd_HeartBeat), func() proto.Message {
		return &pb.HeartBeatReq{}
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

// 服务端监听到的连接接口
type echoProtoServerHandler struct {
	DefaultConnectionHandler
}

func (e *echoProtoServerHandler) OnConnected(connection Connection, success bool) {
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
			connection.SendPacket(packet)
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
func onHeartBeatReq(connection Connection, packet *ProtoPacket) {
	req := packet.Message().(*pb.HeartBeatReq)
	logger.Debug(fmt.Sprintf("Server onHeartBeatReq: %v", req))
	connection.Send(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatRes{
		RequestTimestamp:  req.GetTimestamp(),
		ResponseTimestamp: time.Now().UnixNano() / int64(time.Microsecond),
	})
}

// 服务器收到客户端的TestMessage
func onTestMessageServer(connection Connection, packet *ProtoPacket) {
	req := packet.Message().(*pb.TestMessage)
	logger.Debug(fmt.Sprintf("Server onTestMessage: %v", req))
}

// 客户端连接接口
type echoProtoClientHandler struct {
	DefaultConnectionHandler
	echoCount int
}

// 收到心跳包回复
func (e *echoProtoClientHandler) onHeartBeatRes(connection Connection, packet *ProtoPacket) {
	res := packet.Message().(*pb.HeartBeatRes)
	logger.Debug(fmt.Sprintf("client onHeartBeatRes: %v", res))
}

func (e *echoProtoClientHandler) onTestMessage(connection Connection, packet *ProtoPacket) {
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
func (e *echoProtoClientHandler) onTestDataMessage(connection Connection, packet *ProtoPacket) {
	logger.Debug(fmt.Sprintf("client onTestDataMessage: %v", string(packet.GetStreamData())))
	e.echoCount++
	connection.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		&pb.TestMessage{
			Name: fmt.Sprintf("hello server %v", e.echoCount),
			I32:  int32(e.echoCount),
		})
}
