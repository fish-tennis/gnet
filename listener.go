package gnet

import "sync/atomic"

// 监听接口定义
type Listener interface {
	GetListenerId() uint32
}

// 监听
type baseListener struct {
	listenerId uint32

	handler ListenerHandler
}

func (this *baseListener) GetListenerId() uint32 {
	return this.listenerId
}

var (
	listenerIdCounter uint32 = 0
)

func newListenerId() uint32 {
	return atomic.AddUint32(&listenerIdCounter, 1)
}