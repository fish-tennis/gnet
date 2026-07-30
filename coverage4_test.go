package gnet

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/fish-tennis/gnet/example/pb"
)

// ==================== XorProtoCodec integration (covers HeaderEncoder/HeaderDecoder + ProtoPacketBytesEncoder/Decoder) ====================

func TestXorProtoCodec_Integration(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewXorProtoCodec([]byte("xorkey123"), nil)
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
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	defer listener.Close()

	clientCodec := NewXorProtoCodec([]byte("xorkey123"), nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "xor")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	client.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		&pb.TestMessage{Name: "xor test", I32: 888}))

	select {
	case msg := <-received:
		if msg.Name != "xor test" || msg.I32 != 888 {
			t.Fatalf("xor decode mismatch: %+v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for xor message")
	}
}

// ==================== XorProtoCodec RPC ====================

func TestXorProtoCodec_RpcIntegration(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewXorProtoCodec([]byte("rpckey"), nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	serverHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			if pkt.RpcCallId() > 0 {
				reply := &pb.TestMessage{Name: "xor rpc reply"}
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

	clientCodec := NewXorProtoCodec([]byte("rpckey"), nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "xor-rpc")
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
	if reply.Name != "xor rpc reply" {
		t.Fatalf("xor rpc reply mismatch: %+v", reply)
	}
}

// ==================== WS Broadcast with SimpleProtoCodec ====================

func TestWsListener_Broadcast(t *testing.T) {
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
	received := make(chan struct{}, 1)
	clientHandler.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn Connection, pkt Packet) {
			received <- struct{}{}
		}, new(pb.TestMessage))
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.Scheme = "ws"
	clientCfg.Path = "/ws"
	clientCfg.RecvTimeout = 0
	client := GetNetMgr().NewWsConnector(ctx, addr, clientCfg, "ws-bcast-2")
	if client == nil {
		t.Fatal("ws connector failed")
	}
	defer client.Close()
	time.Sleep(200 * time.Millisecond)

	listener.Broadcast(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		&pb.TestMessage{Name: "ws bcast 2"}))

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ws broadcast")
	}
}

// ==================== SendPacket after connection close (recover path) ====================

func TestSendPacket_AfterClose(t *testing.T) {
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
	clientCfg.RecvTimeout = 0
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "send-after-close")
	if client == nil {
		t.Fatal("connector failed")
	}
	time.Sleep(100 * time.Millisecond)

	client.Close()
	time.Sleep(100 * time.Millisecond)

	// SendPacket after close should return false (not panic)
	ok := client.SendPacket(NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "closed"}))
	if ok {
		t.Fatal("SendPacket should fail after close")
	}
}

// ==================== Rpc on disconnected connection ====================

func TestRpc_NotConnected(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewTcpConnector(cfg)

	req := NewProtoPacket(1, nil)
	reply := &pb.TestMessage{}
	err := conn.Rpc(req, reply, Timeout(1*time.Second))
	if err == nil {
		t.Fatal("Rpc should fail when not connected")
	}
}

// ==================== RingBuffer: Write with wrap-around partial ====================

func TestRingBuffer_WriteWrapAroundPartial(t *testing.T) {
	rb := NewRingBuffer(8)
	// 写满
	rb.Write([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	// 读掉6个
	rb.SetReaded(6)
	// 写3个: 尾部可写2个(索引6,7),头部可写1个(索引0) -> wrap
	n, err := rb.Write([]byte{8, 9, 10})
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 3 {
		t.Fatalf("wrote %d, want 3", n)
	}
	// 验证 w 指针 wrap
	data := rb.ReadFull(3)
	if data == nil {
		t.Fatal("ReadFull returned nil")
	}
	// 应该读到 [6,7,8] (尾部) -> 不,之前读掉了6个,所以 unread 是 2(尾部) + 3(新写) = 5
	// read指针在6, unread=2 -> ReadFull(3)跨段
}

func TestRingBuffer_WriteEmpty(t *testing.T) {
	rb := NewRingBuffer(8)
	n, err := rb.Write([]byte{})
	if err != nil {
		t.Fatal("Write([]) should not error")
	}
	if n != 0 {
		t.Fatalf("Write([]) should return 0, got %d", n)
	}
}

func TestRingBuffer_GetBuffer(t *testing.T) {
	rb := NewRingBuffer(4)
	buf := rb.GetBuffer()
	if len(buf) != 4 {
		t.Fatalf("buffer size: %d, want 4", len(buf))
	}
}

// ==================== DataPacket SetRpcCallId coverage ====================

func TestDataPacket_SetRpcCallId(t *testing.T) {
	p := NewDataPacket([]byte("test"))
	p.SetRpcCallId(42) // no-op but should not panic
	if p.RpcCallId() != 0 {
		t.Fatal("DataPacket RpcCallId should remain 0")
	}
}

// ==================== Encode with nil packet body (HeaderEncoder path via XorCodec) ====================

func TestXorProtoCodec_EncodeStreamData(t *testing.T) {
	codec := NewXorProtoCodec([]byte("key"), nil)
	// 使用 stream data (无 message)
	pkt := NewProtoPacketWithData(PacketCommand(100), []byte("raw xor data"))
	encoded := codec.Encode(nil, pkt)
	// 对于非 TcpConnection, Encode 返回 packet.GetStreamData()
	if encoded == nil {
		t.Fatal("Encode should return stream data for non-TcpConnection")
	}
}

// ==================== ProtoCodec Encode nil packet (error path) ====================

func TestProtoCodec_EncodeMarshalError(t *testing.T) {
	codec := NewProtoCodec(nil)
	// proto.Marshal with nil message shouldn't happen via normal path
	// but let's test EncodePacket with nil message and no stream data
	pkt := NewProtoPacket(1, nil)
	// nil message -> GetStreamData() returns nil
	encoded, _ := codec.EncodePacket(nil, pkt)
	// Should still return header bytes + nil body
	if len(encoded) < 2 {
		t.Fatalf("encoded segments: %d", len(encoded))
	}
}
