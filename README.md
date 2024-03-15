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
- Support Tcp,WebSocket(ws and wss)

## Core module

### Listener(https://github.com/fish-tennis/gnet/blob/main/listener.go)

Listen to a certain port, start a listening goroutine, and manage the connected connections

create a Listener:

```go
netMgr.NewListener("127.0.0.1:10001", connectionConfig, codec, &echoServerHandler{}, &echoListenerHandler{})
```

### Connection(https://github.com/fish-tennis/gnet/blob/main/connection.go)

There are two types of connection:

- connector(call Connect() to connect the server)
- accept by listener(the server call accept() to accept a new connection)

create a Connector:

```go
netMgr.NewConnector("127.0.0.1:10001", connectionConfig, codec, &echoClientHandler{}, nil)
```

### Packet(https://github.com/fish-tennis/gnet/blob/main/packet.go)

The common practices in game servers,the packet consists of a message number and a proto message,meanwhile gnet reserve a binary interface

### Encoding and decoding(https://github.com/fish-tennis/gnet/blob/main/codec.go)

gnet divide TCP stream based decoding into three layers

Layer1:subcontracting stream, format:|Length|Data|,after receiving full packet content, hand it over to the next layer for processing

Layer2:Decoding the data from Layer1,such as decryption,decompression,etc

Layer3:protobuf deserialize,generate proto.Message

![length & data](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/packet.png)

![encode](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/packet_encode.png)

![decode](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/packet_decode.png)

### Handler(https://github.com/fish-tennis/gnet/blob/main/handler.go)

ListenerHandler:when the server accept a new connection or the accepted connection disconnected

ConnectionHandler:when the connection connected,disconnected,receive packet

gnet provided a default ConnectionHandler:

```go
handler := NewDefaultConnectionHandler(codec)
// register packet and process function
handler.Register(123, OnTest, new(pb.TestMessage))
func OnTest(conn Connection, packet Packet) {
    testMessage := packet.Message().(*pb.TestMessage)
    // do something
}
```

### Use RingBuffer to increase performance

![ringbuffer-performance](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/ringbuffer-performance.png)

### goroutine

![connection_goroutine](https://github.com/fish-tennis/doc/blob/master/imgs/gnet/connection_goroutine.png)

## Examples

[echo with protobuf](https://github.com/fish-tennis/gnet/blob/main/echo_proto_test.go)

[echo without protobuf](https://github.com/fish-tennis/gnet/blob/main/echo_data_test.go)

[custom packet struct](https://github.com/fish-tennis/gnet/blob/main/custom_packet_no_ringbuffer_test.go)

[simulate performance testing of a simple game server scenario](https://github.com/fish-tennis/gnet/blob/main/server_test.go)

[tcp without ringbuffer](https://github.com/fish-tennis/gnet/blob/main/tcp_connection_simple_test.go)

[websocket](https://github.com/fish-tennis/gnet/blob/main/ws_connection_test.go)

## Project

[game db&cache framework](https://github.com/fish-tennis/gentity)

[distributed game server framework](https://github.com/fish-tennis/gserver)

gnet is also used in our commercial online game projects
