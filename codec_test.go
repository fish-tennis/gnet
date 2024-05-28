package gnet

import (
	"encoding/binary"
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
	encodedBytes, _ := xorCodec.EncodePacket(nil, testProtoPacket)
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

	simpleHeader := NewSimplePacketHeader(0, 15, 0)
	t.Log(simpleHeader.Flags())
}

func TestLogger(t *testing.T) {
	SetLogger(GetLogger(), DebugLevel)
	for level := DebugLevel; level <= ErrorLevel+1; level++ {
		SetLogLevel(level)
		logger.Debug("debug")
		logger.Info("info")
		logger.Warn("warn")
		logger.Error("error")
	}
	LogStack()
}

func TestCodecError(t *testing.T) {
	errLengthData := []byte{1}
	protoCodec := NewProtoCodec(nil)
	protoCodec.DecodePacket(nil, nil, errLengthData)

	testCommand := uint16(123)
	protoCodec.Register(PacketCommand(testCommand), new(pb.TestMessage))
	errMessageData := make([]byte, 3)
	binary.LittleEndian.PutUint16(errMessageData, testCommand)
	protoCodec.DecodePacket(nil, nil, errMessageData)

	simpleProtoCodec := NewSimpleProtoCodec()
	simpleProtoCodec.Register(PacketCommand(testCommand), new(pb.TestMessage))
	errSimpleMessageData := make([]byte, 7)
	simplePacketHeader := NewSimplePacketHeader(1, 0, PacketCommand(testCommand))
	simplePacketHeader.WriteTo(errSimpleMessageData)
	simpleProtoCodec.Decode(nil, errSimpleMessageData)
}

func TestHandler(t *testing.T) {
	defaultHandler := NewDefaultConnectionHandler(nil)
	defaultHandler.GetCodec()
	defaultHandler.CreateHeartBeatPacket(nil)
	defaultHandler.SetUnRegisterHandler(func(connection Connection, packet Packet) {

	})
	defaultHandler.OnRecvPacket(nil, NewProtoPacket(123, nil))
}
