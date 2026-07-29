package gnet

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fish-tennis/gnet/example/pb"
	"google.golang.org/protobuf/proto"
)

// ==================== EncodePacket/DecodePacket round-trip ====================

// 验证 EncodePacket 改为 headerBuf 后,无 Encoder 路径的编解码一致性
func TestEncodePacketRoundTrip_NoEncoder(t *testing.T) {
	codec := NewProtoCodec(nil)
	cmd := PacketCommand(pb.CmdTest_Cmd_TestMessage)
	codec.Register(cmd, new(pb.TestMessage))

	orig := &pb.TestMessage{Name: "hello", I32: 42}
	pkt := NewProtoPacket(cmd, orig)

	encoded, headerFlags := codec.EncodePacket(nil, pkt)
	if len(encoded) < 2 {
		t.Fatalf("encoded segments too few: %d", len(encoded))
	}

	// 合并所有段
	var merged []byte
	for _, seg := range encoded {
		merged = append(merged, seg...)
	}

	// 用 headerFlags 构造 packetHeader
	header := NewDefaultPacketHeader(uint32(len(merged)), headerFlags)
	decoded := codec.DecodePacket(nil, header, merged)
	if decoded == nil {
		t.Fatal("DecodePacket returned nil")
	}
	decodedMsg, ok := decoded.Message().(*pb.TestMessage)
	if !ok {
		t.Fatalf("decoded message type error: %T", decoded.Message())
	}
	if decodedMsg.Name != "hello" || decodedMsg.I32 != 42 {
		t.Fatalf("decoded message mismatch: %+v", decodedMsg)
	}
}

// 验证有 ProtoPacketBytesEncoder(XOR)的编解码一致性
func TestEncodePacketRoundTrip_WithEncoder(t *testing.T) {
	xorCodec := NewXorProtoCodec([]byte("testkey"), nil)
	cmd := PacketCommand(pb.CmdTest_Cmd_TestMessage)
	xorCodec.Register(cmd, new(pb.TestMessage))

	orig := &pb.TestMessage{Name: "encoder test", I32: 999}
	pkt := NewProtoPacket(cmd, orig)

	encoded, _ := xorCodec.EncodePacket(nil, pkt)
	var merged []byte
	for _, seg := range encoded {
		merged = append(merged, seg...)
	}

	header := NewDefaultPacketHeader(uint32(len(merged)), 0)
	decoded := xorCodec.DecodePacket(nil, header, merged)
	if decoded == nil {
		t.Fatal("DecodePacket returned nil")
	}
	decodedMsg, ok := decoded.Message().(*pb.TestMessage)
	if !ok {
		t.Fatalf("decoded message type error: %T", decoded.Message())
	}
	if decodedMsg.Name != "encoder test" || decodedMsg.I32 != 999 {
		t.Fatalf("decoded message mismatch: %+v", decodedMsg)
	}
}

// 验证 RPC packet(command + rpcCallId)的编解码一致性
func TestEncodePacketRoundTrip_RpcCall(t *testing.T) {
	codec := NewProtoCodec(nil)
	cmd := PacketCommand(100)
	codec.Register(cmd, new(pb.TestMessage))

	pkt := NewProtoPacket(cmd, &pb.TestMessage{Name: "rpc"})
	pkt.SetRpcCallId(12345)

	encoded, headerFlags := codec.EncodePacket(nil, pkt)
	if headerFlags&RpcCall == 0 {
		t.Fatal("headerFlags should contain RpcCall flag")
	}

	var merged []byte
	for _, seg := range encoded {
		merged = append(merged, seg...)
	}
	header := NewDefaultPacketHeader(uint32(len(merged)), headerFlags)
	decoded := codec.DecodePacket(nil, header, merged)
	if decoded == nil {
		t.Fatal("DecodePacket returned nil")
	}
	if decoded.RpcCallId() != 12345 {
		t.Fatalf("rpcCallId mismatch: got %d, want 12345", decoded.RpcCallId())
	}
}

// 验证 ErrorCode packet 的编解码一致性
func TestEncodePacketRoundTrip_ErrorCode(t *testing.T) {
	codec := NewProtoCodec(nil)
	cmd := PacketCommand(101)
	codec.Register(cmd, new(pb.TestMessage))

	pkt := NewProtoPacket(cmd, &pb.TestMessage{Name: "err"})
	pkt.SetErrorCode(404)

	encoded, headerFlags := codec.EncodePacket(nil, pkt)
	if headerFlags&ErrorCode == 0 {
		t.Fatal("headerFlags should contain ErrorCode flag")
	}

	var merged []byte
	for _, seg := range encoded {
		merged = append(merged, seg...)
	}
	header := NewDefaultPacketHeader(uint32(len(merged)), headerFlags)
	decoded := codec.DecodePacket(nil, header, merged)
	if decoded == nil {
		t.Fatal("DecodePacket returned nil")
	}
	if decoded.ErrorCode() != 404 {
		t.Fatalf("errorCode mismatch: got %d, want 404", decoded.ErrorCode())
	}
}

