package gnet

import (
	"log/slog"
	"runtime"
)

// gnet 内部使用的 logger,默认使用 slog.Default()
//
//	gnet internal logger, defaults to slog.Default()
//
// 注意: glog 在包初始化时取一次 slog.Default() 的值。
// 如果应用层在 main 中调用 slog.SetDefault() 修改全局默认 logger,
// gnet 的 glog 不会自动更新(持有的是旧实例引用)。
// 此时需要显式调用 gnet.SetLogger(slog.Default()) 让 gnet 使用新的默认 logger。
//
//	NOTE: glog captures slog.Default() once at package initialization.
//	If the application changes the global default via slog.SetDefault() in main,
//	gnet's glog won't follow automatically (it holds the old reference).
//	Call gnet.SetLogger(slog.Default()) explicitly to sync gnet with the new default.
var glog = slog.Default()

// SetLogger 设置 gnet 内部使用的 logger
//
// 应用层可通过传入不同级别或 handler 的 *slog.Logger,实现 gnet 与应用层日志相互独立。
//
// 例如: 应用层使用 Debug 级别,同时希望 gnet 使用 Info 级别:
//
//	func main() {
//	    // 应用层日志: Debug
//	    slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
//	        Level: slog.LevelDebug,
//	    })))
//	    // gnet 日志: Info (与应用层互不影响)
//	    gnet.SetLogger(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
//	        Level: slog.LevelInfo,
//	    })))
//	}
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
