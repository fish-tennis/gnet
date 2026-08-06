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

func (c *rpcCalls) newRpcCall() *rpcCall {
	call := &rpcCall{
		id: atomic.AddUint32(&_rpcCallSerialId, 1),
		// 使用带1个缓冲的channel,防止readLoop在putReply时,
		// 因调用方已超时退出而不再读取channel,导致readLoop永久阻塞
		reply: make(chan Packet, 1),
	}
	if call.id == 0 {
		call.id = atomic.AddUint32(&_rpcCallSerialId, 1)
	}
	c.rpcCallMutex.Lock()
	c.rpcCalls[call.id] = call
	c.rpcCallMutex.Unlock()
	return call
}

// putReply 尝试将回复包匹配到等待中的RPC调用
// 返回true表示匹配成功(回复已投递),返回false表示不是RPC回复或调用已不存在
//
// 注意: RPC请求和RPC回复都带有rpcCallId>0。当putReply返回false时,
// 该包可能是: (1)非RPC包 (2)RPC请求(服务端收到) (3)过期的RPC回复
// 三种情况都交给OnRecvPacket处理,由业务层自行判断。
func (c *rpcCalls) putReply(replyPacket Packet) bool {
	if replyPacket.RpcCallId() > 0 {
		c.rpcCallMutex.Lock()
		call, exist := c.rpcCalls[replyPacket.RpcCallId()]
		if exist {
			delete(c.rpcCalls, replyPacket.RpcCallId())
		}
		c.rpcCallMutex.Unlock()
		if !exist {
			return false
		}
		call.reply <- replyPacket
		return true
	}
	return false
}

func (c *rpcCalls) removeReply(rpcCallId uint32) {
	c.rpcCallMutex.Lock()
	defer c.rpcCallMutex.Unlock()
	delete(c.rpcCalls, rpcCallId)
}
