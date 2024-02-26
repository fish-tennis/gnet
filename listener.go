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

type ListenerConfig struct {
	AcceptConfig            ConnectionConfig
	AcceptConnectionCreator AcceptConnectionCreator
	ListenerHandler         ListenerHandler
	// ws或wss的http监听路径,如"/ws"或"/wss"
	Path string
	// 签名cert文件,wss专用
	CertFile string
	// 签名key文件,wss专用
	KeyFile string
}

type baseListener struct {
	// unique listener id
	listenerId uint32

	config *ListenerConfig

	handler ListenerHandler
}

func (this *baseListener) GetListenerId() uint32 {
	return this.listenerId
}

func newListenerId() uint32 {
	return atomic.AddUint32(&_listenerIdCounter, 1)
}
