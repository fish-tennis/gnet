package gnet

import (
	"fmt"
	"runtime"
	"time"
)

// 日志写接口
type ILogWriter interface {
	Write(str string)
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
	logWriter ILogWriter = &ConsoleLogWriter{}
)

func SetDefaultLogWriter(w ILogWriter) {
	logWriter = w
}

func LogDebug(v ...interface{}) {
	level := "D"
	var prefix string
	_, file, line, ok := runtime.Caller(1)
	if ok {
		prefix = fmt.Sprintf("[%s][%s][%s:%d]:", level, time.Now().Format("2006-01-02 15:04:05"), file, line)
	} else {
		prefix = fmt.Sprintf("[%s][%s]:", level, time.Now().Format("2006-01-02 15:04:05"))
	}
	if len(v) > 1 {
		logWriter.Write(prefix + fmt.Sprintf(v[0].(string), v[1:]...))
	} else {
		logWriter.Write(prefix + fmt.Sprint(v[0]))
	}
}

func LogStack() {
	buf := make([]byte, 1<<12)
	LogDebug(string(buf[:runtime.Stack(buf, false)]))
}