package gnet

import (
	"google.golang.org/protobuf/proto"
	"sync/atomic"
)

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
	// NOTE: PacketHandlers 非并发安全,必须在连接Start之前完成所有Register/RegisterCreator,
	//        readLoop启动后(即首包到达时)会通过frozen标记冻结,之后Register变为无效(静默return)
	PacketHandlers map[PacketCommand]PacketHandler
	// 标记是否已冻结,冻结后Register/RegisterCreator不再生效
	frozen int32 // 0:可修改 1:已冻结
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
			logger.Error("fatal %v", err)
			LogStack()
		}
	}()
	// 首次收包时冻结,此后不允许再Register
	//
	// 设计说明: PacketHandlers map的并发读写由应用层保证——
	// 业务层必须确保在Connection.Start()之前完成所有Register调用,
	// 不要在readLoop启动(即首次收到数据包)后再Register。
	// frozen只是"尽力而为"的标记,不保证线程安全,不增加额外的锁开销。
	atomic.StoreInt32(&h.frozen, 1)
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
// 设计说明: Register和OnRecvPacket对PacketHandlers map的读写无锁保护,
// 由应用层确保在Connection.Start()之前完成所有Register调用。
// readLoop启动后frozen标记变为1,Register将静默无效。
//
//	register PacketCommand,PacketHandler,proto.Message
func (h *DefaultConnectionHandler) Register(packetCommand PacketCommand, handler PacketHandler, protoMessage proto.Message) {
	if atomic.LoadInt32(&h.frozen) != 0 {
		return
	}
	h.PacketHandlers[packetCommand] = handler
	if h.protoCodec != nil {
		if protoRegister, ok := h.protoCodec.(ProtoRegister); ok {
			protoRegister.Register(packetCommand, protoMessage)
		}
	}
}

// 注册消息号、消息回调和消息工厂函数,完全无反射
// NOTE: 仅在初始化阶段(连接Start之前)调用,readLoop启动后会无效
//
//	register PacketCommand,PacketHandler,ProtoMessageCreator
func (h *DefaultConnectionHandler) RegisterCreator(packetCommand PacketCommand, handler PacketHandler, creator ProtoMessageCreator) {
	if atomic.LoadInt32(&h.frozen) != 0 {
		return
	}
	h.PacketHandlers[packetCommand] = handler
	if h.protoCodec != nil {
		if protoCreatorRegister, ok := h.protoCodec.(ProtoCreatorRegister); ok {
			protoCreatorRegister.RegisterCreator(packetCommand, creator)
		}
	}
}

// RegisterHandler 泛型辅助函数,以类型参数指定消息类型,编译期生成工厂函数,消除反射
// 用法: gnet.RegisterHandler[pb.TestMessage](handler, cmd, onTestMessage)
// NOTE: 仅在初始化阶段(连接Start之前)调用
//
//	generic helper to register PacketCommand,PacketHandler with zero reflection
func RegisterHandler[T any](h *DefaultConnectionHandler, packetCommand PacketCommand, handler PacketHandler) {
	h.RegisterCreator(packetCommand, handler, func() proto.Message {
		var m T
		return any(&m).(proto.Message)
	})
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
