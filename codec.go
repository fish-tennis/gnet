package gnet

// 编解码接口
// 由于读取流数据时,先解码出消息头,再根据消息头的内容来解包体,所以包头和包体的解码分开提供接口
// 但是编码没有这种需求,所以只提供一个编码接口就可以了
type Codec interface {
	// 包头解码接口,返回的数组大小必须是MessageHeaderSize
	// 返回解码后的数据,可能地址和src相同
	DecodeHeader(header []byte) []byte

	// 包体解码接口
	// 返回解码后的数据,可能地址和src相同
	DecodeData(data []byte) []byte

	// 编码接口
	// 返回编码后的数据,可能地址和src相同
	Encode(src []byte) []byte
}

// 不进行编解码
type NoneCodec struct {
}

func (this NoneCodec) DecodeHeader(header []byte) []byte  {
	return header
}

func (this NoneCodec) DecodeData(data []byte) []byte  {
	return data
}

func (this NoneCodec) Encode(src []byte) []byte  {
	return src
}

// 异或编解码
type XorCodec struct {
	key []byte
}

func NewXorCodec(key []byte) *XorCodec {
	return &XorCodec{key: key}
}

func (this XorCodec) DecodeHeader(header []byte) []byte {
	for i := 0; i < PacketHeaderSize; i++ {
		header[i] = header[i] ^ this.key[i%len(this.key)]
	}
	return header
}

func (this XorCodec) DecodeData(data []byte) []byte {
	for i := 0; i < len(data); i++ {
		data[i] = data[i] ^ this.key[(i+PacketHeaderSize)%len(this.key)]
	}
	return data
}

func (this XorCodec) Encode(src []byte) []byte {
	dst := make([]byte, len(src)+PacketHeaderSize)
	packetHeader := NewPacketHeader(uint32(len(src)), 0)
	packetHeader.WriteTo(dst)
	for i := 0; i < PacketHeaderSize; i++ {
		dst[i] = dst[i] ^ this.key[i%len(this.key)]
	}
	for i := 0; i < len(src); i++ {
		dst[i+PacketHeaderSize] = src[i] ^ this.key[(i+PacketHeaderSize)%len(this.key)]
	}
	return dst
}
