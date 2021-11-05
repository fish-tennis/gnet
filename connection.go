package gnet

// 连接
type Connection struct {
	// 发包缓存
	sendBuffer MessageBuffer
	isConnected bool
	handler ConnectionHandler
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
