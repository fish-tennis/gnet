package gnet

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/fish-tennis/gnet/example/pb"
)

// ==================== RpcTimeout 基本功能:请求+回复 ====================

func TestRpcTimeout_Success(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			if pkt.RpcCallId() > 0 {
				reply := &pb.TestMessage{Name: "rpctimeout reply", I32: pkt.Message().(*pb.TestMessage).I32 + 1}
				replyPkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), reply)
				replyPkt.SetRpcCallId(pkt.RpcCallId())
				conn.SendPacket(replyPkt)
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
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpctimeout-success")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "req", I32: 50})
	reply := &pb.TestMessage{}
	err := client.RpcTimeout(req, reply, 5*time.Second, Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("RpcTimeout error: %v", err)
	}
	if reply.Name != "rpctimeout reply" || reply.I32 != 51 {
		t.Fatalf("reply mismatch: %+v", reply)
	}
}

// ==================== RpcTimeout replyTimeout=0 (使用DefaultRpcTimeout) ====================

func TestRpcTimeout_ReplyTimeoutZero(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			if pkt.RpcCallId() > 0 {
				reply := &pb.TestMessage{Name: "zero reply timeout"}
				replyPkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), reply)
				replyPkt.SetRpcCallId(pkt.RpcCallId())
				conn.SendPacket(replyPkt)
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
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpctimeout-zero")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "req"})
	reply := &pb.TestMessage{}
	// replyTimeout=0 -> 使用DefaultRpcTimeout
	err := client.RpcTimeout(req, reply, 0, Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("RpcTimeout error: %v", err)
	}
	if reply.Name != "zero reply timeout" {
		t.Fatalf("reply mismatch: %+v", reply)
	}
}

// ==================== RpcTimeout 回复超时 ====================

func TestRpcTimeout_ReplyTimeoutExpired(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	// 服务端不回复RPC请求
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			// 故意不回复
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
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpctimeout-expired")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "no reply"})
	reply := &pb.TestMessage{}
	// 自定义replyTimeout为300ms
	start := time.Now()
	err := client.RpcTimeout(req, reply, 300*time.Millisecond, Timeout(5*time.Second))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("should timeout")
	}
	if err.Error() != "timeout" {
		t.Fatalf("expected 'timeout', got: %v", err)
	}
	// 验证确实是300ms左右超时,而非DefaultRpcTimeout(3s)
	if elapsed > 2*time.Second {
		t.Fatalf("reply timeout took too long: %v, should be ~300ms", elapsed)
	}
}

// ==================== RpcTimeout 未连接 ====================

func TestRpcTimeout_NotConnected(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewTcpConnector(cfg)

	req := NewProtoPacket(1, nil)
	reply := &pb.TestMessage{}
	err := conn.RpcTimeout(req, reply, 1*time.Second, Timeout(1*time.Second))
	if err == nil {
		t.Fatal("RpcTimeout should fail when not connected")
	}
	if err.Error() != "disconnected" {
		t.Fatalf("expected 'disconnected', got: %v", err)
	}
}

// ==================== RpcTimeout 连接关闭(等待回复期间) ====================

func TestRpcTimeout_ConnectionClosedDuringWait(t *testing.T) {
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
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpctimeout-closed")
	if client == nil {
		t.Fatal("connector failed")
	}

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "close test"})
	reply := &pb.TestMessage{}
	// 用长replyTimeout,验证连接关闭时 RpcTimeout 能快速返回
	err := client.RpcTimeout(req, reply, 30*time.Second, Timeout(5*time.Second))
	if err == nil {
		t.Fatal("should return error")
	}
	t.Logf("RpcTimeout returned error: %v", err)
}

// ==================== RpcTimeout 回复消息类型不匹配 ====================

