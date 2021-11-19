package gnet

// proto+异或
type XorProtoCodec struct {
	*ProtoCodec
	xorKey []byte
}

func NewXorProtoCodec(xorKey []byte, messageCreatorMap map[PacketCommand]ProtoMessageCreator) *XorProtoCodec {
	codec := &XorProtoCodec{
		ProtoCodec:NewProtoCodec(messageCreatorMap),
		xorKey: xorKey,
	}
	codec.ProtoPacketBytesEncoder = func(protoPacketBytes [][]byte) [][]byte {
		keyIndex := 0
		for _,data := range protoPacketBytes {
			for i := 0; i < len(data); i++ {
				data[i] = data[i] ^ codec.xorKey[keyIndex%len(codec.xorKey)]
				keyIndex++
			}
		}
		return protoPacketBytes
	}
	codec.ProtoPacketBytesDecoder = func(packetData []byte) []byte {
		xorEncode(packetData,codec.xorKey)
		return packetData
	}
	return codec
}

// 异或
func xorEncode(data []byte, key []byte) {
	for i := 0; i < len(data); i++ {
		data[i] = data[i] ^ key[i%len(key)]
	}
}
