package gnet

import (
	"fmt"
	"log"
	"os"
	"runtime"
)

// 日志级别,参考zap
const (
	DebugLevel int8 = iota-1
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}

type stdLogger struct {
	std *log.Logger
}

func (s *stdLogger) Debug(format string, args ...interface{}) {
	if logLevel > DebugLevel {
		return
	}
	s.std.Output(2, "[D] "+fmt.Sprintf(format, args...))
}

func (s *stdLogger) Info(format string, args ...interface{}) {
	if logLevel > InfoLevel {
		return
	}
	s.std.Output(2, "[I] "+fmt.Sprintf(format, args...))
}

func (s *stdLogger) Warn(format string, args ...interface{}) {
	if logLevel > WarnLevel {
		return
	}
	s.std.Output(2, "[W] "+fmt.Sprintf(format, args...))
}

func (s *stdLogger) Error(format string, args ...interface{}) {
	if logLevel > ErrorLevel {
		return
	}
	s.std.Output(2, "[E] "+fmt.Sprintf(format, args...))
}

func newStdLogger() Logger {
	return &stdLogger{
		std: log.New(os.Stderr, "", log.LstdFlags | log.Llongfile),
	}
}

var (
	// 默认使用系统库的log接口
	logger = newStdLogger()
	// 默认InfoLevel
	logLevel = int8(0)
)

func GetLogger() Logger {
	return logger
}

func SetLogger(w Logger, level int8) {
	logger = w
	logLevel = level
}

func SetLogLevel(level int8) {
	logLevel = level
}

func LogStack() {
	buf := make([]byte, 1<<12)
	logger.Error(string(buf[:runtime.Stack(buf, false)]))
}