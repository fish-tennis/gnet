package gnet

type ConnectionHandler interface {
	OnConnected(success bool)

	OnDisconnected()

	OnRecvMessage(data[]byte)
}

type ConnectorHandler interface {
	OnConnected(success bool)

	OnDisconnected()

	OnRecvMessage(data[]byte)
}

type ListerHandler interface {
	OnNewConnection(connection *Connection)

	OnConnectionDisconnect(connection *Connection)
}