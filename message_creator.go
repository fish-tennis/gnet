package gnet

import (
	"google.golang.org/protobuf/proto"
	"reflect"
	"sync"
)

var (
	_messagePool = make(map[reflect.Type]*sync.Pool)
)

// proto.Message ctor func
type ProtoMessageCreator func() proto.Message

// Packet ctor func
type PacketCreator func() Packet

type ProtoRegister interface {
	Register(command PacketCommand, protoMessage proto.Message)
}

type messageCreatorInfo struct {
	typ  reflect.Type
	pool *sync.Pool
}

type MessageCreatorMap struct {
	messageCreators map[PacketCommand]*messageCreatorInfo
	usePool         bool
}

func NewMessageCreatorMap(usePool bool) *MessageCreatorMap {
	return &MessageCreatorMap{
		messageCreators: make(map[PacketCommand]*messageCreatorInfo),
		usePool:         usePool,
	}
}

func (m *MessageCreatorMap) Register(command PacketCommand, protoMessage proto.Message) {
	if protoMessage == nil {
		// 可以只注册消息号
		m.messageCreators[command] = nil
		return
	}
	typ := reflect.TypeOf(protoMessage).Elem()
	var pool *sync.Pool
	if m.usePool {
		var ok bool
		pool, ok = _messagePool[typ]
		if !ok {
			pool = &sync.Pool{
				New: func() any {
					return reflect.New(typ).Interface().(proto.Message)
				},
			}
			_messagePool[typ] = pool
		}
	}
	m.messageCreators[command] = &messageCreatorInfo{
		typ:  typ,
		pool: pool,
	}
}

func (m *MessageCreatorMap) NewMessage(command PacketCommand) (proto.Message, *sync.Pool, bool) {
	if creatorInfo, ok := m.messageCreators[command]; ok {
		if creatorInfo == nil {
			// 只注册了消息号的情况
			return nil, nil, true
		}
		if m.usePool {
			return creatorInfo.pool.Get().(proto.Message), creatorInfo.pool, true
		}
		return reflect.New(creatorInfo.typ).Interface().(proto.Message), nil, true
	}
	return nil, nil, false
}
