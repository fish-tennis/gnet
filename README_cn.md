# gnet
[![Go Report Card](https://goreportcard.com/badge/github.com/fish-tennis/gnet)](https://goreportcard.com/report/github.com/fish-tennis/gnet)
[![Go Reference](https://pkg.go.dev/badge/github.com/fish-tennis/gnet.svg)](https://pkg.go.dev/github.com/fish-tennis/gnet)
[![codecov](https://codecov.io/gh/fish-tennis/gnet/branch/main/graph/badge.svg?token=RJ1C0OJAMK)](https://codecov.io/gh/fish-tennis/gnet)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge-flat.svg)](https://github.com/avelino/awesome-go#networking)

[English](https://github.com/fish-tennis/gnet/blob/main/README.md)

基于go语言开发的高性能网络库

## 功能

- 多线程,非阻塞
- 默认支持protobuf
- 使用无锁的RingBuffer优化收发包,针对游戏应用场景,性能可提高5倍
- 编解码接口易扩展
- rpc
- 目前支持Tcp,WebSocket(ws and wss)

## 使用
运行一个服务器
```go
codec := gnet.NewProtoCodec(nil)
handler := gnet.NewDefaultConnectionHandler(codec)
handler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessage, new(pb.TestMessage))
listenerConfig := &gnet.ListenerConfig{
    AcceptConfig: gnet.DefaultConnectionConfig,
}
listenerConfig.AcceptConfig.Codec = codec
listenerConfig.AcceptConfig.Handler = handler
gnet.GetNetMgr().NewListener(ctx, "localhost:10001", listenerConfig)
```

运行一个客户端
```go
codec := gnet.NewProtoCodec(nil)
handler := gnet.NewDefaultConnectionHandler(codec)
handler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), onTestMessage, new(pb.TestMessage))
connectionConfig := gnet.DefaultConnectionConfig
connectionConfig.Codec = clientCodec
connectionConfig.Handler = clientHandler
connector := gnet.GetNetMgr().NewConnector(ctx, "localhost:10001", &connectionConfig, nil)
connector.SendPacket(gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
    &pb.TestMessage{
        Name: "hello",
    }))
```

## 编解码Codec(https://github.com/fish-tennis/gnet/blob/main/codec.go)

gnet把基于TCP流的解码分成3层

第1层:对数据流进行分包,格式:|Length|Data|,在收到一个完整的数据包内容后,交给下一层处理

第2层:对数据包的流数据进行解码,如解密,解压缩等

第3层:对解码后的数据,进行protobuf反序列化,还原成proto.Message对象

![length & data](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/packet.png)

![encode](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/packet_encode.png)

![decode](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/packet_decode.png)

## 使用RingBuffer来提高性能

![ringbuffer-performance](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/ringbuffer-performance.png)

举例:在一个游戏地图中,你周围有很多其他玩家,其他玩家的数据更新需要同步给你,服务器会向你发送很多Packet,如果不使用RingBuffer机制, 就会每个Packet调用一次net.Conn.Write,而net.Conn.Write是系统调用,代价是比较高的,如TcpConnectionSimple

如果使用RingBuffer机制,就会在实际调用net.Conn.Write之前,对多个Packet进行合并,从而减少net.Conn.Write的调用次数,从而提高性能.

## rpc
gnet提供了类似rpc的接口,并不是标准的rpc方法调用,gnet使用消息号作为标识,而不是方法名,
向目标连接发送请求,并阻塞等待回复,本质类似于grpc-go
```go
request := gnet.NewProtoPacket(cmd, &pb.HelloRequest{
    Name: "hello",
})
reply := new(pb.HelloReply)
err := connection.Rpc(request, reply)
if err != nil {
    return
}
logger.Info("reply:%v", reply)
```

## go协程

![connection_goroutine](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/connection_goroutine.png)

## 示例
example/helloworld: protobuf数据包

example/data_packet: 非protobuf数据包

example/custom_packet: 扩展自定义数据包

example/tcp_connection_simple: 不使用RingBuffer

example/packet_size: 数据包长度允许大于RingBuffer

example/websocket: websocket

example/rpc: rpc调用

example/simulate_game: 模拟一个简单的游戏场景,对比使用RingBuffer的性能差异

## 客户端网络库 Connector Library
C#: [gnet_csharp](https://github.com/fish-tennis/gnet_csharp)

## 项目演示

[游戏实体接口gentity](https://github.com/fish-tennis/gentity)

[分布式游戏服务器框架gserver](https://github.com/fish-tennis/gserver)

## 讨论
QQ群: 764912827
