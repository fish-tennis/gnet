package gnet

type Message struct {
	//len uint32
	//cmd uint16
	//flags uint8
	data []byte
}

type MessageHeader struct {
	// (flags << 24) | len
	lenAndFlags uint32
}

// [0,0x00FFFFFF]
func (this *MessageHeader) GetLen() uint32 {
	return this.lenAndFlags & 0x00FFFFFF
}

// [0,0xFF]
func (this *MessageHeader) GetFlags() uint32 {
	return this.lenAndFlags >> 24
}

//type ProtoMessage struct {
//	cmd uint16
//	message proto.Message
//}