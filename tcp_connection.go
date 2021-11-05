package gnet

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
)

type TcpConnection struct {
	*Connection
	conn net.Conn
	// 防止执行多次关闭操作
	closeOnce sync.Once
}

func NewTcpConnector(sendBufferSize,recvTimeout,writeTimeout int, handler ConnectionHandler) *TcpConnection {
	return &TcpConnection{
		Connection : &Connection{
			isConnector: true,
			recvTimeout: recvTimeout,
			writeTimeout: writeTimeout,
			handler: handler,
			sendBuffer: &MessageBuffer{
				buffer: make(chan []byte, sendBufferSize),
			},
		},
	}
}

func NewTcpConnectionAccept(conn net.Conn, config ConnectionConfig, handler ConnectionHandler) *TcpConnection {
	return &TcpConnection{
		Connection : &Connection{
			isConnector: false,
			config: config,
			handler: handler,
			sendBuffer: &MessageBuffer{
				buffer: make(chan []byte, config.sendBufferSize),
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
	if err == nil {
		this.isConnected = false
		if this.handler != nil {
			this.handler.OnConnected(false)
		}
		return false
	}
	this.conn = conn
	this.isConnected = true
	if this.handler != nil {
		this.handler.OnConnected(true)
	}
	return true
}

// 开启读写协程
func (this *TcpConnection) Start() {
	go func() {
		this.readLoop()
		this.Close()
	}()

	go func() {
		this.writeLoop()
		this.Close()
	}()
}

// 收包过程
func (this *TcpConnection) readLoop() {
	messageHeaderData := make([]byte, 4)
	messageHeader := &MessageHeader{}
	for this.isConnected {
		// TODO:目前方案是分别读取消息头和消息体,就算系统已经收到了多个包的数据,但是当前方案,依然需要多次io.ReadFull才能
		//把数据读出来
		// 待对比方案:系统当前有多少数据就读取多少,再自己进行分包(是否减少了系统调用的次数,从而提升了性能?)
		_, err := io.ReadFull(this.conn, messageHeaderData)
		if err != nil {
			if err != io.EOF {
				// ...
			}
			break
		}
		messageHeader.lenAndFlags = binary.LittleEndian.Uint32(messageHeaderData)
		if messageHeader.GetLen() == 0 {
			break
		}
		messageData := make([]byte, messageHeader.GetLen())
		_, dataErr := io.ReadFull(this.conn, messageData)
		if dataErr != nil {
			if dataErr != io.EOF {
				// ...
			}
			break
		}
		// TODO:根据messageHeader.GetFlags()的值对messageData进行处理
		if this.handler != nil {
			this.handler.OnRecvMessage(messageData)
		}
	}
}

// 发包过程
func (this *TcpConnection) writeLoop() {
	// 收包超时计时
	recvTimeoutTimer := time.NewTimer(time.Second * time.Duration(this.recvTimeout))
	defer recvTimeoutTimer.Stop()
	var messageData []byte
	for this.isConnected {
		select {
		case messageData = <-this.sendBuffer.buffer:
		case <-recvTimeoutTimer.C:
			recvTimeoutTimer.Reset(time.Second * time.Duration(this.recvTimeout))
		}
		// NOTE:目前方案是一个消息一个消息分别发送
		// 待对比方案:sendBuffer.buffer由chan改为ringbuffer,当前有多少数据待发送,就一次性发送(是否减少了系统调用的次数,从而提升了性能?)
		if len(messageData) > 0 {
			writeIndex := 0
			for this.isConnected && writeIndex < len(messageData) {
				setTimeoutErr := this.conn.SetWriteDeadline(time.Now().Add(time.Duration(this.writeTimeout)*time.Second))
				// Q:什么情况会导致SetWriteDeadline返回err?
				if setTimeoutErr != nil {
					// ...
					break
				}
				writeCount, err := this.conn.Write(messageData[writeIndex:])
				if err != nil {
					// ...
					break
				}
				writeIndex += writeCount
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
			//this.conn = nil
		}
	})
}
