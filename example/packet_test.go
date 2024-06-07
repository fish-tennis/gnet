package example

import (
	"fmt"
	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
	"testing"
)

func TestPacketEx(t *testing.T) {
	packet := gnet.NewProtoPacketEx(pb.CmdTest_Cmd_TestMessage, &pb.TestMessage{
		Name: "test",
	})
	t.Log(fmt.Sprintf("%v", packet))
}
