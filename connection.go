package gnet

// 连接
type Connection struct {
	// 连接唯一id
	connectionId uint
	// 连接设置
	config ConnectionConfig
	// 发包缓存
	sendBuffer *MessageBuffer
	// 是否是连接方
	isConnector bool
	// 是否连接成功
	isConnected bool
	// 收包超时设置(秒)
	recvTimeout int
	// 发包超时设置(秒)
	writeTimeout int
	// 接口
	handler ConnectionHandler
}

// 连接设置
type ConnectionConfig struct {
	// 发包缓存大小
	sendBufferSize int
	// 收包超时设置(秒)
	recvTimeout int
	// 发包超时设置(秒)
	writeTimeout int
}

// 连接唯一id
func (this *Connection) GetConnectionId() uint {
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