// 验证 EncodePacket 对非 ProtoPacket(DataPacket)不 panic
func TestEncodePacket_NonProtoPacket(t *testing.T) {
	codec := NewProtoCodec(nil)
	pkt := NewDataPacket([]byte("raw data"))

	// 不应 panic,rpcCallId 应为 0
	encoded, flags := codec.EncodePacket(nil, pkt)
	if flags&RpcCall != 0 {
		t.Fatal("DataPacket should not have RpcCall flag")
	}
	if len(encoded) < 2 {
		t.Fatalf("encoded segments too few: %d", len(encoded))
	}
}

// ==================== SimpleProtoCodec Encode/Decode round-trip ====================

func TestSimpleProtoCodecRoundTrip(t *testing.T) {
	codec := NewSimpleProtoCodec()
	cmd := PacketCommand(200)
	codec.Register(cmd, new(pb.TestMessage))

	orig := &pb.TestMessage{Name: "simple", I32: 7}
	pkt := NewProtoPacket(cmd, orig)

	encoded := codec.Encode(nil, pkt)
	if encoded == nil {
		t.Fatal("Encode returned nil")
	}

	// 构造完整包(含 SimplePacketHeader)
	header := NewSimplePacketHeader(uint32(len(encoded)), 0, cmd)
	fullData := make([]byte, SimplePacketHeaderSize+len(encoded))
	header.WriteTo(fullData)
	copy(fullData[SimplePacketHeaderSize:], encoded)

	decoded, err := codec.Decode(nil, fullData)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if decoded == nil {
		t.Fatal("Decode returned nil")
	}
	decodedMsg, ok := decoded.Message().(*pb.TestMessage)
	if !ok {
		t.Fatalf("decoded type error: %T", decoded.Message())
	}
	if decodedMsg.Name != "simple" || decodedMsg.I32 != 7 {
		t.Fatalf("decoded mismatch: %+v", decodedMsg)
	}
}

// ==================== handler frozen + Register ====================

func TestHandlerFrozen(t *testing.T) {
	h := NewDefaultConnectionHandler(nil)
	h.Register(PacketCommand(1), func(conn Connection, pkt Packet) {}, new(pb.TestMessage))

	// 冻结前注册成功
	if h.GetPacketHandler(PacketCommand(1)) == nil {
		t.Fatal("handler for cmd 1 should be registered")
	}

	// 冻结
	atomic.StoreInt32(&h.frozen, 1)

	// 冻结后注册应静默无效
	h.Register(PacketCommand(2), func(conn Connection, pkt Packet) {}, new(pb.TestMessage))
	if h.GetPacketHandler(PacketCommand(2)) != nil {
		t.Fatal("handler for cmd 2 should not be registered after frozen")
	}
}

