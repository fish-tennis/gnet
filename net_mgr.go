package gnet

import "sync"

// 网络管理类,提供对外接口
type NetMgr struct {

	// 监听对象管理
	listenerMap map[uint32]IListener
	listenerMapLock sync.RWMutex

	// 连接对象管理
	connectorMap map[uint32]IConnection
	connectorMapLock sync.RWMutex

	// 关闭通知
	closeNotify chan struct{}
	// 初始化一次
	initOnce sync.Once
}

var (
	// singleton
	netMgr = &NetMgr{
	}
)

// 单例模式,在调用的时候才会执行初始化
func GetNetMgr() *NetMgr {
	netMgr.initOnce.Do(func() {
		netMgr.init()
	})
	return netMgr
}

// 初始化
func (this *NetMgr) init() {
	this.listenerMap = make(map[uint32]IListener)
	this.connectorMap = make(map[uint32]IConnection)
	this.closeNotify = make(chan struct{})
}

// 新监听对象
func (this *NetMgr) NewListener(address string, acceptConnectionConfig ConnectionConfig, acceptConnectionHandler ConnectionHandler) IListener {
	newListener := NewTcpListener(acceptConnectionConfig, acceptConnectionHandler)
	if !newListener.Start(address, this.closeNotify) {
		LogDebug("NewListener Start Failed")
		return nil
	}
	this.listenerMapLock.Lock()
	this.listenerMap[newListener.GetListenerId()] = newListener
	this.listenerMapLock.Unlock()
	return newListener
}

// 新连接对象
func (this *NetMgr) NewConnector(address string, connectionConfig ConnectionConfig, handler ConnectionHandler) IConnection {
	newConnector := NewTcpConnector(connectionConfig, handler)
	if !newConnector.Connect(address) {
		newConnector.Close()
		return nil
	}
	this.connectorMapLock.Lock()
	this.connectorMap[newConnector.GetConnectionId()] = newConnector
	this.connectorMapLock.Unlock()
	newConnector.Start(this.closeNotify)
	return newConnector
}

func (this *NetMgr) Shutdown() {
	// 触发关闭通知,所有select <-closeNotify的地方将收到通知
	close(this.closeNotify)
}