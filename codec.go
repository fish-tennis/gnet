package gnet

// 连接的编解码接口
type Codec interface {
	// 包头长度
	// 应用层可以自己扩展包头长度
	PacketHeaderSize() uint32

	// 编码接口
	Encode(connection Connection, packet Packet) []byte

	// 解码接口
	Decode(connection Connection, data []byte) (newPacket Packet, err error)
}

// 用了RingBuffer的连接的编解码接口
// 流格式: Length+Data
// 这里把编解码分成了2层
// 第1层:从RingBuffer里取出原始包体数据
// 第2层:对包数据执行实际的编解码操作
type RingBufferCodec struct {
	// 包头的编码接口,包头长度不能变
	HeaderEncoder func(connection Connection, packet Packet, headerData []byte)
	// 包体的编码接口
	// NOTE:返回值允许返回多个[]byte,如ProtoPacket在编码时,可以分别返回command和proto.Message的序列化[]byte
	// 如果只返回一个[]byte,就需要把command和proto.Message序列化的[]byte再合并成一个[]byte,造成性能损失
	DataEncoder func(connection Connection, packet Packet) [][]byte
	// 包头的解码接口,包头长度不能变
	HeaderDecoder func(connection Connection, headerData []byte)
	// 包体的解码接口
	DataDecoder func(connection Connection, packetHeader *PacketHeader, packetData []byte) Packet
}

func (this *RingBufferCodec) PacketHeaderSize() uint32 {
	return uint32(DefaultPacketHeaderSize)
}

// TcpConnection做了优化,在Encode的过程中就直接写入sendBuffer
// 返回值:未能写入sendBuffer的数据(sendBuffer写满了的情况)
func (this *RingBufferCodec) Encode(connection Connection, packet Packet) []byte {
	// 优化思路:编码后的数据直接写入RingBuffer.sendBuffer,可以减少一些内存分配
	if tcpConnection,ok := connection.(*TcpConnection); ok {
		packetHeaderSize := int(this.PacketHeaderSize())
		sendBuffer := tcpConnection.sendBuffer
		var encodedData [][]byte
		if this.DataEncoder != nil {
			encodedData = this.DataEncoder(connection, packet)
		} else {
			encodedData = [][]byte{packet.GetStreamData()}
		}
		encodedDataLen := 0
		for _,data := range encodedData {
			encodedDataLen += len(data)
		}
		packetHeader := NewPacketHeader(uint32(encodedDataLen), 0)
		writeBuffer := sendBuffer.WriteBuffer()
		if packetHeaderSize == DefaultPacketHeaderSize && len(writeBuffer) >= packetHeaderSize {
			// 有足够的连续空间可写,则直接写入RingBuffer里
			// 省掉了一次内存分配操作: make([]byte, PacketHeaderSize)
			packetHeader.WriteTo(writeBuffer)
			if this.HeaderEncoder != nil {
				this.HeaderEncoder(connection, packet, writeBuffer[0:packetHeaderSize])
			}
			sendBuffer.SetWrited(packetHeaderSize)
		} else {
			// 没有足够的连续空间可写,则能写多少写多少,有可能一部分写入尾部,一部分写入头部
			packetHeaderData := make([]byte, packetHeaderSize)
			packetHeader.WriteTo(packetHeaderData)
			if this.HeaderEncoder != nil {
				this.HeaderEncoder(connection, packet, packetHeaderData)
			}
			writedHeaderLen,_ := sendBuffer.Write(packetHeaderData)
			if writedHeaderLen < packetHeaderSize {
				// 写不下的包头数据和包体数据,返回给TcpConnection延后处理
				// 合理的设置发包缓存,一般不会运行到这里
				remainData := make([]byte, packetHeaderSize-writedHeaderLen+encodedDataLen)
				// 没写完的header数据
				n := copy(remainData, packetHeaderData[writedHeaderLen:])
				// 编码后的包体数据
				for _,data := range encodedData {
					n += copy(remainData[n:], data)
				}
				return remainData
			}
		}
		writeBuffer = sendBuffer.WriteBuffer()
		writetedDataLen := 0
		for i,data := range encodedData {
			writed,_ := sendBuffer.Write(data)
			writetedDataLen += writed
			if writed < len(data) {
				// 写不下的包体数据,返回给TcpConnection延后处理
				remainData := make([]byte, encodedDataLen-writetedDataLen)
				n := copy(remainData, data[writed:])
				for j := i+1; j < len(encodedData); j++ {
					n += copy(remainData[n:], encodedData[j])
				}
				return remainData
			}
		}
		return nil
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
	return packet.GetStreamData()
}

func (this *RingBufferCodec) Decode(connection Connection, data []byte) (newPacket Packet, err error) {
	if tcpConnection,ok := connection.(*TcpConnection); ok {
		// TcpConnection用了RingBuffer,解码时,尽可能的不产生copy
		packetHeaderSize := int(this.PacketHeaderSize())
		recvBuffer := tcpConnection.recvBuffer
		if recvBuffer.UnReadLength() < packetHeaderSize {
			return
		}
		var packetHeaderData []byte
		readBuffer := recvBuffer.ReadBuffer()
		if len(readBuffer) >= packetHeaderSize {
			if this.HeaderDecoder != nil {
				// 如果header需要解码,那么这里就必须copy了,因为这时还不能确定收到完整的包,所以不能对原始数据进行修改
				packetHeaderData = make([]byte, packetHeaderSize)
				copy(packetHeaderData, readBuffer)
			} else {
				// 这里不产生copy
				packetHeaderData = readBuffer[0:packetHeaderSize]
			}
		} else {
			// TODO: 优化思路: packetHeaderData用sync.pool创建
			packetHeaderData = make([]byte, packetHeaderSize)
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
		if int(header.GetLen()) > recvBuffer.Size() - packetHeaderSize {
			return nil, ErrPacketLength
		}
		if header.GetLen() > tcpConnection.config.MaxPacketSize {
			return nil, ErrPacketLength
		}
		if recvBuffer.UnReadLength() < packetHeaderSize + int(header.GetLen()) {
			return
		}
		recvBuffer.SetReaded(packetHeaderSize)
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
			newPacket = this.DataDecoder(connection, header, packetData)
		} else {
			newPacket = NewDataPacket(packetData)
		}
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
