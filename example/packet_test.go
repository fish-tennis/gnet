package example

import (
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
	"sync"
	"testing"
)

func TestPacketEx(t *testing.T) {
	packet := gnet.NewProtoPacketEx(pb.CmdTest_Cmd_TestMessage, &pb.TestMessage{
		Name: "test",
	})
	t.Log(fmt.Sprintf("%v", packet))
}

func BenchmarkPacketWithoutPool(b *testing.B) {
	//b.ResetTimer()
	packetNew := func() *gnet.ProtoPacket {
		return new(gnet.ProtoPacket)
	}
	messageNew := func() *pb.TestMessage {
		return new(pb.TestMessage)
	}
	for i := 0; i < b.N; i++ {
		packet := packetNew()
		m := messageNew()
		m.I32 = int32(i)
		m.Name = fmt.Sprintf("str%v", i)
		//m.Name = "name"
		packet.SetArgs(m, gnet.PacketCommand(i))
		//b.Logf("%v %v", packet.Command(), packet.Message())
	}
	//b.StopTimer()
}

var (
	pool = sync.Pool{
		New: func() any {
			//b.Log("new TestMessage")
			return new(pb.TestMessage)
		},
	}
	packetPool = sync.Pool{
		New: func() any {
			//b.Log("new ProtoPacket")
			return new(gnet.ProtoPacket)
		},
	}
)

func BenchmarkPacketPool(b *testing.B) {
	//b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packet := packetPool.Get().(*gnet.ProtoPacket)
		m := pool.Get().(*pb.TestMessage)
		m.I32 = int32(i)
		//m.Name = "name"
		m.Name = fmt.Sprintf("str%v", i)
		packet.SetArgs(m, gnet.PacketCommand(i))
		//b.Logf("%v %v", packet.Command(), packet.Message())
		pool.Put(m)
		packetPool.Put(packet)
	}
	//b.StopTimer()
}
