package gnet

import (
	"context"
	"net"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fish-tennis/gnet/example/pb"
	"google.golang.org/protobuf/proto"
)

// ==================== WS Listener coverage ====================

func TestWsListener_GetConnectionAndBroadcast(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	connected := make(chan Connection, 1)
	lh := &testListenerHandler{
		onConnected: func(listener Listener, conn Connection) {
			connected <- conn
		},
	}

	lcfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: lh,
		Path:            "/ws",
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
	listener := GetNetMgr().NewWsListener(ctx, addr, lcfg)
	if listener == nil {
		t.Fatal("ws listener failed")
	}
	defer listener.Close()

	if listener.Addr() == nil {
		t.Fatal("ws Addr should not be nil after Start")
	}
	wsListener := listener.(*WsListener)
	if !wsListener.IsRunning() {
		t.Fatal("ws listener should be running")
	}

	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	received := make(chan struct{}, 1)
	clientHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			received <- struct{}{}
		}, new(pb.TestMessage))
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.Scheme = "ws"
	clientCfg.Path = "/ws"
	client := GetNetMgr().NewWsConnector(ctx, addr, clientCfg, "ws-bcast")
	if client == nil {
		t.Fatal("ws connector failed")
	}
	defer client.Close()

	select {
	case conn := <-connected:
		// GetConnection
		found := listener.GetConnection(conn.GetConnectionId())
		if found == nil {
			t.Fatal("ws GetConnection returned nil")
		}
		notFound := listener.GetConnection(999999)
		if notFound != nil {
			t.Fatal("ws GetConnection should return nil")
		}

		// Broadcast
		listener.Broadcast(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
			&pb.TestMessage{Name: "ws bcast"}))
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ws connection")
	}

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ws broadcast")
	}

	// RangeConnections
	count := 0
	wsListener.RangeConnections(func(conn Connection) bool {
		count++
		return true
	})
	if count == 0 {
		t.Fatal("ws RangeConnections found 0")
	}
}

// ==================== RemoteAddr on connected connections ====================

func TestTcpConnection_RemoteAddr_Connected(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "addr-test")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	if client.LocalAddr() == nil {
		t.Fatal("TCP LocalAddr should not be nil")
	}
	if client.RemoteAddr() == nil {
		t.Fatal("TCP RemoteAddr should not be nil")
	}
}

func TestWsConnection_RemoteAddr_Connected(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)

	lcfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
		Path:            "/ws",
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
	listener := GetNetMgr().NewWsListener(ctx, addr, lcfg)
	defer listener.Close()

	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.Scheme = "ws"
	clientCfg.Path = "/ws"
	client := GetNetMgr().NewWsConnector(ctx, addr, clientCfg, "ws-addr")
	if client == nil {
		t.Fatal("ws connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	if client.LocalAddr() == nil {
		t.Fatal("WS LocalAddr should not be nil")
	}
	if client.RemoteAddr() == nil {
		t.Fatal("WS RemoteAddr should not be nil")
	}
}

func TestTcpConnectionSimple_RemoteAddr_Connected(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)

	listenerCfg := &ListenerConfig{
		AcceptConfig: *defaultTestConfig(serverCodec, serverHandler),
		AcceptConnectionCreator: func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionSimpleAccept(conn, config)
		},
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnectorCustom(ctx, addr, clientCfg, "simple-addr",
		func(cfg *ConnectionConfig) Connection {
			return NewTcpConnectionSimple(cfg)
		})
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	if client.LocalAddr() == nil {
		t.Fatal("Simple LocalAddr should not be nil")
	}
	if client.RemoteAddr() == nil {
		t.Fatal("Simple RemoteAddr should not be nil")
	}
}

// ==================== codec.go: NewDefaultCodec ====================

