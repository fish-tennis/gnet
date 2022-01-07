package gnet

import "io"

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
			// 编码接口可能把消息分解成了几段字节流数组,如消息头和消息体
			// 如果只是返回一个[]byte结果的话,那么编码接口还需要把消息头和消息体进行合并,从而多一次内存分配和拷贝
			encodedData = this.DataEncoder(connection, packet)
		} else {
			// 支持在应用层做数据包的序列化和编码
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
		writedDataLen := 0
		for i,data := range encodedData {
			writed,_ := sendBuffer.Write(data)
			writedDataLen += writed
			if writed < len(data) {
				// 写不下的包体数据,返回给TcpConnection延后处理
				remainData := make([]byte, encodedDataLen-writedDataLen)
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
		recvBuffer := tcpConnection.recvBuffer
		// 先解码包头
		if tcpConnection.curReadPacketHeader == nil {
			packetHeaderSize := int(this.PacketHeaderSize())
			if recvBuffer.UnReadLength() < packetHeaderSize {
				return
			}
			var packetHeaderData []byte
			readBuffer := recvBuffer.ReadBuffer()
			if len(readBuffer) >= packetHeaderSize {
				// 这里不产生copy
				packetHeaderData = readBuffer[0:packetHeaderSize]
			} else {
				//packetHeaderData = make([]byte, packetHeaderSize)
				packetHeaderData = tcpConnection.tmpReadPacketHeaderData
				// 先拷贝RingBuffer的尾部
				n := copy(packetHeaderData, readBuffer)
				// 再拷贝RingBuffer的头部
				copy(packetHeaderData[n:], recvBuffer.buffer)
			}
			recvBuffer.SetReaded(packetHeaderSize)
			if this.HeaderDecoder != nil {
				this.HeaderDecoder(connection, packetHeaderData)
			}
			tcpConnection.curReadPacketHeader = &tcpConnection.tmpReadPacketHeader
			tcpConnection.curReadPacketHeader.ReadFrom(packetHeaderData)
			// 数据包长度超出设置
			if tcpConnection.config.MaxPacketSize > 0 && tcpConnection.curReadPacketHeader.Len() > tcpConnection.config.MaxPacketSize {
				return nil, ErrPacketLengthExceed
			}
		}

		//header := &PacketHeader{}
		header := tcpConnection.curReadPacketHeader
		//header.ReadFrom(packetHeaderData)
		// 数据包长度超出设置
		if tcpConnection.config.MaxPacketSize > 0 && header.Len() > tcpConnection.config.MaxPacketSize {
			return nil, ErrPacketLengthExceed
		}
		var packetData []byte
		// 数据包没有超出RingBuffer大小
		if int(header.Len()) <= recvBuffer.Size() {
			if recvBuffer.UnReadLength() < int(header.Len()) {
				// 包体数据还没收完整
				return
			}
			// 从RingBuffer中读取完整包体数据
			packetData = recvBuffer.ReadFull(int(header.Len()))
		} else {
			// 数据包超出了RingBuffer大小
			// 为什么要处理数据包超出RingBuffer大小的情况?
			// 因为RingBuffer是一种内存换时间的解决方案,对于处理大量连接的应用场景,内存也是要考虑的因素
			// 有一些应用场景,大部分数据包都不大,但是有少量数据包非常大,如果RingBuffer必须设置的比最大数据包还要大,可能消耗过多内存

			// 剩余的包体数据,RingBuffer里面可能已经收了一部分包体数据
			remainDataSize := int(header.Len())  - recvBuffer.UnReadLength()
			remainData := make([]byte, remainDataSize)
			// 阻塞读取剩余的包体数据
			readLen,_ := io.ReadFull(tcpConnection.conn, remainData)
			if readLen != remainDataSize {
				return nil,ErrReadRemainPacket
			}
			if recvBuffer.UnReadLength() == 0 {
				// 包体数据全部是从io.ReadFull读取到的
				packetData = remainData
			} else {
				// 有一部分数据在RingBuffer里面,重组数据
				packetData = make([]byte, header.Len())
				dataInRingBuffer := recvBuffer.ReadFull(recvBuffer.UnReadLength())
				// 先拷贝RingBuffer里面已经收到的一部分包体数据
				n := copy(packetData, dataInRingBuffer)
				// 再拷贝io.ReadFull读取到的
				copy(packetData[n:], remainData)
			}
		}
		if this.DataDecoder != nil {
			// 包体的解码接口
			newPacket = this.DataDecoder(connection, header, packetData)
		} else {
			newPacket = NewDataPacket(packetData)
		}
		tcpConnection.curReadPacketHeader = nil
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
