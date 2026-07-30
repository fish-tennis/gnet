package gnet

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fish-tennis/gnet/example/pb"
	"google.golang.org/protobuf/proto"
)

// ==================== packet.go coverage ====================

func TestNewProtoPacketEx_AllTypes(t *testing.T) {
	// 测试所有支持的参数类型
	p := NewProtoPacketEx(
		nil,                     // 跳过 nil
		PacketCommand(100),      // PacketCommand
		uint16(200),             // uint16
		int(300),                // int
		int16(400),              // int16
		int32(500),              // int32
		int64(600),              // int64
		uint(700),               // uint
		uint32(800),             // uint32
		uint64(900),             // uint64
		&pb.TestMessage{Name: "ex"}, // proto.Message
		[]byte("raw"),           // []byte
		3.14,                    // unsupported type -> logger.Error
	)
	if p == nil {
		t.Fatal("packet is nil")
	}
	// 最后设置的 command 类型生效(按顺序覆盖)
	t.Logf("command: %v", p.Command())
	if p.Message() == nil {
		t.Fatal("message should not be nil")
	}
	if string(p.GetStreamData()) != "raw" {
		t.Fatal("stream data mismatch")
	}
}

func TestProtoPacketEx_WithEnum(t *testing.T) {
	// protoreflect.Enum 类型
	p := NewProtoPacketEx(pb.CmdTest_Cmd_TestMessage, &pb.TestMessage{Name: "enum"})
	if p.Command() != PacketCommand(pb.CmdTest_Cmd_TestMessage) {
		t.Fatalf("command mismatch: %v", p.Command())
	}
}

func TestProtoPacket_WithRpc(t *testing.T) {
	p := NewProtoPacket(1, nil)
	p.WithRpc(uint32(42))
	if p.RpcCallId() != 42 {
		t.Fatalf("rpcCallId mismatch: %d", p.RpcCallId())
	}

	// WithRpc with Packet
	p2 := NewProtoPacket(2, nil)
	p2.SetRpcCallId(99)
	p3 := NewProtoPacket(3, nil)
	p3.WithRpc(p2)
	if p3.RpcCallId() != 99 {
		t.Fatalf("rpcCallId from packet mismatch: %d", p3.RpcCallId())
	}
}

func TestProtoPacket_CloneWithData(t *testing.T) {
	p := NewProtoPacketWithData(10, []byte("stream data"))
	clone := p.Clone().(*ProtoPacket)
	if string(clone.GetStreamData()) != "stream data" {
		t.Fatal("clone stream data mismatch")
	}
}

func TestDefaultPacketHeader_SetFlags(t *testing.T) {
	h := NewDefaultPacketHeader(100, 0)
	h.SetFlags(0xFF)
	if h.Flags() != 0xFF {
		t.Fatalf("flags mismatch: %d", h.Flags())
	}
	if h.Len() != 100 {
		t.Fatalf("len changed after SetFlags: %d", h.Len())
	}
}

func TestDefaultPacketHeader_AddFlags(t *testing.T) {
	h := NewDefaultPacketHeader(100, 0x01)
	h.AddFlags(0x02)
	if h.Flags() != 0x03 {
		t.Fatalf("flags mismatch: %d", h.Flags())
	}
}

func TestSimplePacketHeader_SetFlags(t *testing.T) {
	h := NewSimplePacketHeader(0, 0, 0)
	h.SetFlags(0x05)
	if h.Flags() != 0x05 {
		t.Fatalf("flags mismatch: %d", h.Flags())
	}
}

func TestSimplePacketHeader_AddFlags(t *testing.T) {
	h := NewSimplePacketHeader(0, 0x01, 0)
	h.AddFlags(0x04)
	if h.Flags() != 0x05 {
		t.Fatalf("flags mismatch: %d", h.Flags())
	}
}

