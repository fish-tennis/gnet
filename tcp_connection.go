package gnet

import (
	"context"
	"google.golang.org/protobuf/proto"
	"io"
	"net"
	"sync"
	"time"
)

type TcpConnection struct {
	baseConnection
	conn net.Conn
	// 读协程结束标记
	readStopNotifyChan chan struct{}
	// 防止执行多次关闭操作
	closeOnce sync.Once
	// 关闭回调
	onClose func(connection Connection)
	// 最近收到完整数据包的时间(时间戳:秒)
	lastRecvPacketTick int64
	// 发包缓存chan
	sendPacketCache chan Packet
	// 发包RingBuffer
	sendBuffer *RingBuffer
	// 收包RingBuffer
	recvBuffer *RingBuffer
	// 解码时,用到的一些临时变量
	tmpReadPacketHeader PacketHeader
	tmpReadPacketHeaderData []byte
	curReadPacketHeader PacketHeader
}

func NewTcpConnector(config *ConnectionConfig, codec Codec, handler ConnectionHandler) *TcpConnection {
	if config.MaxPacketSize == 0 {
		config.MaxPacketSize = MaxPacketDataSize
	}
	if config.MaxPacketSize > MaxPacketDataSize {
		config.MaxPacketSize = MaxPacketDataSize
	}
	newConnection := createTcpConnection(config, codec, handler)
	newConnection.isConnector = true
	return newConnection
}

func NewTcpConnectionAccept(conn net.Conn, config *ConnectionConfig, codec Codec, handler ConnectionHandler) *TcpConnection {
	if config.MaxPacketSize == 0 {
		config.MaxPacketSize = MaxPacketDataSize
	}
	if config.MaxPacketSize > MaxPacketDataSize {
		config.MaxPacketSize = MaxPacketDataSize
	}
	newConnection := createTcpConnection(config, codec, handler)
	newConnection.isConnector = false
	newConnection.isConnected = true
	newConnection.conn = conn
	return newConnection
}

func createTcpConnection(config *ConnectionConfig, codec Codec, handler ConnectionHandler) *TcpConnection {
	newConnection := &TcpConnection{
		baseConnection: baseConnection{
			connectionId: NewConnectionId(),
			config:       config,
			codec:        codec,
			handler:      handler,
		},
		readStopNotifyChan: make(chan struct{}, 1),
		sendPacketCache:     make(chan Packet, config.SendPacketCacheCap),
	}
	newConnection.tmpReadPacketHeader = codec.CreatePacketHeader(newConnection, nil, nil)
	return newConnection
}

// 连接
func (this *TcpConnection) Connect(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		this.isConnected = false
		logger.Error("Connect failed %v: %v", this.GetConnectionId(), err.Error())
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
func (this *TcpConnection) Start(ctx context.Context, netMgrWg *sync.WaitGroup, onClose func(connection Connection)) {
	this.onClose = onClose
	// 开启收包协程
	netMgrWg.Add(1)
	go func() {
		defer func() {
			netMgrWg.Done()
			if err := recover(); err != nil {
				logger.Error("read fatal %v: %v", this.GetConnectionId(), err.(error))
				LogStack()
			}
		}()
		this.readLoop()
		this.Close()
		// 读协程结束了,通知写协程也结束
		this.readStopNotifyChan <- struct{}{}
	}()

	// 开启发包协程
	netMgrWg.Add(1)
	go func(ctx context.Context) {
		defer func() {
			netMgrWg.Done()
			if err := recover(); err != nil {
				logger.Error("write fatal %v: %v", this.GetConnectionId(), err.(error))
				LogStack()
			}
		}()
		this.writeLoop(ctx)
		this.Close()
	}(ctx)
}

// 收包过程
func (this *TcpConnection) readLoop() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("readLoop fatal %v: %v", this.GetConnectionId(), err.(error))
			LogStack()
		}
	}()

	logger.Debug("readLoop begin %v", this.GetConnectionId())
	this.recvBuffer = this.createRecvBuffer()
	this.tmpReadPacketHeaderData = make([]byte,this.codec.PacketHeaderSize())
	for this.isConnected {
		// 可写入的连续buffer
		writeBuffer := this.recvBuffer.WriteBuffer()
		if len(writeBuffer) == 0 {
			// 不会运行到这里来,除非recvBuffer的大小设置太小:小于了包头的长度
			logger.Error("%v recvBuffer full", this.GetConnectionId())
			return
		}
		n,err := this.conn.Read(writeBuffer)
		if err != nil {
			if err != io.EOF {
				logger.Debug("readLoop %v err:%v", this.GetConnectionId(), err.Error())
			}
			break
		}
		//LogDebug("%v Read:%v", this.GetConnectionId(), n)
		this.recvBuffer.SetWrited(n)
		for this.isConnected {
			newPacket,decodeError := this.codec.Decode(this, this.recvBuffer.ReadBuffer())
			if decodeError != nil {
				logger.Error("%v decodeError:%v", this.GetConnectionId(), decodeError.Error())
				return
			}
			if newPacket == nil {
				break
			}
			// 最近收到完整数据包的时间
			// 有一种极端情况,网速太慢,即使没有掉线,也可能触发收包超时检测
			this.lastRecvPacketTick = GetCurrentTimeStamp()
			if this.handler != nil {
				this.handler.OnRecvPacket(this, newPacket)
			}
		}
	}
	logger.Debug("readLoop end %v", this.GetConnectionId())
}

