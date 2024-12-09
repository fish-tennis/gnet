package gnet

import "io"

// 连接的编解码接口
// interface for encoding and decoding
type Codec interface {
	// 包头长度
	// 应用层可以自己扩展包头长度
	// the packet header size (without packet data)
	PacketHeaderSize() uint32

	// 创建消息头
	// generate a packet header
	// packet can be nil
	// packetData is the data after encode,can be nil
	CreatePacketHeader(connection Connection, packet Packet, packetData []byte) PacketHeader

	// encoding
	Encode(connection Connection, packet Packet) []byte

	// decoding
	Decode(connection Connection, data []byte) (newPacket Packet, err error)
}

// 用了RingBuffer的连接的编解码接口
// a codec with RingBuffer
// stream format: Length+Data
// process divided into two layers
// layer1: retrieve the original package data from RingBuffer
// layer2: encode or decode of the original package data
// NOTE: only support TcpConnection
type RingBufferCodec struct {
	// 包头的编码接口,包头长度不能变
	// encoder for packer header
	HeaderEncoder func(connection Connection, packet Packet, headerData []byte)

	// 包体的编码接口
	// NOTE:返回值允许返回多个[]byte,如ProtoPacket在编码时,可以分别返回command和proto.Message的序列化[]byte
	// 如果只返回一个[]byte,就需要把command和proto.Message序列化的[]byte再合并成一个[]byte,造成性能损失
	// encoder for packer data
	// allow return multiple []byte,for example:when encode ProtoPacket, return two []byte(serialized data of command and proto.Message)
	DataEncoder func(connection Connection, packet Packet) ([][]byte, uint8)

	// 包头的解码接口,包头长度不能变
	// decoder for packer header
	HeaderDecoder func(connection Connection, headerData []byte)

	// 包体的解码接口
	// decoder for packer data
	DataDecoder func(connection Connection, packetHeader PacketHeader, packetData []byte) Packet
}

func (this *RingBufferCodec) PacketHeaderSize() uint32 {
	return uint32(DefaultPacketHeaderSize)
}

func (this *RingBufferCodec) CreatePacketHeader(connection Connection, packet Packet, packetData []byte) PacketHeader {
	return NewDefaultPacketHeader(0, 0)
}

// TcpConnection做了优化,在Encode的过程中就直接写入sendBuffer
// 返回值:未能写入sendBuffer的数据(sendBuffer写满了的情况)
// encode packet and write encoded data to sendBuffer
// return the remain encoded data not wrote to sendBuffer(when sendBuffer is full)
func (this *RingBufferCodec) Encode(connection Connection, packet Packet) []byte {
	// 优化思路:编码后的数据直接写入RingBuffer.sendBuffer,可以减少一些内存分配
	if tcpConnection, ok := connection.(*TcpConnection); ok {
		packetHeaderSize := int(this.PacketHeaderSize())
		sendBuffer := tcpConnection.sendBuffer
		var encodedData [][]byte
		headerFlags := uint8(0)
		if this.DataEncoder != nil {
			// 编码接口可能把消息分解成了几段字节流数组,如消息头和消息体
			// 如果只是返回一个[]byte结果的话,那么编码接口还需要把消息头和消息体进行合并,从而多一次内存分配和拷贝
			// encoder may decompose the packet into several []byte,such as the packet header and packet body
			encodedData, headerFlags = this.DataEncoder(connection, packet)
		} else {
			// 支持在应用层做数据包的序列化和编码
			// support direct encoded data from outside
			encodedData = [][]byte{packet.GetStreamData()}
		}
		encodedDataLen := 0
		for _, data := range encodedData {
			encodedDataLen += len(data)
		}
		packetHeader := NewDefaultPacketHeader(uint32(encodedDataLen), headerFlags)
		writeBuffer := sendBuffer.WriteBuffer()
		if packetHeaderSize == DefaultPacketHeaderSize && len(writeBuffer) >= packetHeaderSize {
			// 有足够的连续空间可写,则直接写入RingBuffer里
			// 省掉了一次内存分配操作: make([]byte, PacketHeaderSize)
			// If there is enough continuous space to write, write directly to RingBuffer
			// reduce one memory allocation operation: make([]byte, PacketHeaderSize)
			packetHeader.WriteTo(writeBuffer)
			if this.HeaderEncoder != nil {
				this.HeaderEncoder(connection, packet, writeBuffer[0:packetHeaderSize])
			}
			sendBuffer.SetWrote(packetHeaderSize)
		} else {
			// 没有足够的连续空间可写,则能写多少写多少,有可能一部分写入尾部,一部分写入头部
			// If there is not enough continuous space to write, write as much as possible,
			// with some being written to the tail and some being written to the head
			packetHeaderData := make([]byte, packetHeaderSize)
			packetHeader.WriteTo(packetHeaderData)
			if this.HeaderEncoder != nil {
				this.HeaderEncoder(connection, packet, packetHeaderData)
			}
			writedHeaderLen, _ := sendBuffer.Write(packetHeaderData)
			if writedHeaderLen < packetHeaderSize {
				// 写不下的包头数据和包体数据,返回给TcpConnection延后处理
				// 合理的设置发包缓存,一般不会运行到这里
				// The header and body data that cannot be written are returned for delayed processing
				remainData := make([]byte, packetHeaderSize-writedHeaderLen+encodedDataLen)
				// 没写完的header数据
				// copy header data not written to RingBuffer
				n := copy(remainData, packetHeaderData[writedHeaderLen:])
				// 编码后的包体数据
				// copy encoded packet body
				for _, data := range encodedData {
					n += copy(remainData[n:], data)
				}
				return remainData
			}
		}
		writeBuffer = sendBuffer.WriteBuffer()
		writedDataLen := 0
		for i, data := range encodedData {
			writed, _ := sendBuffer.Write(data)
			writedDataLen += writed
			if writed < len(data) {
				// 写不下的包体数据,返回给TcpConnection延后处理
				// The body data that cannot be written are returned for delayed processing
				remainData := make([]byte, encodedDataLen-writedDataLen)
				n := copy(remainData, data[writed:])
				for j := i + 1; j < len(encodedData); j++ {
					n += copy(remainData[n:], encodedData[j])
				}
				return remainData
			}
		}
		return nil
	}
	return packet.GetStreamData()
}