func TestDataPacket_AllMethods(t *testing.T) {
	p := NewDataPacket([]byte("test"))
	if p.Command() != 0 {
		t.Fatal("DataPacket command should be 0")
	}
	if p.Message() != nil {
		t.Fatal("DataPacket message should be nil")
	}
	if p.RpcCallId() != 0 {
		t.Fatal("DataPacket rpcCallId should be 0")
	}
	p.SetRpcCallId(99) // no-op, should not panic
	if p.RpcCallId() != 0 {
		t.Fatal("DataPacket rpcCallId should still be 0")
	}
	if p.ErrorCode() != 0 {
		t.Fatal("DataPacket errorCode should be 0")
	}
	p.SetErrorCode(99) // no-op
	if p.ErrorCode() != 0 {
		t.Fatal("DataPacket errorCode should still be 0")
	}
}

func TestNewDataPacketWithHeader(t *testing.T) {
	h := NewDefaultPacketHeader(10, 0)
	p := NewDataPacketWithHeader(h, []byte("data"))
	if string(p.GetStreamData()) != "data" {
		t.Fatal("data mismatch")
	}
}

// ==================== ring_buffer.go coverage ====================

func TestRingBuffer_ReadFull_NonContinuous(t *testing.T) {
	rb := NewRingBuffer(8)
	// 写满
	rb.Write([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	// 读掉前6个,使写指针wrap
	rb.SetReaded(6)
	// 再写6个,数据不连续(尾部2 + 头部6)
	rb.Write([]byte{8, 9, 10, 11, 12, 13})
	// 此时 UnReadLength=8,但 ReadBuffer 只返回尾部2个
	if rb.UnReadLength() != 8 {
		t.Fatalf("unread length: %d", rb.UnReadLength())
	}
	// ReadFull(5) 跨越尾部和头部,需要 make+双拷贝
	data := rb.ReadFull(5)
	if data == nil {
		t.Fatal("ReadFull returned nil")
	}
	// 期望: 尾部 [6,7] + 头部 [8,9,10]
	expected := []byte{6, 7, 8, 9, 10}
	for i, v := range expected {
		if data[i] != v {
			t.Fatalf("byte %d mismatch: got %d, want %d", i, data[i], v)
		}
	}
}

func TestRingBuffer_ReadFull_TooMuch(t *testing.T) {
	rb := NewRingBuffer(8)
	rb.Write([]byte{1, 2, 3})
	data := rb.ReadFull(5) // UnReadLength < 5
	if data != nil {
		t.Fatal("ReadFull should return nil when not enough data")
	}
}

func TestRingBuffer_Write_PartialWrite(t *testing.T) {
	rb := NewRingBuffer(4)
	n, _ := rb.Write([]byte{1, 2, 3, 4})
	if n != 4 {
		t.Fatalf("wrote %d, want 4", n)
	}
	// buffer满,写入0
	n, _ = rb.Write([]byte{5})
	if n != 0 {
		t.Fatalf("wrote %d on full buffer, want 0", n)
	}
	// 读掉2个,再写1个
	rb.SetReaded(2)
	n, _ = rb.Write([]byte{5})
	if n != 1 {
		t.Fatalf("wrote %d, want 1", n)
	}
}

func TestRingBuffer_Size(t *testing.T) {
	rb := NewRingBuffer(16)
	if rb.Size() != 16 {
		t.Fatalf("size: %d, want 16", rb.Size())
	}
}

// ==================== connection.go coverage ====================

func TestBaseConnection_SetCodec(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewTcpConnector(cfg)

	newCodec := NewProtoCodec(nil)
	conn.SetCodec(newCodec)
	if conn.GetCodec() != newCodec {
		t.Fatal("SetCodec failed")
	}
}

func TestBaseConnection_GetHandler(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewTcpConnector(cfg)

	if conn.GetHandler() != h {
		t.Fatal("GetHandler mismatch")
	}
}

func TestBaseConnection_GetSendPacketChanLen(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	cfg.SendPacketCacheCap = 4
	conn := NewTcpConnector(cfg)

	if conn.GetSendPacketChanLen() != 0 {
		t.Fatal("chan len should be 0")
	}
}

func TestSendPacket_NotConnected(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewTcpConnector(cfg)
	// 未连接,SendPacket 应返回 false
	if conn.SendPacket(NewProtoPacket(1, nil)) {
		t.Fatal("SendPacket should fail when not connected")
	}
}

func TestSendPacket_Timeout(t *testing.T) {
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
		t.Fatal("listener failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	clientCfg.SendPacketCacheCap = 1 // 很小的缓存
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "timeout-send")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// 发一个包填满 chan (writeLoop 会消费),但用极短超时
	ok := client.SendPacket(
		NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "t"}),
		Timeout(1*time.Millisecond),
	)
	// 可能成功也可能超时,取决于 writeLoop 消费速度
	t.Logf("SendPacket with 1ms timeout: %v", ok)
}