func TestHandlerRegisterCreator(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cmd := PacketCommand(300)

	called := false
	h.RegisterCreator(cmd, func(conn Connection, pkt Packet) {
		called = true
	}, func() proto.Message {
		return new(pb.TestMessage)
	})

	// 验证 codec 中也注册了 creator
	if codec.MessageCreatorMap[cmd] == nil {
		t.Fatal("creator not registered in codec")
	}

	// 模拟收包
	pkt := NewProtoPacket(cmd, &pb.TestMessage{Name: "test"})
	h.OnRecvPacket(nil, pkt)
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestRegisterHandlerGeneric(t *testing.T) {
	codec := NewProtoCodec(nil)
	h := NewDefaultConnectionHandler(codec)
	cmd := PacketCommand(301)

	called := false
	RegisterHandler[pb.TestMessage](h, cmd, func(conn Connection, pkt Packet) {
		called = true
	})

	// 验证 codec 中有 creator
	creator := codec.MessageCreatorMap[cmd]
	if creator == nil {
		t.Fatal("creator not registered")
	}
	msg := creator()
	if _, ok := msg.(*pb.TestMessage); !ok {
		t.Fatalf("creator returned wrong type: %T", msg)
	}

	// 模拟收包
	h.OnRecvPacket(nil, NewProtoPacket(cmd, &pb.TestMessage{Name: "generic"}))
	if !called {
		t.Fatal("generic handler was not called")
	}
}

// ==================== SendOption value types ====================

func TestSendOptionValueTypes(t *testing.T) {
	// 验证所有 option 构造函数返回有效的 SendOption
	opts := []SendOption{
		Timeout(time.Second),
		WithDiscard(),
		WithInfiniteTimeout(),
		WithBlock(),
	}

	so := sendOptions{timeout: DefaultRpcTimeout}
	for _, opt := range opts {
		opt.apply(&so)
	}

	// WithInfiniteTimeout 最后应用,timeout 应为 MaxInt64
	if so.timeout != time.Duration(int64(1<<63-1)) {
		// math.MaxInt64
		t.Fatalf("timeout should be MaxInt64, got %v", so.timeout)
	}

	// 单独测试 WithDiscard
	so2 := sendOptions{timeout: DefaultRpcTimeout}
	WithDiscard().apply(&so2)
	if !so2.discard {
		t.Fatal("discard should be true")
	}

	// 单独测试 Timeout
	so3 := sendOptions{timeout: DefaultRpcTimeout}
	Timeout(5 * time.Second).apply(&so3)
	if so3.timeout != 5*time.Second {
		t.Fatalf("timeout should be 5s, got %v", so3.timeout)
	}
}

// ==================== rpcCalls putReply/removeReply ====================

func TestRpcCallsPutReply(t *testing.T) {
	calls := newRpcCalls()
	call := calls.newRpcCall()

	// 非 RPC 包(rpcCallId == 0)不应拦截
	normalPkt := NewProtoPacket(1, nil)
	if calls.putReply(normalPkt) {
		t.Fatal("putReply should return false for non-rpc packet")
	}

	// RPC 回复包应被拦截
	replyPkt := NewProtoPacket(1, &pb.TestMessage{Name: "reply"})
	replyPkt.SetRpcCallId(call.id)
	if !calls.putReply(replyPkt) {
		t.Fatal("putReply should return true for rpc reply")
	}

	// 验证 reply channel 收到包
	select {
	case reply := <-call.reply:
		if reply == nil {
			t.Fatal("reply is nil")
		}
		msg := reply.Message().(*pb.TestMessage)
		if msg.Name != "reply" {
			t.Fatalf("reply name mismatch: %s", msg.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reply")
	}
}

func TestRpcCallsRemoveReply(t *testing.T) {
	calls := newRpcCalls()
	call := calls.newRpcCall()

	calls.removeReply(call.id)

	// 移除后 putReply 应返回 false
	replyPkt := NewProtoPacket(1, nil)
	replyPkt.SetRpcCallId(call.id)
	if calls.putReply(replyPkt) {
		t.Fatal("putReply should return false after removeReply")
	}
}

// ==================== safeCall ====================

func TestSafeCall_NoPanic(t *testing.T) {
	called := false
	safeCall(func() {
		called = true
	})
	if !called {
		t.Fatal("function was not called")
	}
}

func TestSafeCall_WithPanic(t *testing.T) {
	// panic 应被 recover,不应影响后续执行
	safeCall(func() {
		panic("test panic")
	})

	afterPanic := false
	safeCall(func() {
		afterPanic = true
	})
	if !afterPanic {
		t.Fatal("safeCall after panic should still execute")
	}
}

// ==================== Packet Clone ====================

func TestProtoPacketClone(t *testing.T) {
	orig := NewProtoPacket(PacketCommand(10), &pb.TestMessage{Name: "clone", I32: 5})
	orig.SetRpcCallId(99)
	orig.SetErrorCode(7)

	clone := orig.Clone().(*ProtoPacket)
	if clone.command != orig.command {
		t.Fatal("command mismatch")
	}
	if clone.rpcCallId != orig.rpcCallId {
		t.Fatal("rpcCallId mismatch")
	}
	if clone.errorCode != orig.errorCode {
		t.Fatal("errorCode mismatch")
	}
	cloneMsg := clone.Message().(*pb.TestMessage)
	if cloneMsg.Name != "clone" || cloneMsg.I32 != 5 {
		t.Fatalf("message mismatch: %+v", cloneMsg)
	}

	// 修改 clone 不应影响 orig
	cloneMsg.Name = "modified"
	origMsg := orig.Message().(*pb.TestMessage)
	if origMsg.Name != "clone" {
		t.Fatal("clone modification affected original")
	}
}

func TestDataPacketClone(t *testing.T) {
	orig := NewDataPacket([]byte("hello"))
	clone := orig.Clone().(*DataPacket)

	if string(clone.data) != "hello" {
		t.Fatal("clone data mismatch")
	}
	// 修改 clone 不应影响 orig
	clone.data[0] = 'x'
	if orig.data[0] != 'h' {
		t.Fatal("clone modification affected original")
	}
}

// ==================== Concurrent map access for rpcCalls ====================

func TestRpcCallsConcurrent(t *testing.T) {
	calls := newRpcCalls()
	var wg sync.WaitGroup

	// 并发创建和移除 rpcCall
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call := calls.newRpcCall()
			calls.removeReply(call.id)
		}()
	}

	// 并发 putReply
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pkt := NewProtoPacket(1, nil)
			pkt.SetRpcCallId(uint32(n + 1))
			calls.putReply(pkt) // 可能返回 false(call不存在),不应 panic
		}(i)
	}

	wg.Wait()
}

