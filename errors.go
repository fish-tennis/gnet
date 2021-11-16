package gnet

import "errors"

var (
	ErrBufferFull = errors.New("buffer is full")
	ErrNotSupport = errors.New("not support")
)
