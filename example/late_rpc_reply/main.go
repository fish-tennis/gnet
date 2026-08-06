package main

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/fish-tennis/gnet"
	"github.com/fish-tennis/gnet/example/pb"
)

// 本示例演示: RPC超时后如何正确处理迟到回复,包含幂等设计和补偿机制
//
// 场景: 客户端发起购买请求(RPC),设置3秒超时。服务端处理较慢(5秒),客户端超时返回。
// 随后服务端的回复延迟到达,客户端通过OnRecvPacket捕获迟到回复,进行补偿处理。
//
// 核心要点:
// 1. RPC超时只代表"在预期时间内未收到回复",不代表"服务端没有执行"
// 2. 超时后不可盲目重试,否则可能重复执行(如重复扣款)
// 3. 使用唯一requestId实现幂等性,服务端去重
// 4. 迟到回复会投递给OnRecvPacket,通过检查pkt.RpcCallId() > 0捕获并补偿

// PurchaseOrder 购买订单,用于跟踪RPC状态
type PurchaseOrder struct {
	RequestId  uint32       // 唯一请求ID,用于幂等
	ItemName   string       // 购买物品
	Status     int32        // 订单状态: 0=处理中 1=已成功 2=已超时 3=已补偿
	ReplyChan  chan *pb.TestMessage // 补偿通知chan
}

const (
	StatusProcessing  int32 = 0
	StatusSuccess     int32 = 1
	StatusTimeout     int32 = 2
	StatusCompensated int32 = 3
)

