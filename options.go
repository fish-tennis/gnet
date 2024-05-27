package gnet

import "time"

var (
	// rpc默认超时时间
	DefaultRpcTimeout = time.Second * 3

	defaultSendOptions = sendOptions{}
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
	timeout time.Duration
}

func (o TimeoutOption) apply(options *sendOptions) {
	options.timeout = o.timeout
}

func Timeout(timeout time.Duration) SendOption {
	return TimeoutOption{
		timeout: timeout,
	}
}

type BlockOption struct {
}

func (o BlockOption) apply(options *sendOptions) {
	options.block = true
}

func Block() SendOption {
	return BlockOption{}
}

// discard when sendPacketChan is full
type DiscardOption struct {
}

func (o DiscardOption) apply(options *sendOptions) {
	options.discard = true
}

func Discard() DiscardOption {
	return DiscardOption{}
}
