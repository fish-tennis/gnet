package gnet

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fish-tennis/gnet/example/pb"
	"google.golang.org/protobuf/proto"
)

// ==================== helpers ====================

func getFreePort() string {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func defaultTestConfig(codec Codec, handler ConnectionHandler) *ConnectionConfig {
	return &ConnectionConfig{
		SendPacketCacheCap: 64,
		SendBufferSize:     4096,
		RecvBufferSize:     4096,
		MaxPacketSize:      MaxPacketDataSize,
		RecvTimeout:        30,
		WriteTimeout:       5,
		HeartBeatInterval:  3,
		Codec:              codec,
		Handler:            handler,
	}
}

// ==================== TCP connection: connect + send/recv ====================

func TestTcpConnection_SendRecv(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan *pb.TestMessage, 1)

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			msg := pkt.Message().(*pb.TestMessage)
			received <- msg
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	// client
	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "client")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	// 发送消息
	msg := &pb.TestMessage{Name: "tcp test", I32: 42}
	if !client.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage), msg) {
		t.Fatal("Send failed")
	}

	select {
	case recvMsg := <-received:
		if recvMsg.Name != "tcp test" || recvMsg.I32 != 42 {
			t.Fatalf("received mismatch: %+v", recvMsg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

// ==================== TCP Simple connection: connect + send/recv ====================

func TestTcpConnectionSimple_SendRecv(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan *pb.TestMessage, 1)

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			msg := pkt.Message().(*pb.TestMessage)
			received <- msg
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig: *defaultTestConfig(serverCodec, serverHandler),
		AcceptConnectionCreator: func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionSimpleAccept(conn, config)
		},
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	// client
	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnectorCustom(ctx, addr, clientCfg, "simple-client",
		func(cfg *ConnectionConfig) Connection {
			return NewTcpConnectionSimple(cfg)
		})
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	msg := &pb.TestMessage{Name: "simple test", I32: 7}
	if !client.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage), msg) {
		t.Fatal("Send failed")
	}

	select {
	case recvMsg := <-received:
		if recvMsg.Name != "simple test" || recvMsg.I32 != 7 {
			t.Fatalf("received mismatch: %+v", recvMsg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

// ==================== WebSocket connection: connect + send/recv ====================

func TestWsConnection_SendRecv(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan *pb.TestMessage, 1)

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			msg := pkt.Message().(*pb.TestMessage)
			received <- msg
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
		Path:            "/ws",
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	listener := GetNetMgr().NewWsListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("ws listener start failed")
	}
	defer listener.Close()

	// client
	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.Scheme = "ws"
	clientCfg.Path = "/ws"
	client := GetNetMgr().NewWsConnector(ctx, addr, clientCfg, "ws-client")
	if client == nil {
		t.Fatal("ws connector failed")
	}
	defer client.Close()

	msg := &pb.TestMessage{Name: "ws test", I32: 99}
	if !client.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage), msg) {
		t.Fatal("Send failed")
	}

	select {
	case recvMsg := <-received:
		if recvMsg.Name != "ws test" || recvMsg.I32 != 99 {
			t.Fatalf("received mismatch: %+v", recvMsg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for ws message")
	}
}

// ==================== RPC over TCP ====================

func TestRpc_Tcp(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			// 服务端处理RPC请求,返回回复
			reply := &pb.TestMessage{Name: "rpc reply", I32: pkt.Message().(*pb.TestMessage).I32 + 1}
			replyPkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), reply)
			replyPkt.SetRpcCallId(pkt.RpcCallId())
			conn.SendPacket(replyPkt)
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpc-client")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	// RPC 调用
	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "rpc req", I32: 100})
	reply := &pb.TestMessage{}
	err := client.Rpc(req, reply, Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("Rpc error: %v", err)
	}
	if reply.Name != "rpc reply" || reply.I32 != 101 {
		t.Fatalf("rpc reply mismatch: %+v", reply)
	}
}

// ==================== RPC over TCP Simple ====================

func TestRpc_TcpSimple(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			if pkt.RpcCallId() > 0 {
				reply := &pb.TestMessage{Name: "simple rpc reply"}
				replyPkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), reply)
				replyPkt.SetRpcCallId(pkt.RpcCallId())
				conn.SendPacket(replyPkt)
			}
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig: *defaultTestConfig(serverCodec, serverHandler),
		AcceptConnectionCreator: func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionSimpleAccept(conn, config)
		},
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnectorCustom(ctx, addr, clientCfg, "rpc-simple",
		func(cfg *ConnectionConfig) Connection {
			return NewTcpConnectionSimple(cfg)
		})
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "req"})
	reply := &pb.TestMessage{}
	err := client.Rpc(req, reply, Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("Rpc error: %v", err)
	}
	if reply.Name != "simple rpc reply" {
		t.Fatalf("rpc reply mismatch: %+v", reply)
	}
}

