package gnet

import "google.golang.org/protobuf/proto"

// 消息号
type PacketCommand uint16

//type IProtoPacket interface {
//	Message() proto.Message
//}

// proto数据包
type ProtoPacket struct {
	command PacketCommand
	message proto.Message
}

func NewProtoPacket(command PacketCommand, message proto.Message) *ProtoPacket {
	return &ProtoPacket{
		command: command,
		message: message,
	}
}

func (this *ProtoPacket) Message() proto.Message {
	return this.message
}

func (this *ProtoPacket) Command() PacketCommand {
	return this.command
}

// ProtoPacket没有用这个函数,这里只是为了满足Packet的接口
func (this *ProtoPacket) GetData() []byte {
	return nil
}