// ==================== EncodePacket headerBuf correctness ====================

// 验证 headerBuf 的 command 字节在小端序下正确
func TestEncodePacketHeaderBufCommandBytes(t *testing.T) {
	codec := NewProtoCodec(nil)
	cmd := PacketCommand(0x0102) // 便于验证小端序
	pkt := NewProtoPacket(cmd, nil)

	encoded, _ := codec.EncodePacket(nil, pkt)
	// 第一段是 headerData(command)
	cmdBytes := encoded[0]
	if len(cmdBytes) < 2 {
		t.Fatalf("headerData too short: %d", len(cmdBytes))
	}
	// 小端序: 0x02 在低字节, 0x01 在高字节
	if cmdBytes[0] != 0x02 || cmdBytes[1] != 0x01 {
		t.Fatalf("command bytes mismatch: %v", cmdBytes[:2])
	}
}

// ==================== DefaultPacketHeader ====================

func TestDefaultPacketHeaderFlags(t *testing.T) {
	h := NewDefaultPacketHeader(0x1234, 0x05)
	if h.Len() != 0x1234 {
		t.Fatalf("Len mismatch: %d", h.Len())
	}
	if h.Flags() != 0x05 {
		t.Fatalf("Flags mismatch: %d", h.Flags())
	}
	if !h.HasFlag(0x01) {
		t.Fatal("HasFlag 0x01 should be true")
	}
	if !h.HasFlag(0x04) {
		t.Fatal("HasFlag 0x04 should be true")
	}
	if h.HasFlag(0x02) {
		t.Fatal("HasFlag 0x02 should be false")
	}
}

// ==================== SimplePacketHeader ====================

func TestSimplePacketHeaderRoundTrip(t *testing.T) {
	orig := NewSimplePacketHeader(0x1234, 0x07, PacketCommand(0x0102))
	buf := make([]byte, SimplePacketHeaderSize)
	orig.WriteTo(buf)

	decoded := &SimplePacketHeader{}
	decoded.ReadFrom(buf)

	if decoded.Len() != 0x1234 {
		t.Fatalf("Len mismatch: %d", decoded.Len())
	}
	if decoded.Flags() != 0x07 {
		t.Fatalf("Flags mismatch: %d", decoded.Flags())
	}
	if decoded.Command != 0x0102 {
		t.Fatalf("Command mismatch: %d", decoded.Command)
	}
}

// ==================== XorProtoCodec empty key ====================

func TestXorProtoCodecEmptyKey(t *testing.T) {
	// 空 key 应返回 nil,防止后续除零
	codec := NewXorProtoCodec([]byte{}, nil)
	if codec != nil {
		t.Fatal("NewXorProtoCodec with empty key should return nil")
	}
}

// ==================== xorEncode correctness ====================

func TestXorEncodeRoundTrip(t *testing.T) {
	key := []byte("abc")
	data := []byte("hello world test data")
	original := make([]byte, len(data))
	copy(original, data)

	xorEncode(data, key, 0)
	xorEncode(data, key, 0) // 二次 XOR 应恢复原始数据

	for i := range data {
		if data[i] != original[i] {
			t.Fatalf("xor double encode failed at byte %d", i)
		}
	}
}

// ==================== ConnectionConfig InsecureSkipVerify default ====================

func TestDefaultConnectionConfigInsecureSkipVerify(t *testing.T) {
	if !DefaultConnectionConfig.InsecureSkipVerify {
		t.Fatal("DefaultConnectionConfig.InsecureSkipVerify should default to true")
	}
}
