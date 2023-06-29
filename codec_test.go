package gnet

import (
	"fmt"
	"github.com/fish-tennis/gnet/example/pb"
	"testing"
)

func TestXorCodec(t *testing.T) {
	SetLogger(GetLogger(), DebugLevel)
	testProtoPacket := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		&pb.TestMessage{
			Name: "test packet",
			I32:  123,
		})
	xorCodec := NewXorProtoCodec([]byte("test"), nil)
	encodedBytes := xorCodec.EncodePacket(nil, testProtoPacket)
	commandBytes, messageBytes := encodedBytes[0], encodedBytes[1]
	packetBytes := make([]byte, len(commandBytes)+len(messageBytes))
	copy(packetBytes, commandBytes)
	copy(packetBytes[len(commandBytes):], messageBytes)
	packetBytes2 := make([]byte, len(packetBytes))
	copy(packetBytes2, packetBytes)
	decodePacket := xorCodec.DecodePacket(nil, nil, packetBytes)
	if decodePacket == nil {
		logger.Warn("decodePacket nil")
	}

	xorCodec.Register(PacketCommand(pb.CmdTest_Cmd_TestMessage), new(pb.TestMessage))
	decodePacket2 := xorCodec.DecodePacket(nil, nil, packetBytes2)
	t.Log(fmt.Sprintf("%v %v", decodePacket2.Command(), decodePacket2.Message().(*pb.TestMessage)))
}

func TestPacket(t *testing.T) {
	testDataPacket := NewDataPacket([]byte("test data packet"))
	t.Log(testDataPacket.Command())
	t.Log(testDataPacket.Message())
	t.Logf("%v", testDataPacket.Clone())

	testProtoPacket := NewProtoPacket(PacketCommand(pb.CmdTest_Cmd_TestMessage),
		&pb.TestMessage{
			Name: "test packet",
			I32:  123,
		})
	t.Logf("%v", testProtoPacket.Clone())

	header := NewDefaultPacketHeader(123, 0x3)
	t.Log(header.Len())
	t.Log(header.Flags())
}
