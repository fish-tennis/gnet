package gnet

import "time"

// 获取当前时间戳(秒)
func GetCurrentTimeStamp() int64 {
	return time.Now().UnixNano() / int64(time.Second)
}
