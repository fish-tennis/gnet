package example

import (
	"context"
	. "github.com/fish-tennis/gnet"
	"net/http"
	_ "net/http/pprof"
	"testing"
	"time"
)

func TestTimeout(t *testing.T) {
	defer func() {
		if err := recover(); err != nil {
			logger.Debug("fatal %v", err.(error))
			LogStack()
		}
	}()
	SetLogLevel(DebugLevel)

	// 1小时后触发关闭通知,所有监听<-ctx.Done()的地方会收到通知
	ctx,cancel := context.WithTimeout(context.Background(), time.Hour*1)
	defer cancel()

	go http.ListenAndServe(":10009", nil)

	netMgr := GetNetMgr()
	connectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		SendBufferSize:     60, // 设置的比较小,便于测试缓存写满的情况
		RecvBufferSize:     60,
		MaxPacketSize:      60,
		RecvTimeout:        3,
		HeartBeatInterval:  0,
		WriteTimeout:       0,
	}
	listenAddress := "127.0.0.1:10002"
	codec := NewDefaultCodec()
	netMgr.NewListener(ctx, listenAddress, connectionConfig, codec, &DefaultConnectionHandler{}, &echoListenerHandler{})
	time.Sleep(time.Second)

	connectorConnectionConfig := ConnectionConfig{
		SendPacketCacheCap: 100,
		SendBufferSize:     60,
		RecvBufferSize:     60,
		MaxPacketSize:      60,
		RecvTimeout:        0,
		HeartBeatInterval:  0,
		WriteTimeout:       0,
	}
	var connectors []Connection
	for i := 0; i < 10; i++ {
		conn := netMgr.NewConnector(ctx, listenAddress, &connectorConnectionConfig, codec, &DefaultConnectionHandler{}, nil)
		connectors = append(connectors, conn)
	}

	time.Sleep(time.Second)
	for _,conn := range connectors {
		conn.Close()
	}

	netMgr.Shutdown(true)
}