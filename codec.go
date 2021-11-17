package gnet

// 连接的编解码接口
type Codec interface {
	// 编码接口
	Encode(connection Connection, packet *Packet) (encodedData []byte, remainData []byte)

	// 解码接口
	Decode(connection Connection, data []byte) (newPacket *Packet, err error)
}

// 用了RingBuffer的连接的编解码接口
// 流格式: Length+Data
// 这里把编解码分成了2层
// 第1层:从RingBuffer里取出原始包体数据
// 第2层:对包数据执行实际的编解码操作
type RingBufferCodec struct {
	// 包头的编码接口,包头长度不能变
	HeaderEncoder func(connection Connection, packet *Packet, headerData []byte)
	// 包体的编码接口
	DataEncoder func(connection Connection, packet *Packet, packetData []byte) []byte
	// 包头的解码接口,包头长度不能变
	HeaderDecoder func(connection Connection, headerData []byte)
	// 包体的解码接口
	DataDecoder func(connection Connection, packetHeader *PacketHeader, packetData []byte) []byte
}

func (this *RingBufferCodec) Encode(connection Connection, packet *Packet) (encodedData []byte, remainData []byte) {
	// 优化思路:编码后的数据直接写入RingBuffer.sendBuffer,可以减少一些内存分配
	if tcpConnection,ok := connection.(*TcpConnection); ok {
		sendBuffer := tcpConnection.sendBuffer
		encodedData = packet.data
		if this.DataEncoder != nil {
			encodedData = this.DataEncoder(connection, packet, packet.data)
		}
		packetHeader := NewPacketHeader(uint32(len(encodedData)), 0)
		writeBuffer := sendBuffer.WriteBuffer()
		if len(writeBuffer) >= PacketHeaderSize {
			// 有足够的连续空间可写,则直接写入RingBuffer里
			// 省掉了一次内存分配操作: make([]byte, PacketHeaderSize)
			packetHeader.WriteTo(writeBuffer)
			if this.HeaderEncoder != nil {
				this.HeaderEncoder(connection, packet, writeBuffer[0:PacketHeaderSize])
			}
			sendBuffer.SetWrited(PacketHeaderSize)
		} else {
			// 没有足够的连续空间可写,则能写多少写多少,有可能一部分写入尾部,一部分写入头部
			packetHeaderData := make([]byte, PacketHeaderSize)
			packetHeader.WriteTo(packetHeaderData)
			if this.HeaderEncoder != nil {
				this.HeaderEncoder(connection, packet, packetHeaderData)
			}
			writedHeaderLen,_ := sendBuffer.Write(packetHeaderData)
			if writedHeaderLen < PacketHeaderSize {
				// 写不下的包头数据和包体数据,返回给TcpConnection延后处理
				// 合理的设置发包缓存,一般不会运行到这里
				remainData = make([]byte, PacketHeaderSize-writedHeaderLen+len(encodedData))
				// 没写完的header数据
				n := copy(remainData, packetHeaderData[writedHeaderLen:])
				// 编码后的包体数据
				copy(remainData[n:], encodedData)
				return
			}
		}
		writeBuffer = sendBuffer.WriteBuffer()
		writetedDataLen,_ := sendBuffer.Write(encodedData)
		if writetedDataLen < len(encodedData) {
			// 写不下的包体数据,返回给TcpConnection延后处理
			remainData = encodedData[writetedDataLen:]
		}
		return
	}

	//// 不优化的方案,每个包都需要进行一次内存分配和拷贝
	//packetData := packet.data
	//if this.DataEncoder != nil {
	//	packetData = this.DataEncoder(connection, packet, packet.data)
	//}
	//encodedData = make([]byte, PacketHeaderSize + len(packetData))
	//packetHeader := NewPacketHeader(uint32(len(packetData)), 0)
	//packetHeader.WriteTo(encodedData)
	//if this.HeaderEncoder != nil {
	//	this.HeaderEncoder(connection, packet, encodedData[0:PacketHeaderSize])
	//}
	//copy(encodedData[PacketHeaderSize:], packetData)
	//remainData = encodedData
	return
}

func (this *RingBufferCodec) Decode(connection Connection, data []byte) (newPacket *Packet, err error) {
	if tcpConnection,ok := connection.(*TcpConnection); ok {
		// TcpConnection用了RingBuffer,解码时,尽可能的不产生copy
		recvBuffer := tcpConnection.recvBuffer
		if recvBuffer.UnReadLength() < PacketHeaderSize {
			return
		}
		var packetHeaderData []byte
		readBuffer := recvBuffer.ReadBuffer()
		if len(readBuffer) >= PacketHeaderSize {
			if this.HeaderDecoder != nil {
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
			copy(packetHeaderData[n:], recvBuffer.buffer)
		}
		if this.HeaderDecoder != nil {
			this.HeaderDecoder(connection, packetHeaderData)
		}
		header := &PacketHeader{}
		header.ReadFrom(packetHeaderData)
		// TODO: 优化思路: 从sync.pool创建的packetHeaderData回收到sync.pool
		//packetHeaderData = nil
		if int(header.GetLen()) > recvBuffer.Size() - PacketHeaderSize {
			return nil, ErrPacketLength
		}
		if recvBuffer.UnReadLength() < PacketHeaderSize + int(header.GetLen()) {
			return
		}
		recvBuffer.SetReaded(PacketHeaderSize)
		readBuffer = recvBuffer.ReadBuffer()
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
			copy(packetData[n:], recvBuffer.buffer)
		}
		recvBuffer.SetReaded(int(header.GetLen()))
		if this.DataDecoder != nil {
			// 包体的解码接口
			packetData = this.DataDecoder(connection, header, packetData)
		}
		newPacket = NewPacket(packetData)
		return
	}
	//if len(data) >= PacketHeaderSize {
	//	header := &PacketHeader{}
	//	header.ReadFrom(data)
	//	if len(data) >= PacketHeaderSize+int(header.GetLen()) {
	//		return NewPacket(data[PacketHeaderSize:PacketHeaderSize+int(header.GetLen())])
	//	}
	//}
	return nil,ErrNotSupport
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
			HeaderEncoder: func(connection Connection, packet *Packet, headerData []byte) {
				xorEncode(headerData, key)
			},
			DataEncoder: func(connection Connection, packet *Packet, packetData []byte) []byte {
				xorEncode(packetData, key)
				return packetData
			},
			HeaderDecoder: func(connection Connection, headerData []byte) {
				xorEncode(headerData, key)
			},
			DataDecoder: func(connection Connection, packetHeader *PacketHeader, packetData []byte) []byte {
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