func (this *RingBufferCodec) Decode(connection Connection, data []byte) (newPacket Packet, err error) {
	if tcpConnection, ok := connection.(*TcpConnection); ok {
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
				// reduce copy operate
				packetHeaderData = readBuffer[0:packetHeaderSize]
			} else {
				//packetHeaderData = make([]byte, packetHeaderSize)
				packetHeaderData = tcpConnection.tmpReadPacketHeaderData
				// 先拷贝RingBuffer的尾部
				// copy tail of RingBuffer
				n := copy(packetHeaderData, readBuffer)
				// 再拷贝RingBuffer的头部
				// copy head of RingBuffer
				copy(packetHeaderData[n:], recvBuffer.buffer)
			}
			recvBuffer.SetReaded(packetHeaderSize)
			if this.HeaderDecoder != nil {
				this.HeaderDecoder(connection, packetHeaderData)
			}
			tcpConnection.curReadPacketHeader = tcpConnection.tmpReadPacketHeader
			tcpConnection.curReadPacketHeader.ReadFrom(packetHeaderData)
			// 数据包长度超出设置
			// check packet length
			if tcpConnection.config.MaxPacketSize > 0 && tcpConnection.curReadPacketHeader.Len() > tcpConnection.config.MaxPacketSize {
				logger.Error("%v ErrPacketLengthExceed len:%v max:%v", tcpConnection.GetConnectionId(), tcpConnection.curReadPacketHeader.Len(), tcpConnection.config.MaxPacketSize)
				logger.Error("curReadPacketHeader:%v", tcpConnection.curReadPacketHeader)
				return nil, ErrPacketLengthExceed
			}
		}

		//header := &PacketHeader{}
		header := tcpConnection.curReadPacketHeader
		//header.ReadFrom(packetHeaderData)
		// 数据包长度超出设置
		// check packet length
		if tcpConnection.config.MaxPacketSize > 0 && header.Len() > tcpConnection.config.MaxPacketSize {
			logger.Error("%v ErrPacketLengthExceed len:%v max:%v", tcpConnection.GetConnectionId(), header.Len(), tcpConnection.config.MaxPacketSize)
			return nil, ErrPacketLengthExceed
		}
		var packetData []byte
		// 数据包没有超出RingBuffer大小
		// packet body size less than size of RingBuffer
		if int(header.Len()) <= recvBuffer.Size() {
			if recvBuffer.UnReadLength() < int(header.Len()) {
				// 包体数据还没收完整
				return
			}
			// 从RingBuffer中读取完整包体数据
			// read full packet body data from RingBuffer
			packetData = recvBuffer.ReadFull(int(header.Len()))
		} else {
			// 数据包超出了RingBuffer大小
			// 为什么要处理数据包超出RingBuffer大小的情况?
			// 因为RingBuffer是一种内存换时间的解决方案,对于处理大量连接的应用场景,内存也是要考虑的因素
			// 有一些应用场景,大部分数据包都不大,但是有少量数据包非常大,如果RingBuffer必须设置的比最大数据包还要大,可能消耗过多内存
			// packet body size exceeds size of RingBuffer
			// why handle packet body size exceeding the RingBuffer size?
			// because RingBuffer is a solution that use more memory to reduce time, but memory is also a factor to consider for lots of connections
			// in some cases, most packet are small, but a few are very large. if RingBuffer must be set to be larger than the maximum packet size, it may consume too much memory

			// 剩余的包体数据,RingBuffer里面可能已经收了一部分包体数据
			// remain packet body, there may have some packet body data in RingBuffer
			remainDataSize := int(header.Len()) - recvBuffer.UnReadLength()
			remainData := make([]byte, remainDataSize)
			// 阻塞读取剩余的包体数据
			// read remaining packet body data blocking
			readLen, _ := io.ReadFull(tcpConnection.conn, remainData)
			if readLen != remainDataSize {
				return nil, ErrReadRemainPacket
			}
			if recvBuffer.UnReadLength() == 0 {
				// 包体数据全部是从io.ReadFull读取到的
				// all packet body data is read from io.ReadFull
				packetData = remainData
			} else {
				// 有一部分数据在RingBuffer里面,重组数据
				// part of body data in RingBuffer,need reorganizing data
				packetData = make([]byte, header.Len())
				dataInRingBuffer := recvBuffer.ReadFull(recvBuffer.UnReadLength())
				// 先拷贝RingBuffer里面已经收到的一部分包体数据
				// copy data in RingBuffer
				n := copy(packetData, dataInRingBuffer)
				// 再拷贝io.ReadFull读取到的
				// copy data read from io.ReadFull
				copy(packetData[n:], remainData)
			}
		}
		if this.DataDecoder != nil {
			// 包体的解码接口
			newPacket = this.DataDecoder(connection, header, packetData)
		} else {
			newPacket = NewDataPacketWithHeader(header, packetData)
		}
		tcpConnection.curReadPacketHeader = nil
		return
	}
	return nil, ErrNotSupport
}

// 默认编解码,只做长度和数据的解析
// default codec, which format is length + data
type DefaultCodec struct {
	RingBufferCodec
}

func NewDefaultCodec() *DefaultCodec {
	return &DefaultCodec{}
}
