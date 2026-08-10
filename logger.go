package gnet

import (
	"log/slog"
	"runtime"
)

// gnet 内部使用的 logger,默认使用 slog.Default()
//
//	gnet internal logger, defaults to slog.Default()
var glog = slog.Default()

// SetLogger 设置 gnet 内部使用的 logger
//
// 应用层可通过传入不同级别或 handler 的 *slog.Logger,实现 gnet 与应用层日志相互独立。
//
//	Set the logger used internally by gnet.
//	Callers can pass a *slog.Logger with a different level/handler so that
//	gnet's logging is independent from the application's logging.
func SetLogger(l *slog.Logger) {
	if l != nil {
		glog = l
	}
}

// LogStack 打印当前 goroutine 的堆栈信息
func LogStack() {
	buf := make([]byte, 1<<12)
	glog.Error(string(buf[:runtime.Stack(buf, false)]))
}
