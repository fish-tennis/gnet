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
	lastRecvMessageTick uint32
}

func NewTcpConnector(config ConnectionConfig, codec Codec, handler ConnectionHandler) *TcpConnection {
	return &TcpConnection{
		baseConnection: baseConnection{
			connectionId: newConnectionId(),
			isConnector: true,
			config: config,
			codec: codec,
			handler: handler,
			sendBuffer: &MessageBuffer{
				buffer: make(chan []byte, config.SendBufferSize),
			},
		},
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
			sendBuffer: &MessageBuffer{
				buffer: make(chan []byte, config.SendBufferSize),
			},
		},
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
	messageHeaderData := make([]byte, MessageHeaderSize)
	messageHeader := &MessageHeader{}
	for this.isConnected {
		// TODO:目前方案是分别读取消息头和消息体,就算系统已经收到了多个包的数据,但是当前方案,依然需要多次io.ReadFull才能
		//把数据读出来
		// 待对比方案:系统当前有多少数据就读取多少,再自己进行分包(是否减少了系统调用的次数,从而提升了性能?)

		// 此处阻塞读,当连接关闭时,会中断阻塞,返回err
		_, err := io.ReadFull(this.conn, messageHeaderData)
		if err != nil {
			if err != io.EOF {
				// ...
			}
			LogDebug("readLoop %v err:%v", this.GetConnectionId(), err)
			break
		}
		decodeMessageHeaderData := messageHeaderData
		if this.codec != nil {
			// 解码包头
			decodeMessageHeaderData = this.codec.DecodeHeader(messageHeaderData)
			if decodeMessageHeaderData == nil {
				LogDebug("readLoop %v decode header err", this.GetConnectionId())
				break
			}
		}
		messageHeader.ReadFrom(decodeMessageHeaderData)
		if messageHeader.GetLen() == 0 {
			LogDebug("readLoop %v messageHeader len err", this.GetConnectionId())
			break
		}
		// 检查最大数据包限制
		if messageHeader.GetLen() >= MaxMessageDataSize {
			LogDebug("readLoop %v messageHeader len err:%v", this.GetConnectionId(), messageHeader.GetLen())
			break
		}
		// 检查自己设置的最大数据包限制
		if this.config.MaxMessageSize > 0 && messageHeader.GetLen() > 0 {
			LogDebug("readLoop %v messageHeader len err:%v>%v", this.GetConnectionId(), messageHeader.GetLen(), this.config.MaxMessageSize)
			break
		}
		messageData := make([]byte, messageHeader.GetLen())
		// 此处阻塞读,当连接关闭时,会中断阻塞,返回err
		_, dataErr := io.ReadFull(this.conn, messageData)
		if dataErr != nil {
			if dataErr != io.EOF {
				// ...
			}
			LogDebug("readLoop %v err:%v", this.GetConnectionId(), dataErr)
			break
		}
		decodeMessageData := messageData
		if this.codec != nil {
			// 解码包体
			decodeMessageData = this.codec.DecodeData(messageData)
		}
		// 最近收到完整数据包的时间
		// 有一种极端情况,网速太慢,即使没有掉线,也可能触发收包超时检测
		this.lastRecvMessageTick = GetCurrentTimeStamp()
		// TODO:根据messageHeader.GetFlags()的值对messageData进行处理
		if this.handler != nil {
			this.handler.OnRecvMessage(this, decodeMessageData)
		}
	}
	LogDebug("readLoop end %v", this.GetConnectionId())
}

// 发包过程
func (this *TcpConnection) writeLoop(closeNotify chan struct{}) {
	defer func() {
		if err := recover(); err != nil {
			LogDebug("writeLoop fatal %v: %v", this.GetConnectionId(), err.(error))
		}
	}()

	LogDebug("writeLoop begin %v", this.GetConnectionId())
	// 收包超时计时
	recvTimeoutTimer := time.NewTimer(time.Second * time.Duration(this.config.RecvTimeout))
	defer recvTimeoutTimer.Stop()
	var messageData []byte
	for this.isConnected {
		select {
		case messageData = <-this.sendBuffer.buffer:
			LogDebug("messageData %v len:%v", this.GetConnectionId(), len(messageData))
		case <-recvTimeoutTimer.C:
			nextTimeoutTime := this.config.RecvTimeout + this.lastRecvMessageTick - GetCurrentTimeStamp()
			if nextTimeoutTime > 0 {
				recvTimeoutTimer.Reset(time.Second * time.Duration(nextTimeoutTime))
			} else {
				// 指定时间内,一直未读取到数据包,则认为该连接掉线了,可能处于"假死"状态了
				// 需要主动关闭该连接,防止连接"泄漏"
				LogDebug("recv timeout %v", this.GetConnectionId())
				goto END
			}
		case <-closeNotify:
			// 收到外部的关闭通知
			LogDebug("recv closeNotify %v", this.GetConnectionId())
			goto END // break只能跳出select,无法跳出for,所以这里用了goto
		}
		// NOTE:目前方案是一个消息一个消息分别发送
		// 待对比方案:sendBuffer.buffer由chan改为ringbuffer,当前有多少数据待发送,就一次性发送(是否减少了系统调用的次数,从而提升了性能?)
		if len(messageData) > 0 {
			// 数据包编码
			decodeMessageData := messageData
			if this.codec != nil {
				decodeMessageData = this.codec.Encode(messageData)
			}
			//messageHeader := &MessageHeader{
			//	lenAndFlags: uint32(len(decodeMessageData)),
			//}
			//streamDataSize := len(decodeMessageData)+int(MessageHeaderSize)
			//streamData := make([]byte, streamDataSize, streamDataSize)
			//// 写入数据长度
			//messageHeader.WriteTo(streamData)
			//copy(streamData[MessageHeaderSize:], decodeMessageData)
			writeIndex := 0
			for this.isConnected && writeIndex < len(decodeMessageData) {
				setTimeoutErr := this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout)*time.Second))
				// Q:什么情况会导致SetWriteDeadline返回err?
				if setTimeoutErr != nil {
					// ...
					LogDebug("%v setTimeoutErr:%v", this.GetConnectionId(), setTimeoutErr)
					break
				}
				writeCount, err := this.conn.Write(decodeMessageData[writeIndex:])
				if err != nil {
					// ...
					LogDebug("%v write Err:%v", this.GetConnectionId(), err)
					break
				}
				writeIndex += writeCount
				LogDebug("%v write count:%v", this.GetConnectionId(), writeCount)
			}
		}
		messageData = nil
	}
END:
	LogDebug("writeLoop end %v", this.GetConnectionId())
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

// 发送数据
func (this *TcpConnection) Send(data []byte) bool {
	// TODO:对原始数据进行加工处理
	messageHeader := &MessageHeader{
		lenAndFlags: uint32(len(data)),
	}
	streamDataSize := len(data)+int(MessageHeaderSize)
	streamData := make([]byte, streamDataSize, streamDataSize)
	// 写入数据长度
	messageHeader.WriteTo(streamData)
	copy(streamData[MessageHeaderSize:], data)
	return this.sendBuffer.Write(streamData)
}
