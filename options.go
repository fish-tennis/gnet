package gnet

import (
	"math"
	"time"
)

var (
	// rpc默认超时时间
	DefaultRpcTimeout = time.Second * 3
)

// option for Connection.Send
type SendOption interface {
	apply(*sendOptions)
}

// options for Connection.Send
type sendOptions struct {
	// 调用rpc接口时,0表示使用默认值defaultRpcTimeout,<0表示永不超时
	timeout time.Duration
	// TODO:
	block bool
	// discard when sendPacketChan full
	discard bool
}

func defaultSendOptions() *sendOptions {
	return &sendOptions{
		timeout: DefaultRpcTimeout,
	}
}

type TimeoutOption struct {
	Timeout time.Duration
}

func (o TimeoutOption) apply(options *sendOptions) {
	options.timeout = o.Timeout
}

func Timeout(timeout time.Duration) SendOption {
	return TimeoutOption{
		Timeout: timeout,
	}
}

// funcSendOption wraps a function that modifies sendOptions into an
// implementation of the SendOption interface.
type funcSendOption struct {
	f func(*sendOptions)
}

func (fso *funcSendOption) apply(so *sendOptions) {
	fso.f(so)
}

func newFuncSendOption(f func(*sendOptions)) *funcSendOption {
	return &funcSendOption{
		f: f,
	}
}

// 阻塞模式(TODO)
func WithBlock() SendOption {
	return newFuncSendOption(func(options *sendOptions) {
		options.block = true
	})
}

// 消息满时丢弃
func WithDiscard() SendOption {
	return newFuncSendOption(func(options *sendOptions) {
		options.discard = true
	})
}

// 永不超时
func WithInfiniteTimeout() SendOption {
	return newFuncSendOption(func(options *sendOptions) {
		options.timeout = math.MaxInt64
	})
}
