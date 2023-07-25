package gnet

import (
	"context"
	"net"
	"sync"
)

var (
	// singleton
	netMgr = &NetMgr{}
)

// 网络管理类,提供对外接口
//
//	manager class
type NetMgr struct {
	listenerMap     map[uint32]Listener
	listenerMapLock sync.RWMutex

	connectorMap     map[uint32]Connection
	connectorMapLock sync.RWMutex

	initOnce sync.Once
	wg       sync.WaitGroup
}

// 单例模式,在调用的时候才会执行初始化一次
//
//	singleton mode, init once
func GetNetMgr() *NetMgr {
	netMgr.initOnce.Do(func() {
		netMgr.init()
	})
	return netMgr
}

func (this *NetMgr) init() {
	this.listenerMap = make(map[uint32]Listener)
	this.connectorMap = make(map[uint32]Connection)
	this.wg = sync.WaitGroup{}
}

// create a new TcpListener
func (this *NetMgr) NewListener(ctx context.Context, address string, acceptConnectionConfig ConnectionConfig, acceptConnectionCodec Codec,
	acceptConnectionHandler ConnectionHandler, listenerHandler ListenerHandler) Listener {
	return this.NewListenerCustom(ctx, address, acceptConnectionConfig, acceptConnectionCodec,
		acceptConnectionHandler, listenerHandler, func(_conn net.Conn, _config *ConnectionConfig, _codec Codec, _handler ConnectionHandler) Connection {
			return NewTcpConnectionAccept(_conn, _config, _codec, _handler)
		})
}

// create a new Listener, with custom acceptConnectionCreator
func (this *NetMgr) NewListenerCustom(ctx context.Context, address string, acceptConnectionConfig ConnectionConfig, acceptConnectionCodec Codec,
	acceptConnectionHandler ConnectionHandler, listenerHandler ListenerHandler, acceptConnectionCreator AcceptConnectionCreator) Listener {
	newListener := NewTcpListener(acceptConnectionConfig, acceptConnectionCodec, acceptConnectionHandler, listenerHandler)
	newListener.acceptConnectionCreator = acceptConnectionCreator
	newListener.netMgrWg = &this.wg
	if !newListener.Start(ctx, address) {
		logger.Debug("NewListener Start Failed")
		return nil
	}
	this.listenerMapLock.Lock()
	this.listenerMap[newListener.GetListenerId()] = newListener
	this.listenerMapLock.Unlock()

	newListener.onClose = func(listener Listener) {
		this.listenerMapLock.Lock()
		delete(this.listenerMap, listener.GetListenerId())
		this.listenerMapLock.Unlock()
	}
	return newListener
}

// create a new TcpConnection
func (this *NetMgr) NewConnector(ctx context.Context, address string, connectionConfig *ConnectionConfig,
	codec Codec, handler ConnectionHandler, tag interface{}) Connection {
	return this.NewConnectorCustom(ctx, address, connectionConfig, codec, handler, tag, func(_config *ConnectionConfig, _codec Codec, _handler ConnectionHandler) Connection {
		return NewTcpConnector(_config, _codec, _handler)
	})
}

// create a new Connection, with custom connectionCreator
func (this *NetMgr) NewConnectorCustom(ctx context.Context, address string, connectionConfig *ConnectionConfig,
	codec Codec, handler ConnectionHandler, tag interface{}, connectionCreator ConnectionCreator) Connection {
	newConnector := connectionCreator(connectionConfig, codec, handler)
	newConnector.SetTag(tag)
	if !newConnector.Connect(address) {
		newConnector.Close()
		return nil
	}
	this.connectorMapLock.Lock()
	this.connectorMap[newConnector.GetConnectionId()] = newConnector
	this.connectorMapLock.Unlock()
	newConnector.Start(ctx, &this.wg, func(connection Connection) {
		this.connectorMapLock.Lock()
		delete(this.connectorMap, connection.GetConnectionId())
		this.connectorMapLock.Unlock()
	})
	return newConnector
}

// waitForAllNetGoroutine:是否阻塞等待所有网络协程结束
//
//	waitForAllNetGoroutine: wait blocks until all goroutine end
func (this *NetMgr) Shutdown(waitForAllNetGoroutine bool) {
	logger.Debug("Shutdown %v", waitForAllNetGoroutine)
	if waitForAllNetGoroutine {
		// 等待所有网络协程结束
		this.wg.Wait()
		logger.Debug("all net goroutine closed")
	}
}
