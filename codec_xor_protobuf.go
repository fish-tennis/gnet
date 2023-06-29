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
	codec.ProtoPacketBytesEncoder = func(protoPacketBytes [][]byte) [][]byte {
		keyIndex := 0
		for _, data := range protoPacketBytes {
			for i := 0; i < len(data); i++ {
				data[i] = data[i] ^ codec.xorKey[keyIndex%len(codec.xorKey)]
				keyIndex++
			}
		}
		return protoPacketBytes
	}
	codec.ProtoPacketBytesDecoder = func(packetData []byte) []byte {
		xorEncode(packetData, codec.xorKey)
		return packetData
	}
	return codec
}

// xor encode
func xorEncode(data []byte, key []byte) {
	for i := 0; i < len(data); i++ {
		data[i] = data[i] ^ key[i%len(key)]
	}
}
