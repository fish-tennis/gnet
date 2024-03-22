package gnet

import "reflect"

// codec for proto.Message and xor
type XorProtoCodec struct {
	*ProtoCodec
	xorKey []byte
}

func NewXorProtoCodec(xorKey []byte, protoMessageTypeMap map[PacketCommand]reflect.Type) *XorProtoCodec {
	codec := &XorProtoCodec{
		ProtoCodec: NewProtoCodec(protoMessageTypeMap),
		xorKey:     xorKey,
	}
	codec.HeaderEncoder = func(connection Connection, packet Packet, headerData []byte) {
		xorEncode(headerData, codec.xorKey, 0)
	}
	codec.HeaderDecoder = func(connection Connection, headerData []byte) {
		xorEncode(headerData, codec.xorKey, 0)
	}
	codec.ProtoPacketBytesEncoder = func(protoPacketBytes [][]byte) [][]byte {
		keyIndex := int(codec.PacketHeaderSize())
		for _, data := range protoPacketBytes {
			xorEncode(data, codec.xorKey, keyIndex)
			keyIndex += len(data)
		}
		return protoPacketBytes
	}
	codec.ProtoPacketBytesDecoder = func(packetData []byte) []byte {
		xorEncode(packetData, codec.xorKey, int(codec.PacketHeaderSize()))
		return packetData
	}
	return codec
}

// xor encode
func xorEncode(data []byte, key []byte, keyIndex int) {
	for i := 0; i < len(data); i++ {
		data[i] = data[i] ^ key[(i+keyIndex)%len(key)]
	}
}
