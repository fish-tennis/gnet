# gnet
[![Go Report Card](https://goreportcard.com/badge/github.com/fish-tennis/gnet)](https://goreportcard.com/report/github.com/fish-tennis/gnet)
[![Go Reference](https://pkg.go.dev/badge/github.com/fish-tennis/gnet.svg)](https://pkg.go.dev/github.com/fish-tennis/gnet)
[![codecov](https://codecov.io/gh/fish-tennis/gnet/branch/main/graph/badge.svg?token=RJ1C0OJAMK)](https://codecov.io/gh/fish-tennis/gnet)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge-flat.svg)](https://github.com/avelino/awesome-go#networking)

[中文说明](https://github.com/fish-tennis/gnet/blob/main/README_cn.md)

High performance network library,especially for game servers

## Features
- MultiThread, nonblocking
- Default support protobuf
- Optimize receiving and dispatching using lockless RingBuffer, which can improve performance by 5x for some cases
- Easy to implement custom encoding and decoding
- rpc
- Support Tcp,WebSocket(ws and wss)

## Usage
run a server
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

run a client
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

## Encoding and decoding(https://github.com/fish-tennis/gnet/blob/main/codec.go)

gnet divide TCP stream based decoding into three layers

Layer1:subcontracting stream, format:|Length|Data|,after receiving full packet content, hand it over to the next layer for processing

Layer2:Decoding the data from Layer1,such as decryption,decompression,etc

Layer3:protobuf deserialize,generate proto.Message

![length & data](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/packet.png)

![encode](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/packet_encode.png)

![decode](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/packet_decode.png)


## Use RingBuffer to increase performance

![ringbuffer-performance](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/ringbuffer-performance.png)

## rpc
Rpc send a request to target and block wait reply,similar to grpc-go,but gnet use command id instead of method name
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

## goroutine

![connection_goroutine](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/connection_goroutine.png)

## Examples
example/helloworld: a simple example use protobuf message

example/data_packet: a simple example use DataPacket

example/custom_packet: how to extend custom packet struct

example/tcp_connection_simple: use protobuf message without RingBuffer

example/packet_size: send big packet which size is bigger than RingBuffer's cap

example/websocket: a simple example use websocket

example/rpc: how to use rpc

example/simulate_game: a performance test with a game application scenario

## Client Connector Library
C#: [gnet_csharp](https://github.com/fish-tennis/gnet_csharp)

## Project

[game db&cache framework](https://github.com/fish-tennis/gentity)

[distributed game server framework](https://github.com/fish-tennis/gserver)

gnet is also used in our commercial online game projects
