package gnet

type ConnectionHandler interface {
	OnConnected(connection IConnection, success bool)

	OnDisconnected(connection IConnection)

	OnRecvMessage(connection IConnection, data[]byte)
}

type ConnectorHandler interface {
	OnConnected(connection IConnection, success bool)

	OnDisconnected(connection IConnection)

	OnRecvMessage(connection IConnection, data[]byte)
}

type ListerHandler interface {
	OnNewConnection(connection *Connection)

	OnConnectionDisconnect(connection *Connection)
}