func TestRpcTimeout_ReplyTypeMismatch(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			if pkt.RpcCallId() > 0 {
				replyPkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
					&pb.TestMessage{Name: "reply"})
				replyPkt.SetRpcCallId(pkt.RpcCallId())
				conn.SendPacket(replyPkt)
			}
		}, new(pb.TestMessage))

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	// 客户端也注册,确保走 Message()!=nil 的描述符比较路径
	clientCodec := NewProtoCodec(nil)
	clientCodec.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage), new(pb.TestMessage))
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpctimeout-type")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{})
	wrongReply := &pb.HeartBeatRes{}
	err := client.RpcTimeout(req, wrongReply, 3*time.Second, Timeout(5*time.Second))
	if err == nil || err.Error() != "proto message type err" {
		t.Fatalf("expected 'proto message type err', got: %v", err)
	}
}

// ==================== RpcTimeout over TcpSimple ====================

func TestRpcTimeout_TcpSimple(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewSimpleProtoCodec()
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			if pkt.RpcCallId() > 0 {
				reply := &pb.TestMessage{Name: "simple rpctimeout reply"}
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
	defer listener.Close()

	clientCodec := NewSimpleProtoCodec()
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnectorCustom(ctx, addr, clientCfg, "rpctimeout-simple",
		func(cfg *ConnectionConfig) Connection {
			return NewTcpConnectionSimple(cfg)
		})
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "simple req"})
	reply := &pb.TestMessage{}
	err := client.RpcTimeout(req, reply, 5*time.Second, Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("RpcTimeout error: %v", err)
	}
	if reply.Name != "simple rpctimeout reply" {
		t.Fatalf("reply mismatch: %+v", reply)
	}
}

// ==================== RpcTimeout write超时 ====================

func TestRpcTimeout_WriteTimeout(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			if pkt.RpcCallId() > 0 {
				replyPkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "reply"})
				replyPkt.SetRpcCallId(pkt.RpcCallId())
				conn.SendPacket(replyPkt)
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
	clientCfg.SendPacketCacheCap = 1
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpctimeout-wtimeout")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// 用极短的writeTimeout
	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "req"})
	reply := &pb.TestMessage{}
	err := client.RpcTimeout(req, reply, 1*time.Second, Timeout(1*time.Millisecond))
	// 结果可能是nil(写入快)或"write timeout"或"timeout"
	t.Logf("RpcTimeout with 1ms write timeout returned: %v", err)
}

// ==================== Rpc write超时(opts中的Timeout) ====================

func TestRpc_WriteTimeout(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			if pkt.RpcCallId() > 0 {
				replyPkt := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "reply"})
				replyPkt.SetRpcCallId(pkt.RpcCallId())
				conn.SendPacket(replyPkt)
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
	clientCfg.SendPacketCacheCap = 1
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpc-wtimeout")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// Rpc的opts Timeout控制写入sendPacketCache的超时
	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "req"})
	reply := &pb.TestMessage{}
	err := client.Rpc(req, reply, Timeout(1*time.Millisecond))
	t.Logf("Rpc with 1ms write timeout returned: %v", err)
}

// ==================== RpcTimeout 验证回复超时和写入超时是独立的 ====================

func TestRpcTimeout_IndependentTimeouts(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	// 服务端不回复
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			// 故意不回复
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
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "rpctimeout-independent")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// writeTimeout用5s(足够长),replyTimeout用200ms(很短)
	// 如果两个超时混淆了,实际等待时间会是5s而不是200ms
	req := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "req"})
	reply := &pb.TestMessage{}
	start := time.Now()
	err := client.RpcTimeout(req, reply, 200*time.Millisecond, Timeout(5*time.Second))
	elapsed := time.Since(start)
	if err == nil || err.Error() != "timeout" {
		t.Fatalf("expected 'timeout', got: %v", err)
	}
	// 应该在200ms左右返回,而不是5s
	if elapsed > 1*time.Second {
		t.Fatalf("elapsed %v too long, replyTimeout(200ms) was likely ignored", elapsed)
	}
	t.Logf("elapsed: %v", elapsed)
}