func TestShutdown(t *testing.T) {
	// Shutdown(false) 不等待
	GetNetMgr().Shutdown(false)
}

// ==================== handler.go coverage ====================

func TestHandler_OnConnected(t *testing.T) {
	h := NewDefaultConnectionHandler(nil)
	called := false
	h.SetOnConnectedFunc(func(conn Connection, success bool) {
		called = true
	})
	h.OnConnected(nil, true)
	if !called {
		t.Fatal("OnConnected callback not called")
	}
}

func TestHandler_OnDisconnected(t *testing.T) {
	h := NewDefaultConnectionHandler(nil)
	called := false
	h.SetOnDisconnectedFunc(func(conn Connection) {
		called = true
	})
	h.OnDisconnected(nil)
	if !called {
		t.Fatal("OnDisconnected callback not called")
	}
}

func TestHandler_UnRegisterHandler(t *testing.T) {
	h := NewDefaultConnectionHandler(nil)
	called := false
	h.SetUnRegisterHandler(func(conn Connection, pkt Packet) {
		called = true
	})
	// 发一个未注册的消息
	h.OnRecvPacket(nil, NewProtoPacket(999, nil))
	if !called {
		t.Fatal("UnRegisterHandler not called")
	}
}

func TestHandler_OnRecvPacketPanic(t *testing.T) {
	h := NewDefaultConnectionHandler(nil)
	h.Register(PacketCommand(1), func(conn Connection, pkt Packet) {
		panic("test panic")
	}, nil)
	// 不应 crash,defer recover 应捕获
	h.OnRecvPacket(nil, NewProtoPacket(1, nil))
}

// ==================== SimpleProtoCodec coverage ====================

func TestSimpleProtoCodec_RegisterCreator(t *testing.T) {
	codec := NewSimpleProtoCodec()
	cmd := PacketCommand(500)
	codec.RegisterCreator(cmd, func() proto.Message { return new(pb.TestMessage) })
	if codec.MessageCreatorMap[cmd] == nil {
		t.Fatal("creator not registered")
	}
}

func TestSimpleProtoCodec_EncodeWithRpcAndError(t *testing.T) {
	codec := NewSimpleProtoCodec()

	// RPC + ErrorCode 包
	pkt := NewProtoPacket(PacketCommand(1), &pb.TestMessage{Name: "rpc+err"})
	pkt.SetRpcCallId(42)
	pkt.SetErrorCode(500)

	encoded := codec.Encode(nil, pkt)
	if encoded == nil {
		t.Fatal("Encode returned nil")
	}
	// rpcCallId(4) + errorCode(4) + body > 8 bytes
	if len(encoded) <= 8 {
		t.Fatalf("encoded too short for rpc+error: %d", len(encoded))
	}
}

func TestSimpleProtoCodec_EncodeStreamData(t *testing.T) {
	codec := NewSimpleProtoCodec()
	// 无 message,有 stream data
	pkt := NewProtoPacketWithData(1, []byte("raw stream"))
	encoded := codec.Encode(nil, pkt)
	if string(encoded) != "raw stream" {
		t.Fatalf("encoded mismatch: %v", encoded)
	}
}

