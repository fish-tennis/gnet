package gnet

// 编解码接口
type Codec interface {
	// 编码接口
	Encode(connection Connection, packet *Packet) []byte

	// 解码接口
	Decode(connection Connection, data []byte) *Packet
}

// 默认编解码
type DefaultCodec struct {
}

func (this *DefaultCodec) Encode(_ Connection, packet *Packet) []byte  {
	src := packet.data
	dst := make([]byte, len(src)+PacketHeaderSize)
	packetHeader := NewPacketHeader(uint32(len(src)), 0)
	packetHeader.WriteTo(dst)
	copy(dst[PacketHeaderSize:],src)
	return dst
}

func (this *DefaultCodec) Decode(connection Connection, data []byte) *Packet {
	if tcpConnection,ok := connection.(*TcpConnection); ok {
		// TcpConnection用了RingBuffer,解码时,尽可能的不产生copy
		if tcpConnection.recvBuffer.UnReadLength() < PacketHeaderSize {
			return nil
		}
		var packetHeaderData []byte
		readBuffer := tcpConnection.recvBuffer.ReadBuffer()
		if len(readBuffer) >= PacketHeaderSize {
			// 这里不产生copy
			packetHeaderData = readBuffer[0:PacketHeaderSize]
		} else {
			packetHeaderData = make([]byte, PacketHeaderSize)
			// 先拷贝RingBuffer的尾部
			n := copy(packetHeaderData, readBuffer)
			// 再拷贝RingBuffer的头部
			copy(packetHeaderData[n:], tcpConnection.recvBuffer.buffer)
		}
		header := &PacketHeader{}
		header.ReadFrom(packetHeaderData)
		if tcpConnection.recvBuffer.UnReadLength() < PacketHeaderSize + int(header.GetLen()) {
			return nil
		}
		tcpConnection.recvBuffer.SetReaded(PacketHeaderSize)
		readBuffer = tcpConnection.recvBuffer.ReadBuffer()
		if len(readBuffer) >= int(header.GetLen()) {
			// 这里不产生copy
			packetData := readBuffer[0:header.GetLen()]
			tcpConnection.recvBuffer.SetReaded(int(header.GetLen()))
			return NewPacket(packetData)
		} else {
			packetData := make([]byte, header.GetLen())
			// 先拷贝RingBuffer的尾部
			n := copy(packetData, readBuffer)
			// 再拷贝RingBuffer的头部
			copy(packetData[n:], tcpConnection.recvBuffer.buffer)
			tcpConnection.recvBuffer.SetReaded(int(header.GetLen()))
			return NewPacket(packetData)
		}
	}
	if len(data) >= PacketHeaderSize {
		header := &PacketHeader{}
		header.ReadFrom(data)
		if len(data) >= PacketHeaderSize+int(header.GetLen()) {
			return NewPacket(data[PacketHeaderSize:PacketHeaderSize+int(header.GetLen())])
		}
	}
	return nil
}

// 异或编解码
type XorCodec struct {
	key []byte
}

func NewXorCodec(key []byte) *XorCodec {
	return &XorCodec{key: key}
}

func (this *XorCodec) Encode(_ Connection, packet *Packet) []byte {
	src := packet.data
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

func (this *XorCodec) Decode(connection Connection, data []byte) *Packet {
	if tcpConnection,ok := connection.(*TcpConnection); ok {
		// TcpConnection用了RingBuffer,解码时,尽可能的不产生copy
		if tcpConnection.recvBuffer.UnReadLength() < PacketHeaderSize {
			return nil
		}
		packetHeaderData := make([]byte, PacketHeaderSize)
		readBuffer := tcpConnection.recvBuffer.ReadBuffer()
		if len(readBuffer) >= PacketHeaderSize {
			copy(packetHeaderData, readBuffer)
		} else {
			// 先拷贝RingBuffer的尾部
			n := copy(packetHeaderData, readBuffer)
			// 再拷贝RingBuffer的头部
			copy(packetHeaderData[n:], tcpConnection.recvBuffer.buffer)
		}
		for i := 0; i < PacketHeaderSize; i++ {
			packetHeaderData[i] = packetHeaderData[i] ^ this.key[i%len(this.key)]
		}
		header := &PacketHeader{}
		header.ReadFrom(packetHeaderData)
		if tcpConnection.recvBuffer.UnReadLength() < PacketHeaderSize + int(header.GetLen()) {
			return nil
		}
		tcpConnection.recvBuffer.SetReaded(PacketHeaderSize)
		readBuffer = tcpConnection.recvBuffer.ReadBuffer()
		if len(readBuffer) >= int(header.GetLen()) {
			// 这里不产生copy
			packetData := readBuffer[0:header.GetLen()]
			tcpConnection.recvBuffer.SetReaded(int(header.GetLen()))
			for i := 0; i < len(packetData); i++ {
				packetData[i] = packetData[i] ^ this.key[(i+PacketHeaderSize)%len(this.key)]
			}
			return NewPacket(packetData)
		} else {
			packetData := make([]byte, header.GetLen())
			// 先拷贝RingBuffer的尾部
			n := copy(packetData, readBuffer)
			// 再拷贝RingBuffer的头部
			copy(packetData[n:], tcpConnection.recvBuffer.buffer)
			tcpConnection.recvBuffer.SetReaded(int(header.GetLen()))
			for i := 0; i < len(packetData); i++ {
				packetData[i] = packetData[i] ^ this.key[(i+PacketHeaderSize)%len(this.key)]
			}
			return NewPacket(packetData)
		}
	}
	if len(data) >= PacketHeaderSize {
		packetHeaderData := make([]byte, PacketHeaderSize)
		for i := 0; i < PacketHeaderSize; i++ {
			packetHeaderData[i] = data[i] ^ this.key[i%len(this.key)]
		}
		header := &PacketHeader{}
		header.ReadFrom(packetHeaderData)
		if len(data) >= PacketHeaderSize+int(header.GetLen()) {
			packetData := data[PacketHeaderSize:PacketHeaderSize+int(header.GetLen())]
			for i := 0; i < len(packetData); i++ {
				packetData[i] = packetData[i] ^ this.key[(i+PacketHeaderSize)%len(this.key)]
			}
			return NewPacket(packetData)
		}
	}
	return nil
}
