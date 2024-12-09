package gnet

import (
	"context"
	"net"
	"sync"
)

var (
	// singleton
	_netMgr = &NetMgr{}
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
	_netMgr.initOnce.Do(func() {
		_netMgr.init()
	})
	return _netMgr
}

func (m *NetMgr) init() {
	m.listenerMap = make(map[uint32]Listener)
	m.connectorMap = make(map[uint32]Connection)
	m.wg = sync.WaitGroup{}
}

func (m *NetMgr) NewListener(ctx context.Context, address string, listenerConfig *ListenerConfig) Listener {
	if listenerConfig.AcceptConnectionCreator == nil {
		listenerConfig.AcceptConnectionCreator = func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionAccept(conn, config)
		}
	}
	newListener := NewTcpListener(listenerConfig)
	newListener.netMgrWg = &m.wg
	if !newListener.Start(ctx, address) {
		logger.Error("NewListener Start Failed")
		return nil
	}
	m.listenerMapLock.Lock()
	m.listenerMap[newListener.GetListenerId()] = newListener
	m.listenerMapLock.Unlock()

	newListener.onClose = func(listener Listener) {
		m.listenerMapLock.Lock()
		delete(m.listenerMap, listener.GetListenerId())
		m.listenerMapLock.Unlock()
	}
	return newListener
}

func (m *NetMgr) NewWsListener(ctx context.Context, address string, listenerConfig *ListenerConfig) Listener {
	newListener := NewWsListener(listenerConfig)
	newListener.netMgrWg = &m.wg
	if !newListener.Start(ctx, address) {
		logger.Error("NewWsListener Start Failed")
		return nil
	}
	m.listenerMapLock.Lock()
	m.listenerMap[newListener.GetListenerId()] = newListener
	m.listenerMapLock.Unlock()

	newListener.onClose = func(listener Listener) {
		m.listenerMapLock.Lock()
		delete(m.listenerMap, listener.GetListenerId())
		m.listenerMapLock.Unlock()
	}
	return newListener
}

// create a new TcpConnection
func (m *NetMgr) NewConnector(ctx context.Context, address string, connectionConfig *ConnectionConfig,
	tag interface{}) Connection {
	return m.NewConnectorCustom(ctx, address, connectionConfig, tag, func(_config *ConnectionConfig) Connection {
		return NewTcpConnector(_config)
	})
}

func (m *NetMgr) NewWsConnector(ctx context.Context, address string, connectionConfig *ConnectionConfig,
	tag interface{}) Connection {
	return m.NewConnectorCustom(ctx, address, connectionConfig, tag, func(_config *ConnectionConfig) Connection {
		return NewWsConnection(_config)
	})
}

// create a new Connection, with custom connectionCreator
func (m *NetMgr) NewConnectorCustom(ctx context.Context, address string, connectionConfig *ConnectionConfig,
	tag interface{}, connectionCreator ConnectionCreator) Connection {
	newConnector := connectionCreator(connectionConfig)
	newConnector.SetTag(tag)
	if !newConnector.Connect(address) {
		newConnector.Close()
		return nil
	}
	m.connectorMapLock.Lock()
	m.connectorMap[newConnector.GetConnectionId()] = newConnector
	m.connectorMapLock.Unlock()
	newConnector.Start(ctx, &m.wg, func(connection Connection) {
		m.connectorMapLock.Lock()
		delete(m.connectorMap, connection.GetConnectionId())
		m.connectorMapLock.Unlock()
	})
	return newConnector
}

// waitForAllNetGoroutine:是否阻塞等待所有网络协程结束
//
//	waitForAllNetGoroutine: wait blocks until all goroutine end
func (m *NetMgr) Shutdown(waitForAllNetGoroutine bool) {
	logger.Debug("Shutdown %v", waitForAllNetGoroutine)
	if waitForAllNetGoroutine {
		// 等待所有网络协程结束
		m.wg.Wait()
		logger.Debug("all net goroutine closed")
	}
}