func TestSimpleProtoCodec_DecodeRpcAndError(t *testing.T) {
	codec := NewSimpleProtoCodec()
	cmd := PacketCommand(600)

	// 构造含 rpcCallId + errorCode 的包,不注册消息(走 raw data 路径)
	body := []byte{0x0a, 0x01, 0x41} // valid proto: field 1, len 1, "A"
	fullData := make([]byte, SimplePacketHeaderSize+4+4+len(body))
	header := NewSimplePacketHeader(uint32(len(body)), RpcCall|ErrorCode, cmd)
	header.WriteTo(fullData)
	offset := SimplePacketHeaderSize
	fullData[offset] = 42
	fullData[offset+4] = 0xFF
	copy(fullData[offset+8:], body)

	pkt, err := codec.Decode(nil, fullData)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if pkt == nil {
		t.Fatal("Decode returned nil")
	}
	if pkt.RpcCallId() != 42 {
		t.Fatalf("rpcCallId: %d", pkt.RpcCallId())
	}
	if pkt.ErrorCode() != 0xFF {
		t.Fatalf("errorCode: %d", pkt.ErrorCode())
	}
}

// ==================== LocalAddr/RemoteAddr coverage ====================

func TestTcpConnection_AddrNotConnected(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewTcpConnector(cfg)
	if conn.LocalAddr() != nil {
		t.Fatal("LocalAddr should be nil before connect")
	}
	if conn.RemoteAddr() != nil {
		t.Fatal("RemoteAddr should be nil before connect")
	}
}

func TestTcpConnectionSimple_AddrNotConnected(t *testing.T) {
	codec := NewSimpleProtoCodec()
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewTcpConnectionSimple(cfg)
	if conn.LocalAddr() != nil {
		t.Fatal("LocalAddr should be nil before connect")
	}
}

func TestWsConnection_AddrNotConnected(t *testing.T) {
	codec := NewSimpleProtoCodec()
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewWsConnection(cfg)
	if conn.LocalAddr() != nil {
		t.Fatal("LocalAddr should be nil before connect")
	}
}

func TestWsConnection_GetConn(t *testing.T) {
	codec := NewSimpleProtoCodec()
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewWsConnection(cfg)
	if conn.GetConn() != nil {
		t.Fatal("GetConn should be nil before connect")
	}
}

// ==================== NetMgr coverage ====================

func TestNetMgr_NewWsListenerFail(t *testing.T) {
	// 用一个已占用的端口让 Listen 失败
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 先占用端口
	codec := NewSimpleProtoCodec()
	h := NewDefaultConnectionHandler(codec)
	lcfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(codec, h),
		ListenerHandler: nil,
		Path:            "/ws",
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
	l1 := GetNetMgr().NewWsListener(ctx, addr, lcfg)
	if l1 == nil {
		t.Fatal("first listener failed")
	}
	defer l1.Close()

	// 第二个 listener 用相同地址应失败
	l2 := GetNetMgr().NewWsListener(ctx, addr, lcfg)
	if l2 != nil {
		l2.Close()
		t.Fatal("second listener should fail on same port")
	}
}

// ==================== TcpListener coverage ====================

func TestTcpListener_GetConnection(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	codec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(codec)
	connected := make(chan Connection, 1)
	listenerHandler := &testListenerHandler{
		onConnected: func(listener Listener, conn Connection) {
			connected <- conn
		},
	}

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(codec, serverHandler),
		ListenerHandler: listenerHandler,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener failed")
	}
	defer listener.Close()

	clientCfg := defaultTestConfig(NewProtoCodec(nil), NewDefaultConnectionHandler(NewProtoCodec(nil)))
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "getconn-test")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()

	select {
	case conn := <-connected:
		// 验证 GetConnection
		found := listener.GetConnection(conn.GetConnectionId())
		if found == nil {
			t.Fatal("GetConnection returned nil")
		}
		// 验证不存在的 id
		notFound := listener.GetConnection(999999)
		if notFound != nil {
			t.Fatal("GetConnection should return nil for non-existent id")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connection")
	}
}