// 发包过程
func (this *TcpConnection) writeLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("writeLoop fatal %v: %v", this.GetConnectionId(), err.(error))
			//LogStack()
		}
		logger.Debug("writeLoop end %v", this.GetConnectionId())
	}()

	logger.Debug("writeLoop begin %v", this.GetConnectionId())
	// 收包超时计时,用于检测掉线
	recvTimeoutTimer := time.NewTimer(time.Second * time.Duration(this.config.RecvTimeout))
	defer recvTimeoutTimer.Stop()
	// 心跳包计时
	heartBeatTimer := time.NewTimer(time.Second * time.Duration(this.config.HeartBeatInterval))
	defer heartBeatTimer.Stop()
	this.sendBuffer = this.createSendBuffer()
	for this.isConnected {
		var delaySendDecodePacketData []byte
		select {
		case packet := <-this.sendPacketCache:
			if packet == nil {
				logger.Debug("packet==nil %v", this.GetConnectionId())
				return
			}
			// 数据包编码
			// Encode里面会把编码后的数据直接写入sendBuffer
			delaySendDecodePacketData = this.codec.Encode(this, packet)
			if len(delaySendDecodePacketData) > 0 {
				// Encode里面写不完的数据延后处理
				logger.Debug("%v sendBuffer is full delaySize:%v", this.GetConnectionId(), len(delaySendDecodePacketData))
				break
			}
			packetCount := len(this.sendPacketCache)
			// 还有其他数据包在chan里,就进行批量合并
			if packetCount > 0 {
				for i := 0; i < packetCount; i++ {
					// 这里不会阻塞
					newPacket,ok := <-this.sendPacketCache
					if !ok {
						logger.Debug("newPacket==nil %v", this.GetConnectionId())
						return
					}
					// 数据包编码
					delaySendDecodePacketData = this.codec.Encode(this, newPacket)
					if len(delaySendDecodePacketData) > 0 {
						logger.Debug("%v sendBuffer is full delaySize:%v", this.GetConnectionId(), len(delaySendDecodePacketData))
						break
					}
				}
			}
			//LogDebug("%v packetCount:%v unReadLen:%v", this.GetConnectionId(), packetCount+1, sendBuffer.UnReadLength())

		case <-recvTimeoutTimer.C:
			if this.config.RecvTimeout > 0 {
				nextTimeoutTime := int64(this.config.RecvTimeout) + this.lastRecvPacketTick - GetCurrentTimeStamp()
				if nextTimeoutTime > 0 {
					if nextTimeoutTime > int64(this.config.RecvTimeout) {
						nextTimeoutTime = int64(this.config.RecvTimeout)
					}
					recvTimeoutTimer.Reset(time.Second * time.Duration(nextTimeoutTime))
				} else {
					// 指定时间内,一直未读取到数据包,则认为该连接掉线了,可能处于"假死"状态了
					// 需要主动关闭该连接,防止连接"泄漏"
					logger.Debug("recv timeout %v IsConnector %v", this.GetConnectionId(), this.IsConnector())
					return
				}
			}

		case <-heartBeatTimer.C:
			if this.isConnector && this.config.HeartBeatInterval > 0 && this.handler != nil {
				if heartBeatPacket := this.handler.CreateHeartBeatPacket(this); heartBeatPacket != nil {
					delaySendDecodePacketData = this.codec.Encode(this, heartBeatPacket)
					heartBeatTimer.Reset(time.Second * time.Duration(this.config.HeartBeatInterval))
				}
			}

		case <-this.readStopNotifyChan:
			logger.Debug("recv readStopNotify %v", this.GetConnectionId())
			return

		case <-ctx.Done():
			// 收到外部的关闭通知
			logger.Debug("recv closeNotify %v", this.GetConnectionId())
			return
		}

		if this.sendBuffer.UnReadLength() > 0 {
			// 可读数据有可能分别存在数组的尾部和头部,所以需要循环发送,有可能需要发送多次
			for this.isConnected && this.sendBuffer.UnReadLength() > 0 {
				if this.config.WriteTimeout > 0 {
					setTimeoutErr := this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout)*time.Second))
					// Q:什么情况会导致SetWriteDeadline返回err?
					if setTimeoutErr != nil {
						// ...
						logger.Debug("%v setTimeoutErr:%v", this.GetConnectionId(), setTimeoutErr.Error())
						return
					}
				}
				readBuffer := this.sendBuffer.ReadBuffer()
				//LogDebug("readBuffer:%v", readBuffer)
				//LogDebug("%v readBuffer:%v", this.GetConnectionId(), len(readBuffer))
				writeCount, err := this.conn.Write(readBuffer)
				if err != nil {
					// ...
					logger.Debug("%v write Err:%v", this.GetConnectionId(), err.Error())
					return
				}
				this.sendBuffer.SetReaded(writeCount)
				//LogDebug("%v send:%v unread:%v", this.GetConnectionId(), writeCount, sendBuffer.UnReadLength())
				if len(delaySendDecodePacketData) > 0 {
					writedLen,_ := this.sendBuffer.Write(delaySendDecodePacketData)
					// 这里不一定能全部写完
					if writedLen < len(delaySendDecodePacketData) {
						delaySendDecodePacketData = delaySendDecodePacketData[writedLen:]
						logger.Debug("%v write delaybuffer :%v", this.GetConnectionId(), writedLen)
					} else {
						delaySendDecodePacketData = nil
					}
				}
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
			logger.Debug("close %v %v", this.GetConnectionId(), this.IsConnector())
			//this.conn = nil
		}
		if this.handler != nil {
			this.handler.OnDisconnected(this)
		}
		if this.onClose != nil {
			this.onClose(this)
		}
	})
}