func TestNewDefaultCodec(t *testing.T) {
	codec := NewDefaultCodec()
	if codec == nil {
		t.Fatal("NewDefaultCodec returned nil")
	}
	if codec.PacketHeaderSize() != DefaultPacketHeaderSize {
		t.Fatal("DefaultCodec PacketHeaderSize mismatch")
	}
	h := codec.CreatePacketHeader(nil, nil, nil)
	if h == nil {
		t.Fatal("CreatePacketHeader returned nil")
	}
}

// ==================== codec.go: Encode/Decode with DefaultCodec (no DataEncoder) ====================

func TestDefaultCodec_EncodeDecode_NoDataEncoder(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewDefaultCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	received := make(chan []byte, 1)
	serverHandler.SetUnRegisterHandler(func(conn Connection, pkt Packet) {
		received <- pkt.GetStreamData()
	})

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewDefaultCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "default-codec")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	client.SendPacket(NewDataPacket([]byte("default codec test")))

	select {
	case data := <-received:
		if string(data) != "default codec test" {
			t.Fatalf("data mismatch: %s", string(data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// ==================== NewProtoCodec with map ====================

func TestNewProtoCodec_WithMap(t *testing.T) {
	cmd := PacketCommand(700)
	// 兼容旧的 map[PacketCommand]reflect.Type 初始化方式
	m := map[PacketCommand]reflect.Type{
		cmd: reflect.TypeOf(pb.TestMessage{}),
	}
	codec := NewProtoCodec(m)
	if codec.MessageCreatorMap[cmd] == nil {
		t.Fatal("creator should be registered from map")
	}
	msg := codec.MessageCreatorMap[cmd]()
	if _, ok := msg.(*pb.TestMessage); !ok {
		t.Fatalf("creator returned wrong type: %T", msg)
	}
}

// ==================== Register with nil protoMessage ====================

func TestProtoCodec_RegisterNil(t *testing.T) {
	codec := NewProtoCodec(nil)
	codec.Register(PacketCommand(800), nil) // protoMessage=nil
	if codec.MessageCreatorMap[800] != nil {
		t.Fatal("creator should be nil for nil protoMessage")
	}
}

func TestSimpleProtoCodec_RegisterNil(t *testing.T) {
	codec := NewSimpleProtoCodec()
	codec.Register(PacketCommand(801), nil) // protoMessage=nil
	if codec.MessageCreatorMap[801] != nil {
		t.Fatal("creator should be nil for nil protoMessage")
	}
}

// ==================== Heartbeat via Simple connection ====================

func TestTcpConnectionSimple_Heartbeat(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat),
		func(conn Connection, pkt Packet) {
			conn.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatRes{}))
		}, new(pb.HeartBeatReq))

	listenerCfg := &ListenerConfig{
		AcceptConfig: *defaultTestConfig(serverCodec, serverHandler),
		AcceptConnectionCreator: func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionSimpleAccept(conn, config)
		},
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat),
		func(conn Connection, pkt Packet) {}, new(pb.HeartBeatRes))
	clientHandler.RegisterHeartBeat(func() Packet {
		return NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatReq{})
	})
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.HeartBeatInterval = 1
	clientCfg.RecvTimeout = 5
	client := GetNetMgr().NewConnectorCustom(ctx, addr, clientCfg, "hb-simple",
		func(cfg *ConnectionConfig) Connection {
			return NewTcpConnectionSimple(cfg)
		})
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	time.Sleep(2500 * time.Millisecond)
	if !client.IsConnected() {
		t.Fatal("simple connection should be alive with heartbeat")
	}
}

// ==================== WS heartbeat ====================

