package gnet

import "sync/atomic"

type IConnection interface {
	GetConnectionId() uint32

	Send(data []byte) bool

	IsConnected() bool
}

// 连接
type Connection struct {
	// 连接唯一id
	connectionId uint32
	// 连接设置
	config ConnectionConfig
	// 发包缓存
	sendBuffer *MessageBuffer
	// 是否是连接方
	isConnector bool
	// 是否连接成功
	isConnected bool
	// 接口
	handler ConnectionHandler
}

// 连接设置
type ConnectionConfig struct {
	// 发包缓存大小
	SendBufferSize int
	// 收包超时设置(秒)
	RecvTimeout int
	// 发包超时设置(秒)
	WriteTimeout int
}

// 连接唯一id
func (this *Connection) GetConnectionId() uint32 {
	return this.connectionId
}

// 发送数据
func (this *Connection) Send(data []byte) bool {
	return this.sendBuffer.Write(data)
}

// 开启读写协程
func (this *Connection) Start() {

}

// 是否连接成功
func (this *Connection) IsConnected() bool {
	return this.isConnected
}

// 关闭
func (this *Connection) Close() {
}

var (
	connectionIdCounter uint32 = 0
)

func newConnectionId() uint32 {
	return atomic.AddUint32(&connectionIdCounter, 1)
}