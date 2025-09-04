package example

import (
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
	"testing"
	"unsafe"
)

func TestPacketEx(t *testing.T) {
	packet := gnet.NewProtoPacketEx(pb.CmdTest_Cmd_TestMessage, &pb.TestMessage{
		Name: "test",
	})
	t.Log(fmt.Sprintf("%v", packet))

	packetHeaderSize := unsafe.Sizeof(gnet.DefaultPacketHeader{})
	t.Log(fmt.Sprintf("DefaultPacketHeaderSize:%v", packetHeaderSize))

	packetHeaderSize = unsafe.Sizeof(gnet.SimplePacketHeader{})
	t.Log(fmt.Sprintf("SimplePacketHeaderSize:%v", packetHeaderSize))
}