func TestWsConnection_Heartbeat(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat),
		func(conn Connection, pkt Packet) {
			conn.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatRes{}))
		}, new(pb.HeartBeatReq))

	lcfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
		Path:            "/ws",
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
	listener := GetNetMgr().NewWsListener(ctx, addr, lcfg)
	defer listener.Close()

	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat),
		func(conn Connection, pkt Packet) {}, new(pb.HeartBeatRes))
	clientHandler.RegisterHeartBeat(func() Packet {
		return NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatReq{})
	})
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.Scheme = "ws"
	clientCfg.Path = "/ws"
	clientCfg.HeartBeatInterval = 1
	clientCfg.RecvTimeout = 5
	client := GetNetMgr().NewWsConnector(ctx, addr, clientCfg, "ws-hb")
	if client == nil {
		t.Fatal("ws connector failed")
	}
	defer client.Close()

	time.Sleep(2500 * time.Millisecond)
	if !client.IsConnected() {
		t.Fatal("ws connection should be alive with heartbeat")
	}
}

// ==================== RecvTimeout triggers close ====================

func TestTcpConnection_RecvTimeout(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	// 服务端不回复任何消息

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.RecvTimeout = 2 // 2秒超时
	clientCfg.HeartBeatInterval = 0 // 禁用心跳
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "recv-timeout")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	// 等待超过 RecvTimeout,连接应被关闭
	time.Sleep(3500 * time.Millisecond)
	if client.IsConnected() {
		t.Fatal("connection should be closed after recv timeout")
	}
}

func TestTcpConnectionSimple_RecvTimeout(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)

	listenerCfg := &ListenerConfig{
		AcceptConfig: *defaultTestConfig(serverCodec, serverHandler),
		AcceptConnectionCreator: func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionSimpleAccept(conn, config)
		},
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.RecvTimeout = 2
	clientCfg.HeartBeatInterval = 0
	client := GetNetMgr().NewConnectorCustom(ctx, addr, clientCfg, "simple-recv-timeout",
		func(cfg *ConnectionConfig) Connection {
			return NewTcpConnectionSimple(cfg)
		})
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	time.Sleep(3500 * time.Millisecond)
	if client.IsConnected() {
		t.Fatal("simple connection should be closed after recv timeout")
	}
}

// ==================== Rpc with proto message reply (not stream data) ====================

func TestRpc_WithProtoMessageReply(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			// 用已有 proto.Message 直接回复(网络层已反序列化)
			replyPkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
				&pb.TestMessage{Name: "direct reply", I32: 777})
			replyPkt.SetRpcCallId(pkt.RpcCallId())
			conn.SendPacket(replyPkt)
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpc-proto")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "req"})
	reply := &pb.TestMessage{}
	err := client.Rpc(req, reply, Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("Rpc error: %v", err)
	}
	if reply.Name != "direct reply" || reply.I32 != 777 {
		t.Fatalf("reply mismatch: %+v", reply)
	}
}

// ==================== Rpc type mismatch errors ====================

func TestRpc_ReplyTypeMismatch(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			replyPkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
				&pb.TestMessage{Name: "reply"})
			replyPkt.SetRpcCallId(pkt.RpcCallId())
			conn.SendPacket(replyPkt)
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	// 客户端也注册TestMessage,确保走Message()!=nil的描述符比较路径
	clientCodec := NewProtoCodec(nil)
	clientCodec.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage), new(pb.TestMessage))
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpc-type")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// reply 类型不匹配 -> "proto message type err"
	req2 := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{})
	wrongReply := &pb.HeartBeatRes{}
	err2 := client.Rpc(req2, wrongReply, Timeout(3*time.Second))
	if err2 == nil || err2.Error() != "proto message type err" {
		t.Fatalf("expected 'proto message type err', got: %v", err2)
	}
}

// ==================== WS Connect fail ====================

func TestWsConnection_ConnectFail(t *testing.T) {
	codec := NewSimpleProtoCodec()
	h := NewDefaultConnectionHandler(codec)
	h.SetOnConnectedFunc(func(conn Connection, success bool) {
		if success {
			t.Fatal("should not succeed")
		}
	})
	cfg := defaultTestConfig(codec, h)
	cfg.Scheme = "ws"
	cfg.Path = "/ws"
	conn := NewWsConnection(cfg)
	if conn.Connect("127.0.0.1:1") {
		t.Fatal("Connect should fail")
	}
	if conn.IsConnected() {
		t.Fatal("should not be connected")
	}
}

