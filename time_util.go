package gnet

import "time"

// 获取当前时间戳(秒)
func GetCurrentTimeStamp() uint32 {
	return uint32(time.Now().UnixNano()/int64(time.Second))
}
