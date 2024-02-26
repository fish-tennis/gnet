package gnet

import (
	"context"
	"encoding/binary"
	"fmt"
	"google.golang.org/protobuf/proto"
	"net"
	"testing"
	"time"
	"unsafe"
)

// show how to use custom packet without RingBuffer
//
//	演示如何自定义消息头
//	这里不使用RingBuffer
func TestCustomPacketNoRingBuffer(t *testing.T) {
	defer func() {
		if err := recover(); err != nil {
			logger.Debug("fatal %v", err.(error))
			LogStack()
		}
	}()

	SetLogLevel(DebugLevel)
	// 10秒后触发关闭通知,所有监听<-ctx.Done()的地方会收到通知
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	netMgr := GetNetMgr()
	serverCodec := &customCodec{}
	serverHandler := &echoCustomPacketServerHandler{}
	connectionConfig := ConnectionConfig{
		SendPacketCacheCap: 16,
		MaxPacketSize:      1024 * 1024 * 32, // 支持超出DefaultPacketHeaderSize大小的包
		HeartBeatInterval:  3,
		RecvTimeout:        0,
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"

	// 自定义TcpConnection
	listenerConfig := &ListenerConfig{
		AcceptConfig: connectionConfig,
		AcceptConnectionCreator: func(conn net.Conn, config *ConnectionConfig) Connection {
			return NewTcpConnectionSimpleAccept(conn, config)
		},
	}
	listenerConfig.AcceptConfig.Codec = serverCodec
	listenerConfig.AcceptConfig.Handler = serverHandler
	if netMgr.NewListener(ctx, listenAddress, listenerConfig) == nil {
		panic("listen failed")
	}
	time.Sleep(time.Second)

	connectionConfig.Codec = &customCodec{}
	connectionConfig.Handler = &echoCustomPacketClientHandler{}
	// 自定义TcpConnection
	if netMgr.NewConnectorCustom(ctx, listenAddress, &connectionConfig, nil, func(config *ConnectionConfig) Connection {
		return NewTcpConnectionSimple(config)
	}) == nil {
		panic("connect failed")
	}

	netMgr.Shutdown(true)
}

// 自定义包头
// implement of PacketHeader
type customPacketHeader struct {
	len     uint32 // 支持更大的Packet
	command uint16 // 消息号
	flags   uint16 // 预留标记
}

// 包体长度,不包含包头的长度
// [0,0xFFFFFFFF]
func (this *customPacketHeader) Len() uint32 {
	return this.len
}

// 消息号
func (this *customPacketHeader) Command() uint16 {
	return this.command
}

// 标记
func (this *customPacketHeader) Flags() uint16 {
	return this.flags
}

// 从字节流读取数据,len(messageHeaderData)>=MessageHeaderSize
// 使用小端字节序
func (this *customPacketHeader) ReadFrom(packetHeaderData []byte) {
	this.len = binary.LittleEndian.Uint32(packetHeaderData)
	this.command = binary.LittleEndian.Uint16(packetHeaderData[4:])
	this.flags = binary.LittleEndian.Uint16(packetHeaderData[6:])
}

// 写入字节流,使用小端字节序
func (this *customPacketHeader) WriteTo(packetHeaderData []byte) {
	binary.LittleEndian.PutUint32(packetHeaderData, this.len)
	binary.LittleEndian.PutUint16(packetHeaderData[4:], this.command)
	binary.LittleEndian.PutUint16(packetHeaderData[6:], this.flags)
}

// 包含一个消息号和[]byte的数据包
type customDataPacket struct {
	command uint16
	data    []byte
}

func newCustomDataPacket(command uint16, data []byte) *customDataPacket {
	return &customDataPacket{
		command: command,
		data:    data,
	}
}

func (this *customDataPacket) Command() PacketCommand {
	return PacketCommand(this.command)
}

func (this *customDataPacket) Message() proto.Message {
	return nil
}

func (this *customDataPacket) GetStreamData() []byte {
	return this.data
}

// deep copy
func (this *customDataPacket) Clone() Packet {
	newPacket := &customDataPacket{data: make([]byte, len(this.data))}
	newPacket.command = this.command
	copy(newPacket.data, this.data)
	return newPacket
}

// 自定义编解码
type customCodec struct {
}

// 使用CustomPacketHeader
func (this *customCodec) CreatePacketHeader(connection Connection, packet Packet, packetData []byte) PacketHeader {
	if packet == nil {
		return &customPacketHeader{
			len: uint32(len(packetData)),
		}
	}
	return &customPacketHeader{
		len:     uint32(len(packetData)),
		command: uint16(packet.Command()),
	}
}

func (this *customCodec) PacketHeaderSize() uint32 {
	return uint32(int(unsafe.Sizeof(customPacketHeader{})))
}

// 这里直接返回原包的字节流数据
// 实际业务可以在此进行编码,如加密,压缩等
func (this *customCodec) Encode(connection Connection, packet Packet) []byte {
	return packet.GetStreamData()
}

// 这里的data是完整的包数据,包含了包头
func (this *customCodec) Decode(connection Connection, data []byte) (newPacket Packet, err error) {
	packetHeader := &customPacketHeader{}
	packetHeader.ReadFrom(data[0:])
	newPacket = &customDataPacket{
		command: packetHeader.Command(),
		data:    data[this.PacketHeaderSize():],
	}
	return
}

// 服务端监听到的连接接口
type echoCustomPacketServerHandler struct {
}

func (e *echoCustomPacketServerHandler) OnConnected(connection Connection, success bool) {
	logger.Debug(fmt.Sprintf("Server OnConnected %v %v", connection.GetConnectionId(), success))
	if success {
		// 开一个协程,服务器自动给客户端发消息
		serialId := 0
		packetDataSize := 1024 * 1024 * 30
		go func() {
			autoSendTimer := time.NewTimer(time.Second)
			for connection.IsConnected() {
				select {
				case <-autoSendTimer.C:
					serialId++
					// 模拟一个30M的包
					packetData := make([]byte, packetDataSize, packetDataSize)
					for j := 0; j < len(packetData); j++ {
						packetData[j] = byte(j)
					}
					packet := newCustomDataPacket(2, packetData)
					connection.SendPacket(packet)
					autoSendTimer.Reset(time.Second)
				}
			}
		}()
	}
}

func (e *echoCustomPacketServerHandler) OnDisconnected(connection Connection) {
	logger.Debug(fmt.Sprintf("Server OnDisconnected %v", connection.GetConnectionId()))
}

func (e *echoCustomPacketServerHandler) OnRecvPacket(connection Connection, packet Packet) {
	if len(packet.GetStreamData()) < 100 {
		logger.Debug(fmt.Sprintf("Server OnRecvPacket %v: %v", connection.GetConnectionId(), string(packet.GetStreamData())))
	} else {
		logger.Debug(fmt.Sprintf("Server OnRecvPacket %v: len:%v", connection.GetConnectionId(), len(packet.GetStreamData())))
	}
}

// 服务器不需要发送心跳请求包
func (e *echoCustomPacketServerHandler) CreateHeartBeatPacket(connection Connection) Packet {
	return nil
}

// 客户端连接接口
type echoCustomPacketClientHandler struct {
	echoCount int
}

func (e *echoCustomPacketClientHandler) OnConnected(connection Connection, success bool) {
	logger.Debug(fmt.Sprintf("Client OnConnected %v %v", connection.GetConnectionId(), success))
}

func (e *echoCustomPacketClientHandler) OnDisconnected(connection Connection) {
	logger.Debug(fmt.Sprintf("Client OnDisconnected %v", connection.GetConnectionId()))
}

func (e *echoCustomPacketClientHandler) OnRecvPacket(connection Connection, packet Packet) {
	if len(packet.GetStreamData()) < 100 {
		logger.Debug(fmt.Sprintf("Client OnRecvPacket %v: %v", connection.GetConnectionId(), string(packet.GetStreamData())))
	} else {
		logger.Debug(fmt.Sprintf("Client OnRecvPacket %v: len:%v", connection.GetConnectionId(), len(packet.GetStreamData())))
	}
	e.echoCount++
	// 模拟一个回复包
	echoPacket := newCustomDataPacket(3, []byte(fmt.Sprintf("hello server %v", e.echoCount)))
	connection.SendPacket(echoPacket)
}

// 客户端定时发送心跳请求包
func (e *echoCustomPacketClientHandler) CreateHeartBeatPacket(connection Connection) Packet {
	return newCustomDataPacket(1, []byte("heartbeat"))
}
