package gnet

// 消息缓存
type MessageBuffer struct {
	buffer chan []byte
}

func (this *MessageBuffer) Write(data []byte) bool {
	this.buffer <- data
	return true
}
