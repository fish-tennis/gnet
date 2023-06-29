package gnet

import (
	"net"
	"sync/atomic"
)

var (
	_listenerIdCounter uint32 = 0
)

// interface for Listener
type Listener interface {
	GetListenerId() uint32

	GetConnection(connectionId uint32) Connection

	// 广播消息
	//  broadcast packet to accepted connections
	Broadcast(packet Packet)

	// Addr returns the listener's network address.
	Addr() net.Addr

	Close()
}

type baseListener struct {
	// unique listener id
	listenerId uint32

	handler ListenerHandler
}

func (this *baseListener) GetListenerId() uint32 {
	return this.listenerId
}

func newListenerId() uint32 {
	return atomic.AddUint32(&_listenerIdCounter, 1)
}
