package gnet

import (
	"io"
	"net"
	"sync"
	"time"
)

type TcpConnection struct {
	baseConnection
	conn net.Conn
	// 防止执行多次关闭操作
	closeOnce sync.Once
	// 关闭回调
	onClose func(connection Connection)
	// 最近收到完整数据包的时间(时间戳:秒)
	lastRecvPacketTick uint32
	// 发包缓存
	sendPacketCache chan *Packet
}

func NewTcpConnector(config ConnectionConfig, codec Codec, handler ConnectionHandler) *TcpConnection {
	return &TcpConnection{
		baseConnection: baseConnection{
			connectionId: newConnectionId(),
			isConnector: true,
			config: config,
			codec: codec,
			handler: handler,
			//sendBuffer: &MessageBuffer{
			//	buffer: make(chan []byte, config.SendBufferSize),
			//},
		},
		sendPacketCache: make(chan *Packet, config.SendPacketCacheCap),
	}
}

func NewTcpConnectionAccept(conn net.Conn, config ConnectionConfig, codec Codec, handler ConnectionHandler) *TcpConnection {
	return &TcpConnection{
		baseConnection: baseConnection{
			connectionId: newConnectionId(),
			isConnector: false,
			config: config,
			codec: codec,
			handler: handler,
			//sendBuffer: &MessageBuffer{
			//	buffer: make(chan []byte, config.SendBufferSize),
			//},
		},
		sendPacketCache: make(chan *Packet, config.SendPacketCacheCap),
		conn: conn,
	}
}

//// 发送数据
//func (this* TcpConnection) Send(data []byte) bool {
//	return false
//}

// 连接
func (this *TcpConnection) Connect(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		this.isConnected = false
		LogDebug("Connect failed %v: %v", this.GetConnectionId(), err)
		if this.handler != nil {
			this.handler.OnConnected(this,false)
		}
		return false
	}
	this.conn = conn
	this.isConnected = true
	if this.handler != nil {
		this.handler.OnConnected(this,true)
	}
	return true
}

// 开启读写协程
func (this *TcpConnection) Start(closeNotify chan struct{}) {
	// 开启收包协程
	go func() {
		this.readLoop()
		this.Close()
	}()

	// 开启发包协程
	go func() {
		this.writeLoop(closeNotify)
		this.Close()
	}()
}

// 收包过程
func (this *TcpConnection) readLoop() {
	defer func() {
		if err := recover(); err != nil {
			LogDebug("readLoop fatal %v: %v", this.GetConnectionId(), err.(error))
		}
	}()

	LogDebug("readLoop begin %v", this.GetConnectionId())
	packetHeaderData := make([]byte, PacketHeaderSize)
	packetHeader := &PacketHeader{}
	for this.isConnected {
		// TODO:目前方案是分别读取消息头和消息体,就算系统已经收到了多个包的数据,但是当前方案,依然需要多次io.ReadFull才能
		//把数据读出来
		// 待对比方案:系统当前有多少数据就读取多少,再自己进行分包(是否减少了系统调用的次数,从而提升了性能?)

		// 此处阻塞读,当连接关闭时,会中断阻塞,返回err
		// TODO:改为this.conn.Read,有多少就读出来多少
		_, err := io.ReadFull(this.conn, packetHeaderData)
		if err != nil {
			if err != io.EOF {
				// ...
			}
			LogDebug("readLoop %v err:%v", this.GetConnectionId(), err)
			break
		}
		decodePacketHeaderData := packetHeaderData
		if this.codec != nil {
			// 解码包头
			decodePacketHeaderData = this.codec.DecodeHeader(packetHeaderData)
			if decodePacketHeaderData == nil {
				LogDebug("readLoop %v decode header err", this.GetConnectionId())
				break
			}
		}
		packetHeader.ReadFrom(decodePacketHeaderData)
		if packetHeader.GetLen() == 0 {
			LogDebug("readLoop %v packetHeader len err", this.GetConnectionId())
			break
		}
		// 检查最大数据包限制
		if packetHeader.GetLen() >= MaxPacketDataSize {
			LogDebug("readLoop %v packetHeader len err:%v", this.GetConnectionId(), packetHeader.GetLen())
			break
		}
		// 检查自己设置的最大数据包限制
		if this.config.MaxPacketSize > 0 && packetHeader.GetLen() > 0 {
			LogDebug("readLoop %v packetHeader len err:%v>%v", this.GetConnectionId(), packetHeader.GetLen(), this.config.MaxPacketSize)
			break
		}
		packetData := make([]byte, packetHeader.GetLen())
		// 此处阻塞读,当连接关闭时,会中断阻塞,返回err
		_, dataErr := io.ReadFull(this.conn, packetData)
		if dataErr != nil {
			if dataErr != io.EOF {
				// ...
			}
			LogDebug("readLoop %v err:%v", this.GetConnectionId(), dataErr)
			break
		}
		decodePacketData := packetData
		if this.codec != nil {
			// 解码包体
			decodePacketData = this.codec.DecodeData(packetData)
		}
		// 最近收到完整数据包的时间
		// 有一种极端情况,网速太慢,即使没有掉线,也可能触发收包超时检测
		this.lastRecvPacketTick = GetCurrentTimeStamp()
		// TODO:根据messageHeader.GetFlags()的值对messageData进行处理
		if this.handler != nil {
			this.handler.OnRecvPacket(this, &Packet{data: decodePacketData})
		}
	}
	LogDebug("readLoop end %v", this.GetConnectionId())
}