func main() {
	gnet.GetNetMgr()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ==================== 服务端 ====================

	serverCodec := gnet.NewProtoCodec(nil)
	serverHandler := gnet.NewDefaultConnectionHandler(serverCodec)

	// 服务端: 已处理过的requestId去重(幂等)
	processedRequests := make(map[uint64]bool)

	// 服务端处理购买请求
	serverHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn gnet.Connection, pkt gnet.Packet) {
			req := pkt.Message().(*pb.TestMessage)

			// 幂等检查: 同一个requestId只处理一次
			if req.GetI64() > 0 && processedRequests[uint64(req.GetI64())] {
				log.Printf("服务端: 重复请求(requestId=%d),已处理过,直接返回成功",
					req.GetI64())
				reply := &pb.TestMessage{Name: "购买成功(去重)", I32: req.I32}
				replyPkt := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), reply)
				replyPkt.SetRpcCallId(pkt.RpcCallId())
				conn.SendPacket(replyPkt)
				return
			}

			// 记录已处理
			if req.GetI64() > 0 {
				processedRequests[uint64(req.GetI64())] = true
			}

			log.Printf("服务端: 收到购买请求 name=%s, requestId=%d",
				req.Name, req.GetI64())

			// 模拟耗时处理(5秒),超过客户端的3秒超时
			time.Sleep(5 * time.Second)

			log.Printf("服务端: 购买处理完成 name=%s", req.Name)
			reply := &pb.TestMessage{Name: "购买成功", I32: 100}
			replyPkt := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), reply)
			replyPkt.SetRpcCallId(pkt.RpcCallId())
			conn.SendPacket(replyPkt)
		}, new(pb.TestMessage))

	listenerCfg := &gnet.ListenerConfig{
		AcceptConfig:    *defaultConfig(serverCodec, serverHandler),
		ListenerHandler: nil,
	}
	listener := gnet.GetNetMgr().NewListener(ctx, "127.0.0.1:18099", listenerCfg)
	defer listener.Close()

	time.Sleep(100 * time.Millisecond)

	// ==================== 客户端 ====================

	clientCodec := gnet.NewProtoCodec(nil)
	clientHandler := gnet.NewDefaultConnectionHandler(clientCodec)

	// 客户端订单管理: requestId -> Order
	pendingOrders := make(map[uint32]*PurchaseOrder)

	// 客户端handler: 处理迟到回复和异步通知
	clientHandler.Register(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage),
		func(conn gnet.Connection, pkt gnet.Packet) {
			if pkt.RpcCallId() > 0 {
				// RpcCallId > 0: 迟到的RPC回复!
				// RPC已超时,但服务端实际已处理完成
				lateReply := pkt.Message().(*pb.TestMessage)

				// 通过RpcCallId查找对应的订单(RpcCallId就是order.RequestId)
				order, ok := pendingOrders[pkt.RpcCallId()]
				if !ok {
					log.Printf("收到迟到回复但订单不存在: rpcCallId=%d", pkt.RpcCallId())
					return
				}

				// 补偿处理:
				// 1. 确认服务端已执行成功
				atomic.StoreInt32(&order.Status, StatusCompensated)
				log.Printf("补偿: 订单requestId=%d 确认成功, 服务端回复: %s, 到账: %d",
					order.RequestId, lateReply.Name, lateReply.I32)

				// 2. 通知上层(如UI层更新显示)
				order.ReplyChan <- lateReply
				return
			}

			// RpcCallId == 0: 正常的异步通知
			log.Printf("收到异步通知: %v", pkt.Message())
		}, new(pb.TestMessage))

	clientCfg := defaultConfig(clientCodec, clientHandler)
	client := gnet.GetNetMgr().NewConnector(ctx, "127.0.0.1:18099", clientCfg, "demo-client")
	if client == nil {
		log.Fatal("连接失败")
	}
	defer client.Close()

	time.Sleep(200 * time.Millisecond)

	// ==================== 发起RPC购买 ====================

	// 创建订单,分配唯一requestId
	// 注意: requestId与rpcCallId一一对应
	order := &PurchaseOrder{
		RequestId: 1,
		ItemName:  "钻石x100",
		Status:    StatusProcessing,
		ReplyChan: make(chan *pb.TestMessage, 1),
	}
	pendingOrders[order.RequestId] = order

	log.Printf("=== 发起购买请求: %s, requestId=%d ===", order.ItemName, order.RequestId)
	log.Println("=== 重要: RPC超时不代表服务端没执行,不可盲目重试 ===")

	// 构造RPC请求,携带requestId用于服务端幂等
	req := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{
		Name: order.ItemName,
		I64:  int64(order.RequestId), // requestId传给服务端做幂等
	})
	reply := &pb.TestMessage{}

	// 设置3秒回复超时(服务端需5秒处理,会触发超时)
	startTime := time.Now()
	err := client.RpcTimeout(req, reply, 3*time.Second, gnet.Timeout(gnet.DefaultSendTimeout))
	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("RPC超时: %v (耗时: %v)", err, elapsed)
		atomic.StoreInt32(&order.Status, StatusTimeout)
		log.Println("→ 客户端认为操作未完成")
		log.Println("→ 但服务端可能: (1)还没收到 (2)正在处理 (3)已完成但回复迟到")
		log.Println("→ 对不可重试操作(如扣款): 需幂等设计或对账机制,不可直接重试")
	}

	// ==================== 等待迟到回复 + 补偿 ====================

	log.Println("=== 等待迟到回复... ===")
	select {
	case lateReply := <-order.ReplyChan:
		status := atomic.LoadInt32(&order.Status)
		log.Printf("=== 补偿完成: 订单状态=%d, 服务端确认: %s ===", status, lateReply.Name)
	case <-time.After(10 * time.Second):
		log.Println("未收到迟到回复,需事后对账确认")
	}

	// ==================== 演示幂等重试 ====================
	//
	// 如果超时后确实需要重试,携带相同的requestId,服务端会去重,
	// 不会重复执行业务逻辑(如不会重复扣款)
	log.Println("=== 演示幂等重试(相同requestId) ===")
	retryOrder := &PurchaseOrder{
		RequestId: 2, // 注意: 重试同一笔交易应用相同的requestId,这里用新ID演示
		ItemName:  "钻石x100(重试)",
		Status:    StatusProcessing,
		ReplyChan: make(chan *pb.TestMessage, 1),
	}
	// 使用与order.RequestId=1相同的ID,测试服务端去重
	retryReq := gnet.NewProtoPacket(gnet.PacketCommand(pb.CmdTest_Cmd_TestMessage), &pb.TestMessage{
		Name: retryOrder.ItemName,
		I64:  1, // 相同requestId=1,服务端会去重
	})
	retryReply := &pb.TestMessage{}
	err = client.RpcTimeout(retryReq, retryReply, 8*time.Second, gnet.Timeout(gnet.DefaultSendTimeout))
	if err != nil {
		log.Printf("重试RPC错误: %v", err)
	} else {
		log.Printf("重试RPC成功: %s (服务端去重,未重复执行)", retryReply.Name)
	}

	log.Println("=== 示例结束 ===")
	time.Sleep(500 * time.Millisecond)
}

// ==================== 辅助函数 ====================

func defaultConfig(codec gnet.Codec, handler gnet.ConnectionHandler) *gnet.ConnectionConfig {
	cfg := &gnet.ConnectionConfig{
		SendPacketCacheCap: 1024,
		SendBufferSize:     gnet.DefaultConnectionConfig.SendBufferSize,
		RecvTimeout:        0,
		HeartBeatInterval:  0,
	}
	cfg.Codec = codec
	cfg.Handler = handler
	return cfg
}