// 异步发送proto包
// NOTE:调用Send(command,message)之后,不要再对message进行读写!
func (this *TcpConnection) Send(command PacketCommand, message proto.Message) bool {
	if !this.isConnected {
		return false
	}
	packet := NewProtoPacket(command, message)
	// NOTE:当sendPacketCache满时,这里会阻塞
	this.sendPacketCache <- packet
	return true
}

// 异步发送数据
// NOTE:调用SendPacket(packet)之后,不要再对packet进行读写!
func (this *TcpConnection) SendPacket(packet Packet) bool {
	if !this.isConnected {
		return false
	}
	// NOTE:当sendPacketCache满时,这里会阻塞
	this.sendPacketCache <- packet
	return true
}

// 超时发包,超时未发送则丢弃,适用于某些允许丢弃的数据包
// 可以防止某些"不重要的"数据包造成chan阻塞,比如游戏项目常见的聊天广播
func (this *TcpConnection) TrySendPacket(packet Packet, timeout time.Duration) bool {
	if timeout == 0 {
		// 非阻塞方式写chan
		select {
		case this.sendPacketCache <- packet:
			return true
		default:
			return false
		}
	}
	sendTimeout := time.After(timeout)
	for {
		select {
		case this.sendPacketCache <- packet:
			return true
		case <-sendTimeout:
			return false
		}
	}
	return false
}

// 创建用于批量发包的RingBuffer
func (this *TcpConnection) createSendBuffer() *RingBuffer {
	ringBufferSize := this.config.SendBufferSize
	if ringBufferSize == 0 {
		if this.config.MaxPacketSize > 0 {
			ringBufferSize = this.config.MaxPacketSize*2
		} else {
			ringBufferSize = 65535
		}
	}
	return NewRingBuffer(int(ringBufferSize))
}

// 创建用于批量收包的RingBuffer
func (this *TcpConnection) createRecvBuffer() *RingBuffer {
	ringBufferSize := this.config.RecvBufferSize
	if ringBufferSize == 0 {
		if this.config.MaxPacketSize > 0 {
			ringBufferSize = this.config.MaxPacketSize*2
		} else {
			ringBufferSize = 65535
		}
	}
	return NewRingBuffer(int(ringBufferSize))
}

// 发包RingBuffer
func (this *TcpConnection) GetSendBuffer() *RingBuffer {
	return this.sendBuffer
}

// 收包RingBuffer
func (this *TcpConnection) GetRecvBuffer() *RingBuffer {
	return this.recvBuffer
}

// LocalAddr returns the local network address.
func (this *TcpConnection) LocalAddr() net.Addr {
	if this.conn == nil {
		return nil
	}
	return this.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (this *TcpConnection) RemoteAddr() net.Addr {
	if this.conn == nil {
		return nil
	}
	return this.conn.RemoteAddr()
}

func (this *TcpConnection) GetSendPacketChanLen() int {
	return len(this.sendPacketCache)
}
