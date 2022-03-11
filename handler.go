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

type PacketHandlerRegister interface {
	Register(packetCommand PacketCommand, handler PacketHandler, creator ProtoMessageCreator)
}


// ProtoPacket消息回调
type PacketHandler func(connection Connection, packet* ProtoPacket)

// ProtoPacket默认ConnectionHandler
type DefaultConnectionHandler struct {
	// 注册消息的处理函数map
	PacketHandlers map[PacketCommand]PacketHandler
	// 未注册消息的处理函数
	UnRegisterHandler PacketHandler
	// 连接回调
	onConnectedFunc func(connection Connection, success bool)
	onDisconnectedFunc func(connection Connection)
	// handler一般总是和codec配合使用
	protoCodec Codec
	// 心跳包消息号(只对connector有效)
	heartBeatCommand PacketCommand
	// 心跳包构造函数(只对connector有效)
	heartBeatCreator ProtoMessageCreator
}

func (this *DefaultConnectionHandler) OnConnected(connection Connection, success bool) {
	if this.onConnectedFunc != nil {
		this.onConnectedFunc(connection, success)
	}
}

func (this *DefaultConnectionHandler) OnDisconnected(connection Connection) {
	if this.onDisconnectedFunc != nil {
		this.onDisconnectedFunc(connection)
	}
}

func (this *DefaultConnectionHandler) OnRecvPacket(connection Connection, packet Packet) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("fatal %v", err.(error))
			LogStack()
		}
	}()
	if protoPacket,ok := packet.(*ProtoPacket); ok {
		if packetHandler,ok2 := this.PacketHandlers[protoPacket.command]; ok2 {
			if packetHandler != nil {
				packetHandler(connection, protoPacket)
				return
			}
		}
		if this.UnRegisterHandler != nil {
			this.UnRegisterHandler(connection, protoPacket)
		}
	}
}

func (this *DefaultConnectionHandler) CreateHeartBeatPacket(connection Connection) Packet {
	if this.heartBeatCreator != nil {
		return NewProtoPacket(this.heartBeatCommand, this.heartBeatCreator())
	}
	return nil
}

func NewDefaultConnectionHandler(protoCodec Codec) *DefaultConnectionHandler {
	return &DefaultConnectionHandler{
		PacketHandlers: make(map[PacketCommand]PacketHandler),
		protoCodec:     protoCodec,
	}
}

func (this *DefaultConnectionHandler) GetCodec() Codec {
	return this.protoCodec
}

// 注册消息号和消息回调,消息构造的映射
// handler在TcpConnection的read协程中被调用
func (this *DefaultConnectionHandler) Register(packetCommand PacketCommand, handler PacketHandler, creator ProtoMessageCreator) {
	this.PacketHandlers[packetCommand] = handler
	if this.protoCodec != nil && creator != nil {
		if protoRegister,ok := this.protoCodec.(ProtoRegister); ok {
			protoRegister.Register(packetCommand, creator)
		}
	}
}

func (this *DefaultConnectionHandler) GetPacketHandler(packetCommand PacketCommand) PacketHandler {
	return this.PacketHandlers[packetCommand]
}

// 注册心跳包(只对connector有效)
func (this *DefaultConnectionHandler) RegisterHeartBeat(packetCommand PacketCommand, creator ProtoMessageCreator) {
	this.heartBeatCommand = packetCommand
	this.heartBeatCreator = creator
}

// 未注册消息的处理函数
// unRegisterHandler在TcpConnection的read协程中被调用
func (this *DefaultConnectionHandler) SetUnRegisterHandler(unRegisterHandler PacketHandler) {
	this.UnRegisterHandler = unRegisterHandler
}

// 设置连接回调
func (this *DefaultConnectionHandler) SetOnConnectedFunc(onConnectedFunc func(connection Connection, success bool)) {
	this.onConnectedFunc = onConnectedFunc
}

// 设置连接断开回调
func (this *DefaultConnectionHandler) SetOnDisconnectedFunc(onDisconnectedFunc func(connection Connection)) {
	this.onDisconnectedFunc = onDisconnectedFunc
}