package gnet

// 编解码接口
// 由于读取流数据时,先解码出消息头,再根据消息头的内容来解包体,所以包头和包体的解码分开提供接口
// 但是编码没有这种需求,所以只提供一个编码接口就可以了
type Codec interface {
	// 包头解码接口
	// 返回解码后的数据,可能地址和src相同
	DecodeHeader(src []byte) []byte

	// 包体解码接口
	// 返回解码后的数据,可能地址和src相同
	DecodeData(src []byte) []byte

	// 编码接口
	// 返回编码后的数据,可能地址和src相同
	Encode(src []byte) []byte
}

// 不处理的接口
type NoneCodec struct {
}

func (this *NoneCodec) DecodeHeader(src []byte) []byte  {
	return src
}

func (this *NoneCodec) DecodeData(src []byte) []byte  {
	return src
}

func (this *NoneCodec) Encode(src []byte) []byte  {
	return src
}

