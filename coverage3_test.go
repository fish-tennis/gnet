package gnet

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fish-tennis/gnet/example/pb"
)

// ==================== WS RecvTimeout ====================

func TestWsConnection_RecvTimeout(t *testing.T) {
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
	clientCfg.RecvTimeout = 2
	clientCfg.HeartBeatInterval = 0 // 禁用心跳
	client := GetNetMgr().NewWsConnector(ctx, addr, clientCfg, "ws-recv-timeout")
	if client == nil {
		t.Fatal("ws connector failed")
	}
	defer client.Close()

	time.Sleep(3500 * time.Millisecond)
	if client.IsConnected() {
		t.Fatal("ws connection should be closed after recv timeout")
	}
}

// ==================== WS ctx.Done (closeNotify with connector close) ====================

func TestWsConnection_CtxDone(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())

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
	clientCfg.RecvTimeout = 0
	client := GetNetMgr().NewWsConnector(ctx, addr, clientCfg, "ws-ctxdone")
	if client == nil {
		t.Fatal("ws connector failed")
	}
	defer client.Close()

	time.Sleep(200 * time.Millisecond)
	if !client.IsConnected() {
		t.Fatal("ws should be connected")
	}

	// cancel ctx -> writeLoop 收到 ctx.Done -> connector 发 CloseMessage -> readLoop 退出
	cancel()
	time.Sleep(500 * time.Millisecond)
}

// ==================== SendPacket timeout branch ====================

func TestSendPacket_TimeoutBranch(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	// 服务端不消费消息,让客户端chan堆积

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listenerCfg.AcceptConfig.RecvTimeout = 0
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.SendPacketCacheCap = 1
	clientCfg.RecvTimeout = 0
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "send-timeout")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// 用很短的超时,触发 timeout 分支
	// writeLoop 消费很快,所以需要快速填充chan
	ok := client.SendPacket(
		NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "1"}),
		Timeout(1*time.Millisecond),
	)
	t.Logf("first send: %v", ok)
}

// ==================== SendPacket blocking + writeStopNotify ====================

func TestSendPacket_BlockUntilClose(t *testing.T) {
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
	clientCfg.SendPacketCacheCap = 1
	clientCfg.RecvTimeout = 0
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "block-close")
	if client == nil {
		t.Fatal("connector failed")
	}
	time.Sleep(100 * time.Millisecond)

	// 先关闭服务端连接,让客户端writeLoop失败退出
	// 客户端writeLoop退出后会close(writeStopNotifyChan)
	// 此时阻塞中的SendPacket应该通过writeStopNotifyChan返回false

	// 启动一个goroutine发送(timeout=0 blocking)
	result := make(chan bool, 1)
	go func() {
		// 填满chan后阻塞
		ok := client.SendPacket(
			NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "block"}),
		)
		result <- ok
	}()

	time.Sleep(100 * time.Millisecond)
	// 关闭连接,writeLoop会退出,close(writeStopNotifyChan)
	client.Close()

	select {
	case ok := <-result:
		if ok {
			t.Log("SendPacket returned true before close took effect")
		} else {
			t.Log("SendPacket returned false (writeStopNotify)")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SendPacket blocked forever")
	}
}

// ==================== TrySendPacket with timeout > 0 ====================

func TestTrySendPacket_WithTimeout(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {}, new(pb.TestMessage))

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
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "trysend-to")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// TrySendPacket with timeout > 0
	pkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "try-to"})
	ok := client.TrySendPacket(pkt, 2*time.Second)
	if !ok {
		t.Fatal("TrySendPacket should succeed")
	}
}

// ==================== WS server side connection close (server triggers disconnect) ====================

func TestWsServer_CloseConnection(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			// 服务端收到消息后关闭连接
			conn.Close()
		}, new(pb.TestMessage))

	disconnected := int32(0)
	lh := &testListenerHandler{
		onDisconnect: func(listener Listener, conn Connection) {
			atomic.AddInt32(&disconnected, 1)
		},
	}

	lcfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: lh,
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
	clientCfg.RecvTimeout = 0
	client := GetNetMgr().NewWsConnector(ctx, addr, clientCfg, "ws-srv-close")
	if client == nil {
		t.Fatal("ws connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// 发消息让服务端关闭连接
	client.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "close me"}))

	time.Sleep(500 * time.Millisecond)
	if atomic.LoadInt32(&disconnected) == 0 {
		t.Fatal("server should have detected disconnect")
	}
}

// ==================== Simple connection server-side close ====================

func TestTcpSimpleServer_CloseConnection(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			conn.Close()
		}, new(pb.TestMessage))

	disconnected := int32(0)
	lh := &testListenerHandler{
		onDisconnect: func(listener Listener, conn Connection) {
			atomic.AddInt32(&disconnected, 1)
		},
	}

	listenerCfg := &ListenerConfig{
		AcceptConfig: *defaultTestConfig(serverCodec, serverHandler),
		AcceptConnectionCreator: func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionSimpleAccept(conn, config)
		},
		ListenerHandler: lh,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.RecvTimeout = 0
	client := GetNetMgr().NewConnectorCustom(ctx, addr, clientCfg, "simple-srv-close",
		func(cfg *ConnectionConfig) Connection {
			return NewTcpConnectionSimple(cfg)
		})
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	client.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "close me"}))

	time.Sleep(500 * time.Millisecond)
	if atomic.LoadInt32(&disconnected) == 0 {
		t.Fatal("server should have detected disconnect")
	}
}

// ==================== TCP recv timeout with small nextTimeoutTime (clamp path) ====================

func TestTcpConnection_RecvTimeout_ClampNextTimeout(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	// 服务端回一条消息后不再回复,让 lastRecvPacketTick 更新,然后超时检测时 nextTimeoutTime > RecvTimeout 的 clamp 分支被触发
	var msgCount int32
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			if atomic.AddInt32(&msgCount, 1) == 1 {
				// 回复一条
				conn.SendPacket(pkt)
			}
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
	clientCfg.RecvTimeout = 3
	clientCfg.HeartBeatInterval = 0
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "clamp")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// 发一条消息,让 lastRecvPacketTick 更新到接近当前时间
	client.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "init"}))

	// 等待超过 RecvTimeout,连接应该超时关闭
	time.Sleep(4500 * time.Millisecond)
	if client.IsConnected() {
		t.Fatal("connection should timeout after RecvTimeout")
	}
}
