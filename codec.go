package gnet

// 连接的编解码接口
type Codec interface {
	// 编码接口
	Encode(connection Connection, packet *Packet) []byte

	// 解码接口
	Decode(connection Connection, data []byte) *Packet
}

// 用了RingBuffer的连接的编解码接口
// 流格式: Length+Data
// 这里把编解码分成了2层
// 第1层:从RingBuffer里取出原始包体数据
// 第2层:对包数据执行实际的编解码操作
type RingBufferCodec struct {
	// 包头的编码接口,包头长度不能变
	headerEncoder func(headerData []byte)
	// 包体的编码接口
	dataEncoder func(packetData []byte) []byte
	// 包头的解码接口,包头长度不能变
	headerDecoder func(headerData []byte)
	// 包体的解码接口
	dataDecoder func(packetData []byte) []byte
}

func (this *RingBufferCodec) Encode(_ Connection, packet *Packet) []byte  {
	// TODO:优化思路:直接写入sendBuffer,可以减少copy
	src := packet.data
	if this.dataEncoder != nil {
		src = this.dataEncoder(packet.data)
	}
	dst := make([]byte, PacketHeaderSize + len(src))
	packetHeader := NewPacketHeader(uint32(len(src)), 0)
	packetHeader.WriteTo(dst)
	if this.headerEncoder != nil {
		this.headerEncoder(dst[0:PacketHeaderSize])
	}
	copy(dst[PacketHeaderSize:],src)
	return dst
}

func (this *RingBufferCodec) Decode(connection Connection, data []byte) *Packet {
	if tcpConnection,ok := connection.(*TcpConnection); ok {
		// TcpConnection用了RingBuffer,解码时,尽可能的不产生copy
		if tcpConnection.recvBuffer.UnReadLength() < PacketHeaderSize {
			return nil
		}
		var packetHeaderData []byte
		readBuffer := tcpConnection.recvBuffer.ReadBuffer()
		if len(readBuffer) >= PacketHeaderSize {
			if this.headerDecoder != nil {
				// 如果header需要解码,那么这里就必须copy了,因为这时还不能确定收到完整的包,所以不能对原始数据进行修改
				packetHeaderData = make([]byte, PacketHeaderSize)
				copy(packetHeaderData, readBuffer)
			} else {
				// 这里不产生copy
				packetHeaderData = readBuffer[0:PacketHeaderSize]
			}
		} else {
			// TODO: 优化思路: packetHeaderData用sync.pool创建
			packetHeaderData = make([]byte, PacketHeaderSize)
			// 先拷贝RingBuffer的尾部
			n := copy(packetHeaderData, readBuffer)
			// 再拷贝RingBuffer的头部
			copy(packetHeaderData[n:], tcpConnection.recvBuffer.buffer)
		}
		if this.headerDecoder != nil {
			this.headerDecoder(packetHeaderData)
		}
		header := &PacketHeader{}
		header.ReadFrom(packetHeaderData)
		// TODO: 优化思路: 从sync.pool创建的packetHeaderData回收到sync.pool
		//packetHeaderData = nil
		if tcpConnection.recvBuffer.UnReadLength() < PacketHeaderSize + int(header.GetLen()) {
			return nil
		}
		tcpConnection.recvBuffer.SetReaded(PacketHeaderSize)
		readBuffer = tcpConnection.recvBuffer.ReadBuffer()
		var packetData []byte
		if len(readBuffer) >= int(header.GetLen()) {
			// 这里不产生copy
			packetData = readBuffer[0:header.GetLen()]
		} else {
			// TODO: 优化思路,用sync.pool创建,Q:何时回收?
			packetData = make([]byte, header.GetLen())
			// 先拷贝RingBuffer的尾部
			n := copy(packetData, readBuffer)
			// 再拷贝RingBuffer的头部
			copy(packetData[n:], tcpConnection.recvBuffer.buffer)
		}
		tcpConnection.recvBuffer.SetReaded(int(header.GetLen()))
		if this.dataDecoder != nil {
			// 包体的解码接口
			packetData = this.dataDecoder(packetData)
		}
		return NewPacket(packetData)
	}
	//if len(data) >= PacketHeaderSize {
	//	header := &PacketHeader{}
	//	header.ReadFrom(data)
	//	if len(data) >= PacketHeaderSize+int(header.GetLen()) {
	//		return NewPacket(data[PacketHeaderSize:PacketHeaderSize+int(header.GetLen())])
	//	}
	//}
	return nil
}


// 默认编解码,只做长度和数据的解析
type DefaultCodec struct {
	RingBufferCodec
}

func NewDefaultCodec() *DefaultCodec {
	return &DefaultCodec{}
}

// 异或编解码
type XorCodec struct {
	RingBufferCodec
	key []byte
}

func NewXorCodec(key []byte) *XorCodec {
	return &XorCodec{
		key: key,
		RingBufferCodec:RingBufferCodec{
			headerEncoder: func(headerData []byte) {
				xorEncode(headerData, key)
			},
			dataEncoder: func(packetData []byte) []byte {
				xorEncode(packetData, key)
				return packetData
			},
			headerDecoder: func(headerData []byte) {
				xorEncode(headerData, key)
			},
			dataDecoder: func(packetData []byte) []byte {
				xorEncode(packetData, key)
				return packetData
			},
		},
	}
}

// 异或
func xorEncode(data []byte, key []byte) {
	for i := 0; i < len(data); i++ {
		data[i] = data[i] ^ key[i%len(key)]
	}
}
