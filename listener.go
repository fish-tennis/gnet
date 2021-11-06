package gnet

import "sync/atomic"

type IListener interface {
	GetListenerId() uint32
}

// 监听
type Listener struct {
	listenerId uint32
}

func (this *Listener) GetListenerId() uint32 {
	return this.listenerId
}

var (
	listenerIdCounter uint32 = 0
)

func newListenerId() uint32 {
	return atomic.AddUint32(&listenerIdCounter, 1)
}