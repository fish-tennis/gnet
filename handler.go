package gnet

import "google.golang.org/protobuf/proto"

// handler for Connection
type ConnectionHandler interface {
	// 连接成功或失败
	//  after connect
	OnConnected(connection Connection, success bool)

	// 断开连接
	//  when disconnected
	OnDisconnected(connection Connection)

	// 收到一个完整数据包
	// 在收包协程中调用
	//  after recv a full packet, calling in the read goroutine
	OnRecvPacket(connection Connection, packet Packet)

	// 创建一个心跳包(只对connector有效)
	// 在connector的发包协程中调用
	//  generate a heartbeat packet, calling int the connector's write goroutine
	CreateHeartBeatPacket(connection Connection) Packet
}

// handler for Listener
type ListenerHandler interface {
	// accept a new connection
	OnConnectionConnected(listener Listener, acceptedConnection Connection)

	// a connection disconnect
	OnConnectionDisconnect(listener Listener, connection Connection)
}

type PacketHandlerRegister interface {
	Register(packetCommand PacketCommand, handler PacketHandler, protoMessage proto.Message)
}

// handler for Packet
type PacketHandler func(connection Connection, packet Packet)

// default ConnectionHandler for Proto
type DefaultConnectionHandler struct {
	// 注册消息的处理函数map
	//  registered map of PacketCommand and PacketHandler
	PacketHandlers map[PacketCommand]PacketHandler
	// 未注册消息的处理函数
	//  packetHandler for unregistered PacketCommand
	UnRegisterHandler PacketHandler
	// connected callback
	onConnectedFunc func(connection Connection, success bool)
	// disconnected callback
	onDisconnectedFunc func(connection Connection)
	// handler一般总是和codec配合使用
	protoCodec Codec
	// 心跳包消息号(只对connector有效)
	//  PacketCommand for heartBeat packet (only valid for connector)
	heartBeatCommand PacketCommand
	// 心跳包构造函数(只对connector有效)
	//  heartBeat packet generator(only valid for connector)
	heartBeatCreator ProtoMessageCreator
	// 心跳包构造函数(只对connector有效)
	//  heartBeat packet generator(only valid for connector)
	heartBeatPacketCreator PacketCreator
}

func (h *DefaultConnectionHandler) OnConnected(connection Connection, success bool) {
	if h.onConnectedFunc != nil {
		h.onConnectedFunc(connection, success)
	}
}

func (h *DefaultConnectionHandler) OnDisconnected(connection Connection) {
	if h.onDisconnectedFunc != nil {
		h.onDisconnectedFunc(connection)
	}
}

func (h *DefaultConnectionHandler) OnRecvPacket(connection Connection, packet Packet) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("fatal %v", err.(error))
			LogStack()
		}
	}()
	if packetHandler, ok := h.PacketHandlers[packet.Command()]; ok {
		if packetHandler != nil {
			packetHandler(connection, packet)
			return
		}
	}
	if h.UnRegisterHandler != nil {
		h.UnRegisterHandler(connection, packet)
	}
}

func (h *DefaultConnectionHandler) CreateHeartBeatPacket(connection Connection) Packet {
	if h.heartBeatPacketCreator != nil {
		return h.heartBeatPacketCreator()
	}
	return nil
}

func NewDefaultConnectionHandler(protoCodec Codec) *DefaultConnectionHandler {
	return &DefaultConnectionHandler{
		PacketHandlers: make(map[PacketCommand]PacketHandler),
		protoCodec:     protoCodec,
	}
}

func (h *DefaultConnectionHandler) GetCodec() Codec {
	return h.protoCodec
}

// 注册消息号和消息回调,proto.Message的映射
// handler在TcpConnection的read协程中被调用
//
//	register PacketCommand,PacketHandler,proto.Message
func (h *DefaultConnectionHandler) Register(packetCommand PacketCommand, handler PacketHandler, protoMessage proto.Message) {
	h.PacketHandlers[packetCommand] = handler
	if h.protoCodec != nil {
		if protoRegister, ok := h.protoCodec.(ProtoRegister); ok {
			protoRegister.Register(packetCommand, protoMessage)
		}
	}
}

func (h *DefaultConnectionHandler) GetPacketHandler(packetCommand PacketCommand) PacketHandler {
	return h.PacketHandlers[packetCommand]
}

// 注册心跳包(只对connector有效)
//
//	register heartBeatPacketCreator, only valid for connector
func (h *DefaultConnectionHandler) RegisterHeartBeat(heartBeatPacketCreator PacketCreator) {
	h.heartBeatPacketCreator = heartBeatPacketCreator
}

// 未注册消息的处理函数
// unRegisterHandler在TcpConnection的read协程中被调用
//
//	register the PacketHandler for unRegister PacketCommand
func (h *DefaultConnectionHandler) SetUnRegisterHandler(unRegisterHandler PacketHandler) {
	h.UnRegisterHandler = unRegisterHandler
}

// set connected callback
func (h *DefaultConnectionHandler) SetOnConnectedFunc(onConnectedFunc func(connection Connection, success bool)) {
	h.onConnectedFunc = onConnectedFunc
}

// set disconnected callback
func (h *DefaultConnectionHandler) SetOnDisconnectedFunc(onDisconnectedFunc func(connection Connection)) {
	h.onDisconnectedFunc = onDisconnectedFunc
}
