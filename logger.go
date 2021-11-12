package gnet

import (
	"fmt"
	"runtime"
	"time"
)

// 日志级别
type LogLevel int8

const (
	DebugLevel LogLevel = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// 日志写接口
type LogWriter interface {
	Write(str string)
}

// 日志
type Logger struct {
	writer LogWriter
	level LogLevel
}

func (this *Logger) write(levelStr,format string, args ...interface{}) {
	var prefix string
	// call LogDebug
	// LogDebug
	// Logger.Debug
	// Logger.write
	_, file, line, ok := runtime.Caller(3)
	if ok {
		prefix = fmt.Sprintf("[%s][%s][%s:%d]:", levelStr, time.Now().Format("2006-01-02 15:04:05"), file, line)
	} else {
		prefix = fmt.Sprintf("[%s][%s]:", levelStr, time.Now().Format("2006-01-02 15:04:05"))
	}
	if len(args) > 0 {
		this.writer.Write(prefix + fmt.Sprintf(format, args...))
	} else {
		this.writer.Write(prefix + format)
	}
}

func (this *Logger) Debug(format string, args ...interface{}) {
	if this.level >= DebugLevel {
		this.write("D", format, args...)
	}
}

func (this *Logger) Info(format string, args ...interface{}) {
	if this.level >= InfoLevel {
		this.write("I", format, args...)
	}
}

func (this *Logger) Warn(format string, args ...interface{}) {
	if this.level >= WarnLevel {
		this.write("W", format, args...)
	}
}

func (this *Logger) Error(format string, args ...interface{}) {
	if this.level >= ErrorLevel {
		this.write("E", format, args...)
	}
}

func (this *Logger) Fatal(format string, args ...interface{}) {
	if this.level >= FatalLevel {
		this.write("F", format, args...)
	}
}

// 控制台输出日志
type ConsoleLogWriter struct {
}

func (this *ConsoleLogWriter) Write(str string) {
	fmt.Println(str)
}

// 什么都不处理,可用于关闭日志
type NoneLogWriter struct {
}

func (this *NoneLogWriter) Write(str string) {
}

var (
	// 单例
	logger = &Logger{
		writer: &ConsoleLogWriter{},
		level: DebugLevel,
	}
)

func SetLogWriter(w LogWriter) {
	logger.writer = w
}

func SetLogLevel(level LogLevel) {
	logger.level = level
}

func LogDebug(format string, args ...interface{}) {
	logger.Debug(format, args...)
}

func LogInfo(format string, args ...interface{}) {
	logger.Info(format, args...)
}

func LogWarn(format string, args ...interface{}) {
	logger.Warn(format, args...)
}

func LogError(format string, args ...interface{}) {
	logger.Error(format, args...)
}

func LogFatal(format string, args ...interface{}) {
	logger.Fatal(format, args...)
}

func LogStack() {
	buf := make([]byte, 1<<12)
	logger.Error(string(buf[:runtime.Stack(buf, false)]))
}