// ==================== Shutdown(true) ====================

func TestShutdown_WaitForAll(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		cancel()
		t.Fatal("listener failed")
	}

	// cancel ctx -> listener 和连接会关闭 -> wg.Done
	cancel()
	// Shutdown(true) 等待所有 goroutine 退出
	time.Sleep(200 * time.Millisecond)
	GetNetMgr().Shutdown(true)
}

// ==================== Concurrent send + close ====================

func TestConnection_ConcurrentSendAndClose(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.SendPacketCacheCap = 4
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "concurrent")
	if client == nil {
		t.Fatal("connector failed")
	}
	time.Sleep(100 * time.Millisecond)

	// 并发发送和关闭,不应 panic
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			client.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
				&pb.TestMessage{Name: "concurrent"}))
		}
	}()

	time.Sleep(50 * time.Millisecond)
	client.Close()
	<-done
}

// ==================== Mass send (batch encode path) ====================

func TestTcpConnection_BatchSend(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	var recvCount int32
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			atomic.AddInt32(&recvCount, 1)
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.SendPacketCacheCap = 256
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "batch")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// 快速发送大量消息,触发 writeLoop 的 batch encode 路径
	for i := 0; i < 50; i++ {
		client.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
			&pb.TestMessage{Name: "batch"}))
	}

	time.Sleep(500 * time.Millisecond)
	if atomic.LoadInt32(&recvCount) < 50 {
		t.Fatalf("received %d, want >= 50", recvCount)
	}
}

// ==================== Large packet send ====================

func TestTcpConnection_LargePacket(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	received := make(chan *pb.TestMessage, 1)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			received <- pkt.Message().(*pb.TestMessage)
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	// 用大 SendBufferSize 避免分包问题
	listenerCfg.AcceptConfig.SendBufferSize = 65536
	listenerCfg.AcceptConfig.RecvBufferSize = 65536
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.SendBufferSize = 65536
	clientCfg.RecvBufferSize = 65536
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "large")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// 构造中等大小包(触发 RingBuffer 的 wrap-around 分配路径)
	// 使用重复的ASCII字符避免UTF-8编码问题
	bigStr := make([]byte, 512)
	for i := range bigStr {
		bigStr[i] = byte('A' + (i % 26))
	}
	msg := &pb.TestMessage{Name: string(bigStr), I32: 12345}
	client.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), msg))

	select {
	case recvMsg := <-received:
		if len(recvMsg.Name) != 512 {
			t.Fatalf("name length mismatch: %d", len(recvMsg.Name))
		}
		if recvMsg.I32 != 12345 {
			t.Fatalf("i32 mismatch: %d", recvMsg.I32)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for large packet")
	}
}

// ==================== RegisterCreator frozen check ====================

func TestHandlerRegisterCreator_Frozen(t *testing.T) {
	h := NewDefaultConnectionHandler(nil)
	atomic.StoreInt32(&h.frozen, 1)

	// 冻结后 RegisterCreator 应静默 return(不 panic)
	h.RegisterCreator(PacketCommand(999), func(conn Connection, pkt Packet) {},
		func() proto.Message { return new(pb.TestMessage) })
	// 验证没有被注册
	if h.GetPacketHandler(PacketCommand(999)) != nil {
		t.Fatal("should not register after frozen")
	}
}

// ==================== rpcCall id overflow ====================

func TestRpcCall_IdZero(t *testing.T) {
	// 模拟 id 回绕到 0 的场景
	calls := newRpcCalls()
	// 手动设置 _rpcCallSerialId 接近 0
	atomic.StoreUint32(&_rpcCallSerialId, 0xFFFFFFFF)
	call := calls.newRpcCall()
	// id 应该跳过 0
	if call.id == 0 {
		t.Fatal("rpcCall id should never be 0")
	}
}
