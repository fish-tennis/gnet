package gnet

import (
	"encoding/binary"
	"google.golang.org/protobuf/proto"
)

type ProtoMessageCreator func() proto.Message

type ProtoCodec struct {
	RingBufferCodec
	messageCreatorMap map[PacketCommand]ProtoMessageCreator
}

func NewProtoCodec(messageCreatorMap map[PacketCommand]ProtoMessageCreator) *ProtoCodec {
	codec := &ProtoCodec{
		RingBufferCodec:RingBufferCodec{},
		messageCreatorMap: messageCreatorMap,
	}
	codec.DataEncoder = codec.EncodePacket
	codec.DataDecoder = codec.DecodePacket
	return codec
}

func (this *ProtoCodec) EncodePacket(connection Connection, packet Packet) [][]byte {
	if protoPacket,ok := packet.(*ProtoPacket); ok {
		protoMessage := protoPacket.Message()
		// 先写入消息号
		commandBytes := make([]byte,2)
		binary.LittleEndian.PutUint16(commandBytes, uint16(protoPacket.Command()))
		messageBytes,err := proto.Marshal(protoMessage)
		if err != nil {
			return nil
		}
		return [][]byte{commandBytes,messageBytes}
		//fullData := make([]byte, len(commandBytes)+len(messageBytes))
		//n := copy(fullData, commandBytes)
		//copy(fullData[n:], messageBytes)
		//return fullData

		//protoBuffer := proto.NewBuffer(nil)
		//protoBuffer.EncodeRawBytes(commandBytes)
		//// 再写入proto消息
		//err := protoBuffer.Marshal(protoMessage)
		//if err != nil {
		//	return nil
		//}
		//return protoBuffer.Bytes()
	}
	return nil
}

func (this *ProtoCodec) DecodePacket(connection Connection, packetHeader *PacketHeader, packetData []byte) Packet {
	command := binary.LittleEndian.Uint16(packetData[:2])
	if messageCreator,ok := this.messageCreatorMap[PacketCommand(command)]; ok {
		newProtoMessage := messageCreator()
		err := proto.Unmarshal(packetData[2:], newProtoMessage)
		if err != nil {
			return nil
		}
		return &ProtoPacket{
			command: PacketCommand(command),
			message: newProtoMessage,
		}
	}
	LogError("unsupport command:%v", command)
	return &ProtoPacket{
		command: PacketCommand(command),
	}
}
