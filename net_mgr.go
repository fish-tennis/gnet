package gnet

import (
	"context"
	"sync"
)

// 网络管理类,提供对外接口
type NetMgr struct {

	// 监听对象管理
	listenerMap map[uint32]Listener
	listenerMapLock sync.RWMutex

	// 连接对象管理
	connectorMap map[uint32]Connection
	connectorMapLock sync.RWMutex

	// 初始化一次
	initOnce sync.Once
	// 管理协程的关闭
	wg sync.WaitGroup
}

var (
	// singleton
	netMgr = &NetMgr{
	}
)

// 单例模式,在调用的时候才会执行初始化一次
func GetNetMgr() *NetMgr {
	netMgr.initOnce.Do(func() {
		netMgr.init()
	})
	return netMgr
}

// 初始化
func (this *NetMgr) init() {
	this.listenerMap = make(map[uint32]Listener)
	this.connectorMap = make(map[uint32]Connection)
	this.wg = sync.WaitGroup{}
}

// 新监听对象
func (this *NetMgr) NewListener(ctx context.Context, address string, acceptConnectionConfig ConnectionConfig, acceptConnectionCodec Codec,
	acceptConnectionHandler ConnectionHandler, listenerHandler ListenerHandler) Listener {
	newListener := NewTcpListener(acceptConnectionConfig, acceptConnectionCodec, acceptConnectionHandler, listenerHandler)
	newListener.netMgrWg = &this.wg
	if !newListener.Start(ctx, address) {
		LogDebug("NewListener Start Failed")
		return nil
	}
	this.listenerMapLock.Lock()
	this.listenerMap[newListener.GetListenerId()] = newListener
	this.listenerMapLock.Unlock()

	newListener.onClose = func(listener Listener) {
		this.listenerMapLock.Lock()
		delete(this.listenerMap, newListener.GetListenerId())
		this.listenerMapLock.Unlock()
	}
	return newListener
}

// 新连接对象
func (this *NetMgr) NewConnector(ctx context.Context, address string, connectionConfig ConnectionConfig, codec Codec, handler ConnectionHandler, tag interface{}) Connection {
	newConnector := NewTcpConnector(connectionConfig, codec, handler)
	newConnector.netMgrWg = &this.wg
	newConnector.SetTag(tag)
	if !newConnector.Connect(address) {
		newConnector.Close()
		return nil
	}
	this.connectorMapLock.Lock()
	this.connectorMap[newConnector.GetConnectionId()] = newConnector
	this.connectorMapLock.Unlock()
	newConnector.Start(ctx)

	newConnector.onClose = func(connection Connection) {
		this.connectorMapLock.Lock()
		delete(this.connectorMap, connection.GetConnectionId())
		this.connectorMapLock.Unlock()
	}
	return newConnector
}

// 关闭
// waitForAllNetGoroutine:是否阻塞等待所有网络协程结束
func (this *NetMgr) Shutdown(waitForAllNetGoroutine bool) {
	if waitForAllNetGoroutine {
		// 等待所有网络协程结束
		this.wg.Wait()
		LogDebug("all net goroutine closed")
	}
}
