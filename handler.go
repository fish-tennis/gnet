package gnet

type ConnectionHandler interface {
	OnConnected(connection Connection, success bool)

	OnDisconnected(connection Connection)

	OnRecvMessage(connection Connection, data[]byte)
}

//type ConnectorHandler interface {
//	OnConnected(connection Connection, success bool)
//
//	OnDisconnected(connection Connection)
//
//	OnRecvMessage(connection Connection, data[]byte)
//}

type ListenerHandler interface {
	OnConnectionConnected(connection Connection)

	OnConnectionDisconnect(connection Connection)
}