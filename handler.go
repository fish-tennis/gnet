package gnet

type ConnectionHandler interface {
	OnConnected(connection Connection, success bool)

	OnDisconnected(connection Connection)

	OnRecvPacket(connection Connection, packet *Packet)
}

//type ConnectorHandler interface {
//	OnConnected(connection Connection, success bool)
//
//	OnDisconnected(connection Connection)
//
//	OnRecvPacket(connection Connection, packet *Packet)
//}

type ListenerHandler interface {
	OnConnectionConnected(connection Connection)

	OnConnectionDisconnect(connection Connection)
}