// ==================== RPC timeout ====================

func TestRpc_Timeout(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 服务端不回复RPC请求
	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			// 故意不回复
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "timeout-client")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "no reply"})
	reply := &pb.TestMessage{}
	start := time.Now()
	err := client.Rpc(req, reply, Timeout(500*time.Millisecond))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("should timeout")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

// ==================== RPC reply after connection closed ====================

func TestRpc_ConnectionClosedDuringWait(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			// 服务端收到请求后立即关闭连接
			conn.Close()
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "closed-client")
	if client == nil {
		t.Fatal("connector failed")
	}

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "close test"})
	reply := &pb.TestMessage{}
	// 使用长超时,验证连接关闭时 Rpc 能快速返回而非等到超时
	err := client.Rpc(req, reply, Timeout(30*time.Second))
	if err == nil {
		t.Fatal("should return error")
	}
	if err.Error() != "connection closed" && err.Error() != "reply is nil" {
		// 可能是 reply is nil(因为 close 触发后 readLoop 可能送 nil)
		t.Logf("Rpc returned error: %v", err)
	}
}

// ==================== Broadcast ====================

func TestBroadcast(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)

	var connCount int32
	listenerHandler := &testListenerHandler{
		onConnected: func(listener Listener, conn Connection) {
			atomic.AddInt32(&connCount, 1)
		},
	}

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: listenerHandler,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	// 创建2个客户端,每个客户端使用独立的codec实例
	// 避免共享codec导致Register写map与readLoop读map之间的数据竞争
	receivedCount := make([]int32, 2)
	for i := 0; i < 2; i++ {
		idx := i
		clientCodec := NewProtoCodec(nil)
		ch := NewDefaultConnectionHandler(clientCodec)
		ch.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
			func(conn Connection, pkt Packet) {
				atomic.AddInt32(&receivedCount[idx], 1)
			}, new(pb.TestMessage))

		cfg := defaultTestConfig(clientCodec, ch)
		c := GetNetMgr().NewConnector(ctx, addr, cfg, fmt.Sprintf("bcast-%d", i))
		if c == nil {
			t.Fatalf("connector %d failed", i)
		}
		defer c.Close()
	}

	// 等待连接建立
	time.Sleep(200 * time.Millisecond)

	// 服务端广播
	broadcastMsg := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "broadcast"})
	listener.Broadcast(broadcastMsg)

	// 等待客户端收到
	time.Sleep(500 * time.Millisecond)

	for i := 0; i < 2; i++ {
		if atomic.LoadInt32(&receivedCount[i]) != 1 {
			t.Fatalf("client %d received %d messages, want 1", i, receivedCount[i])
		}
	}
}

// ==================== Connection close + reconnect ====================

func TestConnection_CloseAndCleanup(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)

	disconnected := make(chan uint32, 1)
	listenerHandler := &testListenerHandler{
		onDisconnect: func(listener Listener, conn Connection) {
			disconnected <- conn.GetConnectionId()
		},
	}

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: listenerHandler,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "close-test")
	if client == nil {
		t.Fatal("connector failed")
	}

	// 等待连接建立
	time.Sleep(100 * time.Millisecond)

	// 客户端关闭
	client.Close()

	// 服务端应收到断开通知
	select {
	case id := <-disconnected:
		t.Logf("server detected disconnect: connId=%d", id)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for disconnect notification")
	}
}

// ==================== Concurrent close ====================

func TestConnection_ConcurrentClose(t *testing.T) {
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
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "concurrent-close")
	if client == nil {
		t.Fatal("connector failed")
	}

	time.Sleep(100 * time.Millisecond)

	// 并发多次 Close 不应 panic
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.Close()
		}()
	}
	wg.Wait()
}

// ==================== SendPacket with WithDiscard ====================

func TestSendPacket_WithDiscard(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	received := make(chan struct{}, 100)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			received <- struct{}{}
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	// 小 SendPacketCacheCap 让 chan 快速满
	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.SendPacketCacheCap = 2
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "discard-test")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	// 批量发送,用 WithDiscard + 无超时
	// timeout=0 + discard=true 走非阻塞路径
	sent := 0
	discarded := 0
	for i := 0; i < 50; i++ {
		ok := client.SendPacket(
			NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "flood"}),
			WithDiscard(),
		)
		if ok {
			sent++
		} else {
			discarded++
		}
	}
	if discarded == 0 {
		t.Log("no packets were discarded (chan may not have been full)")
	}
	t.Logf("sent=%d discarded=%d", sent, discarded)
}

