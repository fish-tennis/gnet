package gnet

// 连接回调
type ConnectionHandler interface {
	OnConnected(connection Connection, success bool)

	OnDisconnected(connection Connection)

	// 收到一个完整数据包
	// 在收包协程中调用
	OnRecvPacket(connection Connection, packet Packet)

	// 创建一个心跳包(对connector有效)
	// 在connector的发包协程中调用
	CreateHeartBeatPacket() Packet
}

// 监听回调
type ListenerHandler interface {
	// accept a new connection
	OnConnectionConnected(connection Connection)

	// a connection disconnect
	OnConnectionDisconnect(connection Connection)
}