func TestTcpListener_RangeConnections(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	codec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(codec)
	var connCount int32
	listenerHandler := &testListenerHandler{
		onConnected: func(listener Listener, conn Connection) {
			atomic.AddInt32(&connCount, 1)
		},
	}

	listenerCfg := &ListenerConfig{
		AcceptConfig:    *defaultTestConfig(codec, serverHandler),
		ListenerHandler: listenerHandler,
	}
	listener := GetNetMgr().NewListener(ctx, addr, listenerCfg)
	if listener == nil {
		t.Fatal("listener failed")
	}
	defer listener.Close()

	clientCfg := defaultTestConfig(NewProtoCodec(nil), NewDefaultConnectionHandler(NewProtoCodec(nil)))
	c := GetNetMgr().NewConnector(ctx, addr, clientCfg, "range-test")
	if c == nil {
		t.Fatal("connector failed")
	}
	defer c.Close()

	time.Sleep(200 * time.Millisecond)

	// RangeConnections (TcpListener specific)
	tcpListener := listener.(*TcpListener)
	count := 0
	tcpListener.RangeConnections(func(conn Connection) bool {
		count++
		return true
	})
	if count == 0 {
		t.Fatal("RangeConnections found 0 connections")
	}

	// RangeConnections with early stop
	count2 := 0
	tcpListener.RangeConnections(func(conn Connection) bool {
		count2++
		return false // stop immediately
	})
	if count2 != 1 {
		t.Fatalf("RangeConnections early stop: expected 1, got %d", count2)
	}
}

// ==================== ConnectionConfig coverage ====================

func TestTcpConnection_ConnectFail(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cfg := defaultTestConfig(codec, h)
	conn := NewTcpConnector(cfg)
	// 连接一个不存在的地址(windows上端口1通常被拒绝)
	if conn.Connect("127.0.0.1:1") {
		t.Fatal("Connect should fail")
	}
	if conn.IsConnected() {
		t.Fatal("should not be connected")
	}
}

func TestTcpConnectionSimple_ConnectFail(t *testing.T) {
	codec := NewSimpleProtoCodec()
	h := NewDefaultConnectionHandler(codec)
	h.SetOnConnectedFunc(func(conn Connection, success bool) {
		if success {
			t.Fatal("should not succeed")
		}
	})
	cfg := defaultTestConfig(codec, h)
	conn := NewTcpConnectionSimple(cfg)
	if conn.Connect("127.0.0.1:1") {
		t.Fatal("Connect should fail")
	}
}

// ==================== Send with various options ====================

func TestSend_WithInfiniteTimeout(t *testing.T) {
	addr := getFreePort()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverCodec := NewProtoCodec(nil)
	serverHandler := NewDefaultConnectionHandler(serverCodec)
	received := make(chan struct{}, 1)
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
		t.Fatal("listener failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "inf-timeout")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// WithInfiniteTimeout 设置 timeout=MaxInt64
	ok := client.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "inf"},
		WithInfiniteTimeout())
	if !ok {
		t.Fatal("Send with WithInfiniteTimeout failed")
	}

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestSend_WithBlock(t *testing.T) {
	// WithBlock 当前是 TODO,仅验证不 panic
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
	if listener == nil {
		t.Fatal("listener failed")
	}
	defer listener.Close()

	clientCodec := NewProtoCodec(nil)
	clientHandler := NewDefaultConnectionHandler(clientCodec)
	clientCfg := defaultTestConfig(clientCodec, clientHandler)
	client := GetNetMgr().NewConnector(ctx, addr, clientCfg, "block-test")
	if client == nil {
		t.Fatal("connector failed")
	}
	defer client.Close()
	time.Sleep(100 * time.Millisecond)

	// WithBlock 不 panic 即可
	client.Send(PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{Name: "block"},
		WithBlock())
}
