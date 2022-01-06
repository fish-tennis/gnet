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
)

type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}

type StdLogger struct {
	std *log.Logger
	callDepth int
}

func (s *StdLogger) Debug(format string, args ...interface{}) {
	if logLevel > DebugLevel {
		return
	}
	s.std.Output(s.callDepth, "[D] "+fmt.Sprintf(format, args...))
}

func (s *StdLogger) Info(format string, args ...interface{}) {
	if logLevel > InfoLevel {
		return
	}
	s.std.Output(s.callDepth, "[I] "+fmt.Sprintf(format, args...))
}

func (s *StdLogger) Warn(format string, args ...interface{}) {
	if logLevel > WarnLevel {
		return
	}
	s.std.Output(s.callDepth, "[W] "+fmt.Sprintf(format, args...))
}

func (s *StdLogger) Error(format string, args ...interface{}) {
	if logLevel > ErrorLevel {
		return
	}
	s.std.Output(s.callDepth, "[E] "+fmt.Sprintf(format, args...))
}

func NewStdLogger(callDepth int) Logger {
	return &StdLogger{
		std: log.New(os.Stderr, "", log.LstdFlags | log.Llongfile),
		callDepth: callDepth,
	}
}

var (
	// 默认使用系统库的log接口
	logger = NewStdLogger(2)
	// 默认InfoLevel
	logLevel = InfoLevel
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