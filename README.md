# gnet
基于go语言开发的游戏网络库

## 设计理念
- 代码尽可能的简单易读
- 接口尽可能的简单
- 封装层次尽可能的少
- 主要针对游戏,不做大而全的设计,不打算做成通用网络库
- 针对游戏应用场景优化,高性能
- 容易扩展

## 功能
- 多线程,非阻塞,异步发消息
- 简单易用的API
- 默认支持protobuf
- 使用无锁的环形buffer减少内存分配和拷贝的次数,以优化性能
- TCP数据流分包,进行批量合并,以优化性能
- 编解码接口易扩展

## 核心模块
### 监听Listener(https://github.com/fish-tennis/gnet/blob/main/listener.go)
监听某个端口,开启一个监听协程,并管理监听到的连接
创建一个Listener:
```go
netMgr.NewListener("127.0.0.1:10001", connectionConfig, codec, &echoServerHandler{}, &echoListenerHandler{})
```
### 连接Connection(https://github.com/fish-tennis/gnet/blob/main/connection.go)
对连接的封装,连接有2种:
- 一种是发起连接的一方(调用connect连接服务器的一方)
- 一种是Listener监听到的连接(Listener通过accept监听到的连接)
创建一个Connector:
```go
netMgr.NewConnector("127.0.0.1:10001", connectionConfig, codec, &echoClientHandler{})
```
### 数据包Packet(https://github.com/fish-tennis/gnet/blob/main/packet.go)
游戏行业的常规做法,数据包由消息号和proto消息构成,同时预留一个二进制数据的接口(不使用proto消息的应用可以使用该接口,如示例[不使用proto的echo](https://github.com/fish-tennis/gnet/blob/main/example/echo_data_test.go))
### 编解码Codec(https://github.com/fish-tennis/gnet/blob/main/codec.go)
gnet把基于TCP流的解码分成3层
第1层:对数据流进行分包,格式:|Length|Data|,在收到一个完整的数据包内容后,交给下一层处理
第2层:对数据包的流数据进行解码,如解密,解压缩等
第3层:对解码后的数据,进行protobuf反序列化,还原成proto.Message对象
### 应用层接口Handler(https://github.com/fish-tennis/gnet/blob/main/handler.go)
ListenerHandler:当监听到新连接和连接断开时,提供回调接口
ConnectionHandler:在连接成功或失败,连接断开,收到数据包时,提供回调接口
应用层的逻辑主要处理OnRecvPacket接口
```go
func (e *echoClientHandler) OnRecvPacket(connection Connection, packet Packet) {
	connection.Send(packet)
}
```

## 示例
[使用proto的echo](https://github.com/fish-tennis/gnet/blob/main/example/echo_proto_test.go)

[不使用proto的echo](https://github.com/fish-tennis/gnet/blob/main/example/echo_data_test.go)

[模拟一个简单的游戏应用场景的性能测试](https://github.com/fish-tennis/gnet/blob/main/example/server_test.go)

