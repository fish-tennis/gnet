package gnet

import (
	"net"
	"net/http"
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

	// CheckOrigin returns true if the request Origin header is acceptable. If
	// CheckOrigin is nil, then a safe default is used: return false if the
	// Origin request header is present and the origin host is not equal to
	// request Host header.
	//
	// A CheckOrigin function should carefully validate the request origin to
	// prevent cross-site request forgery.
	// NOTE:有些客户端会发送特定的Origin,如果使用默认的gorilla/websocket默认的checkSameOrigin会导致连接不上,比如ts版本的cocos
	CheckOrigin func(r *http.Request) bool // websocket专用
}

type baseListener struct {
	// unique listener id
	listenerId uint32

	config *ListenerConfig

	handler ListenerHandler
}

func (l *baseListener) GetListenerId() uint32 {
	return l.listenerId
}

func newListenerId() uint32 {
	return atomic.AddUint32(&_listenerIdCounter, 1)
}