// 发包过程
func (this *TcpConnection) writeLoop(closeNotify chan struct{}) {
	defer func() {
		if err := recover(); err != nil {
			LogDebug("writeLoop fatal %v: %v", this.GetConnectionId(), err.(error))
			LogStack()
		}
		LogDebug("writeLoop end %v", this.GetConnectionId())
	}()

	LogDebug("writeLoop begin %v", this.GetConnectionId())
	// 收包超时计时,用于检测掉线
	recvTimeoutTimer := time.NewTimer(time.Second * time.Duration(this.config.RecvTimeout))
	defer recvTimeoutTimer.Stop()
	ringBufferSize := this.config.BatchPacketBufferSize
	if ringBufferSize == 0 {
		if this.config.MaxPacketSize > 0 {
			ringBufferSize = this.config.MaxPacketSize*2
		} else {
			ringBufferSize = 65535
		}
	}
	sendBuffer := NewRingBuffer(int(ringBufferSize))
	for this.isConnected {
		select {
		case packet := <-this.sendPacketCache:
			if packet == nil {
				LogDebug("packet==nil %v", this.GetConnectionId())
				return
			}
			// 数据包编码
			decodePacketData := packet.data
			if this.codec != nil {
				decodePacketData = this.codec.Encode(packet.data)
			}
			if _,bufferErr := sendBuffer.Write(decodePacketData); bufferErr != nil {
				LogDebug("sendBuffer is full %v", this.GetConnectionId())
				return
			}
			LogDebug("packet %v len:%v decodeLen:%v unReadLen:%v", this.GetConnectionId(), len(packet.data), len(decodePacketData), sendBuffer.UnReadLength())

		case <-recvTimeoutTimer.C:
			nextTimeoutTime := this.config.RecvTimeout + this.lastRecvPacketTick - GetCurrentTimeStamp()
			if nextTimeoutTime > 0 {
				recvTimeoutTimer.Reset(time.Second * time.Duration(nextTimeoutTime))
			} else {
				// 指定时间内,一直未读取到数据包,则认为该连接掉线了,可能处于"假死"状态了
				// 需要主动关闭该连接,防止连接"泄漏"
				LogDebug("recv timeout %v", this.GetConnectionId())
				return
			}

		case <-closeNotify:
			// 收到外部的关闭通知
			LogDebug("recv closeNotify %v", this.GetConnectionId())
			return
		}

		if sendBuffer.UnReadLength() > 0 {
			// 可读数据有可能分别存在数组的尾部和头部,所以需要循环发送,有可能发送2次
			for this.isConnected && sendBuffer.UnReadLength() > 0 {
				if this.config.WriteTimeout > 0 {
					setTimeoutErr := this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout)*time.Second))
					// Q:什么情况会导致SetWriteDeadline返回err?
					if setTimeoutErr != nil {
						// ...
						LogDebug("%v setTimeoutErr:%v", this.GetConnectionId(), setTimeoutErr)
						break
					}
				}
				readBuffer := sendBuffer.ReadBuffer()
				//LogDebug("%v readBuffer:%v", this.GetConnectionId(), len(readBuffer))
				writeCount, err := this.conn.Write(readBuffer)
				if err != nil {
					// ...
					LogDebug("%v write Err:%v", this.GetConnectionId(), err)
					break
				}
				sendBuffer.SetReaded(writeCount)
				//LogDebug("%v write count:%v unread:%v", this.GetConnectionId(), writeCount, sendBuffer.UnReadLength())
			}
		}
	}
}

// 关闭
func (this *TcpConnection) Close() {
	this.closeOnce.Do(func() {
		this.isConnected = false
		if this.conn != nil {
			this.conn.Close()
			LogDebug("close %v", this.GetConnectionId())
			//this.conn = nil
		}
		if this.onClose != nil {
			this.onClose(this)
		}
	})
}

// 异步发送数据
func (this *TcpConnection) Send(packet *Packet) bool {
	this.sendPacketCache <- packet
	return true
}
