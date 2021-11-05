package gnet

// 消息缓存
type MessageBuffer struct {
	// TODO:待改为ringbuffer
	buffer chan []byte
}

// 写入数据
func (this *MessageBuffer) Write(data []byte) bool {
	this.buffer <- data
	return true
}
