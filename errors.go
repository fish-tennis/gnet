package gnet

import "errors"

var (
	ErrBufferFull = errors.New("buffer is full")
	ErrNotSupport = errors.New("not support")
	ErrPacketLength = errors.New("packet length error")
	// 数据包长度超出设置
	ErrPacketLengthExceed = errors.New("packet length exceed")
	ErrReadRemainPacket = errors.New("read remain packet data error")
)
