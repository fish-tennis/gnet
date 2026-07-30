package gnet

import (
	"math"
	"time"
)

var (
	// rpc等待回复的默认超时时间
	DefaultRpcTimeout = time.Second * 3
	// 发包写入sendPacketCache的默认超时时间
	DefaultSendTimeout = time.Second * 3
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

// 阻塞模式(TODO)
type blockOption struct{}

func (blockOption) apply(options *sendOptions) {
	options.block = true
}

func WithBlock() SendOption {
	return blockOption{}
}

// 消息满时丢弃
type discardOption struct{}

func (discardOption) apply(options *sendOptions) {
	options.discard = true
}

func WithDiscard() SendOption {
	return discardOption{}
}

// 永不超时
type infiniteTimeoutOption struct{}

func (infiniteTimeoutOption) apply(options *sendOptions) {
	options.timeout = math.MaxInt64
}

func WithInfiniteTimeout() SendOption {
	return infiniteTimeoutOption{}
}