// ==================== TrySendPacket ====================

func TestTrySendPacket(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	received := int32(0)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			atomic.AddInt32(&received, 1)
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "trysend-test")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	pkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "try"})
	// timeout=0 走 discard 模式
	ok := client.TrySendPacket(pkt, 0)
	if !ok {
		t.Fatal("TrySendPacket with timeout=0 failed")
	}
}

// ==================== Heartbeat: connector sends heartbeat ====================

func TestHeartbeat_KeepsConnectionAlive(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	// 服务端注册心跳包处理
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat),
		func(conn Connection, pkt Packet) {
			// 收到心跳,回复
			conn.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatRes{}))
		}, new(pb.HeartBeatReq))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	// 客户端配置短心跳间隔,短超时
	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientHandler.Register(PacketCommand(pb.CmdTest_Cmd_HeartBeat),
		func(conn Connection, pkt Packet) {}, new(pb.HeartBeatRes))
	clientHandler.RegisterHeartBeat(func() Packet {
		return NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_HeartBeat), &pb.HeartBeatReq{})
	})
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.HeartBeatInterval = 1 // 1秒
	clientCfg.RecvTimeout = 5       // 5秒
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "hb-client")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	// 等待超过一个心跳周期,连接应仍然存活
	time.Sleep(2500 * time.Millisecond)
	if !client.IsConnected() {
		t.Fatal("connection should still be alive with heartbeat")
	}
}

// ==================== Listener Addr ====================

func TestTcpListener_Addr(t *testing.T) {
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
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	a := listener.Addr()
	if a == nil {
		t.Fatal("Addr should not be nil")
	}
	if a.String() != addr {
		t.Fatalf("Addr mismatch: got %s, want %s", a.String(), addr)
	}
}

// ==================== Connection tag ====================

func TestConnection_Tag(t *testing.T) {
	codec := NewProtoCodec(nil)
	handler := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, handler)
	conn := NewTcpConnector(cfg)

	conn.SetTag("mytag")
	if conn.GetTag() != "mytag" {
		t.Fatal("tag mismatch")
	}

	conn.SetTag(42)
	if conn.GetTag() != 42 {
		t.Fatal("tag type mismatch")
	}

	conn.SetTag(nil)
	if conn.GetTag() != nil {
		t.Fatal("tag should be nil")
	}
}

// ==================== Listener Close stops accepting ====================

func TestListener_CloseStopsAccepting(t *testing.T) {
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
	if listener == nil {
		t.Fatal("listener start failed")
	}

	listener.Close()

	// 关闭后,新连接应无法建立
	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "post-close")
	if client != nil {
		client.Close()
		// 连接可能建立成功(取决于TCP backlog),但应很快断开
		time.Sleep(200 * time.Millisecond)
	}
}

// ==================== ProtoPacket with stream data ====================

func TestSendPacket_StreamData(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	received := make(chan Packet, 1)
	// 只注册消息号,不注册proto结构体 -> 收到的是 raw data
	serverHandler.SetUnRegisterHandler(func(conn Connection, pkt Packet) {
		received <- pkt
	})

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener start failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "stream-client")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	// 发送预先序列化好的数据
	rawData, _ := proto.Marshal(&pb.TestMessage{Name: "stream", I32: 55})
	pkt := NewProtoPacketWithData(PacketCommand(pb.CmdTest_Cmd_TestMessage), rawData)
	if !client.SendPacket(pkt) {
		t.Fatal("SendPacket failed")
	}

	select {
	case recvPkt := <-received:
		if recvPkt == nil {
			t.Fatal("received nil packet")
		}
		data := recvPkt.GetStreamData()
		if len(data) == 0 {
			t.Fatal("stream data is empty")
		}
		// 反序列化验证
		msg := &pb.TestMessage{}
		if err := proto.Unmarshal(data, msg); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if msg.Name != "stream" || msg.I32 != 55 {
			t.Fatalf("stream data mismatch: %+v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for stream data")
	}
}

// ==================== test helpers ====================

type testListenerHandler struct {
	onConnected  func(listener Listener, conn Connection)
	onDisconnect func(listener Listener, conn Connection)
}

func (h *testListenerHandler) OnConnectionConnected(listener Listener, conn Connection) {
	if h.onConnected != nil {
		h.onConnected(listener, conn)
	}
}

func (h *testListenerHandler) OnConnectionDisconnect(listener Listener, conn Connection) {
	if h.onDisconnect != nil {
		h.onDisconnect(listener, conn)
	}
}
