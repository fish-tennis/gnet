package gnet

import (
	"sync"
	"sync/atomic"
)

var (
	_rpcCallSerialId = uint32(0)
)

type rpcCall struct {
	// unique id of every rpc call
	id    uint32
	reply chan Packet
}

// manage the pending rpcCall map
type rpcCalls struct {
	rpcCallMutex sync.Mutex
	rpcCalls     map[uint32]*rpcCall
}

func newRpcCalls() *rpcCalls {
	return &rpcCalls{
		rpcCalls: make(map[uint32]*rpcCall),
	}
}

func (this *rpcCalls) newRpcCall() *rpcCall {
	call := &rpcCall{
		id:    atomic.AddUint32(&_rpcCallSerialId, 1),
		reply: make(chan Packet),
	}
	if call.id == 0 {
		call.id = atomic.AddUint32(&_rpcCallSerialId, 1)
	}
	this.rpcCallMutex.Lock()
	this.rpcCalls[call.id] = call
	this.rpcCallMutex.Unlock()
	return call
}

func (this *rpcCalls) putReply(replyPacket Packet) bool {
	if rpcCallIdSetter, ok := replyPacket.(RpcCallIdSetter); ok && rpcCallIdSetter.RpcCallId() > 0 {
		this.rpcCallMutex.Lock()
		call, exist := this.rpcCalls[rpcCallIdSetter.RpcCallId()]
		if exist {
			delete(this.rpcCalls, rpcCallIdSetter.RpcCallId())
		}
		this.rpcCallMutex.Unlock()
		if !exist {
			return false
		}
		call.reply <- replyPacket
		return true
	}
	return false
}

func (this *rpcCalls) removeReply(rpcCallId uint32) {
	this.rpcCallMutex.Lock()
	defer this.rpcCallMutex.Unlock()
	delete(this.rpcCalls, rpcCallId)
}
