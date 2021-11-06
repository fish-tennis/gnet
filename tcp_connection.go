package gnet

import (
	"io"
	"net"
	"sync"
	"time"
)

type TcpConnection struct {
	Connection
	conn net.Conn
	// 防止执行多次关闭操作
	closeOnce sync.Once
}

func NewTcpConnector(config ConnectionConfig, handler ConnectionHandler) *TcpConnection {
	return &TcpConnection{
		Connection : Connection{
			connectionId: newConnectionId(),
			isConnector: true,
			config: config,
			handler: handler,
			sendBuffer: &MessageBuffer{
				buffer: make(chan []byte, config.SendBufferSize),
			},
		},
	}
}

func NewTcpConnectionAccept(conn net.Conn, config ConnectionConfig, handler ConnectionHandler) *TcpConnection {
	return &TcpConnection{
		Connection : Connection{
			connectionId: newConnectionId(),
			isConnector: false,
			config: config,
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
	go func() {
		this.readLoop()
		this.Close()
	}()

	go func() {
		this.writeLoop(closeNotify)
		this.Close()
	}()
}

// 收包过程
func (this *TcpConnection) readLoop() {
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
		messageHeader.ReadFrom(messageHeaderData)
		if messageHeader.GetLen() == 0 {
			LogDebug("readLoop %v messageHeader len err", this.GetConnectionId())
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
		// TODO:根据messageHeader.GetFlags()的值对messageData进行处理
		if this.handler != nil {
			this.handler.OnRecvMessage(this, messageData)
		}
	}
	LogDebug("readLoop end %v", this.GetConnectionId())
}

// 发包过程
func (this *TcpConnection) writeLoop(closeNotify chan struct{}) {
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
			recvTimeoutTimer.Reset(time.Second * time.Duration(this.config.RecvTimeout))
		case <-closeNotify:
			// 收到外部的关闭通知
			LogDebug("recv closeNotify %v", this.GetConnectionId())
			goto END // break只能跳出select,无法跳出for,所以这里用了goto
		}
		// NOTE:目前方案是一个消息一个消息分别发送
		// 待对比方案:sendBuffer.buffer由chan改为ringbuffer,当前有多少数据待发送,就一次性发送(是否减少了系统调用的次数,从而提升了性能?)
		if len(messageData) > 0 {
			writeIndex := 0
			for this.isConnected && writeIndex < len(messageData) {
				setTimeoutErr := this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.config.WriteTimeout)*time.Second))
				// Q:什么情况会导致SetWriteDeadline返回err?
				if setTimeoutErr != nil {
					// ...
					LogDebug("%v setTimeoutErr:%v", this.GetConnectionId(), setTimeoutErr)
					break
				}
				writeCount, err := this.conn.Write(messageData[writeIndex:])
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
	})
}

// 发送数据
func (this *TcpConnection) Send(data []byte) bool {
	// TODO:对原始数据进行加工处理,如加密
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
