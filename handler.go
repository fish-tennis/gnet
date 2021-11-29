package gnet

// 连接回调
type ConnectionHandler interface {
	// 连接成功或失败
	OnConnected(connection Connection, success bool)

	// 断开连接
	OnDisconnected(connection Connection)

	// 收到一个完整数据包
	// 在收包协程中调用
	OnRecvPacket(connection Connection, packet Packet)

	// 创建一个心跳包(只对connector有效)
	// 在connector的发包协程中调用
	CreateHeartBeatPacket(connection Connection) Packet
}


// 监听回调
type ListenerHandler interface {
	// accept a new connection
	OnConnectionConnected(listener Listener, acceptedConnection Connection)

	// a connection disconnect
	OnConnectionDisconnect(listener Listener, connection Connection)
}


// ProtoPacket消息回调
type PacketHandler func(connection Connection, packet* ProtoPacket)

// ProtoPacket默认ConnectionHandler
type DefaultConnectionHandler struct {
	// 注册消息的处理函数map
	packetHandlers map[PacketCommand]PacketHandler
	// 未注册消息的处理函数
	unRegisterHandler PacketHandler
	// handler一般总是和codec配合使用
	protoCodec *ProtoCodec
	// 心跳包消息号(只对connector有效)
	heartBeatCommand PacketCommand
	// 心跳包构造函数(只对connector有效)
	heartBeatCreator ProtoMessageCreator
}

func (this *DefaultConnectionHandler) OnConnected(connection Connection, success bool) {
}

func (this *DefaultConnectionHandler) OnDisconnected(connection Connection) {
}

func (this *DefaultConnectionHandler) OnRecvPacket(connection Connection, packet Packet) {
	defer func() {
		if err := recover(); err != nil {
			LogError("fatal %v", err.(error))
			LogStack()
		}
	}()
	if protoPacket,ok := packet.(*ProtoPacket); ok {
		if packetHandler,ok2 := this.packetHandlers[protoPacket.command]; ok2 {
			if packetHandler != nil {
				packetHandler(connection, protoPacket)
				return
			}
		}
		if this.unRegisterHandler != nil {
			this.unRegisterHandler(connection, protoPacket)
		}
	}
}

func (this *DefaultConnectionHandler) CreateHeartBeatPacket(connection Connection) Packet {
	if this.heartBeatCreator != nil {
		return NewProtoPacket(this.heartBeatCommand, this.heartBeatCreator())
	}
	return nil
}

func NewDefaultConnectionHandler(protoCodec *ProtoCodec) *DefaultConnectionHandler {
	return &DefaultConnectionHandler{
		packetHandlers:make(map[PacketCommand]PacketHandler),
		protoCodec: protoCodec,
	}
}

// 注册消息号和消息回调,消息构造的映射
func (this *DefaultConnectionHandler) Register(packetCommand PacketCommand, handler PacketHandler, creator ProtoMessageCreator) {
	this.packetHandlers[packetCommand] = handler
	if this.protoCodec != nil && creator != nil {
		this.protoCodec.Register(packetCommand, creator)
	}
}

// 注册心跳包(只对connector有效)
func (this *DefaultConnectionHandler) RegisterHeartBeat(packetCommand PacketCommand, creator ProtoMessageCreator) {
	this.heartBeatCommand = packetCommand
	this.heartBeatCreator = creator
}

// 未注册消息的处理函数
func (this *DefaultConnectionHandler) SetUnRegisterHandler(unRegisterHandler PacketHandler) {
	this.unRegisterHandler = unRegisterHandler
}