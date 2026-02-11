package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/payment"

	_ "github.com/mbobakov/grpc-consul-resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

type server struct {
	payment.UnimplementedPaymentServiceServer
	orderClient order.OrderServiceClient
}

// Pay 支付接口实现
func (s *server) Pay(ctx context.Context, req *payment.PayRequest) (*payment.PayResponse, error) {
	log.Printf("📥 [Payment] 收到支付请求: OrderNo=%s, Amount=%.2f", req.OrderNo, req.Amount)

	// 1. 模拟与第三方支付网关（支付宝/微信）的交互延迟 (0.5s - 1.5s)
	time.Sleep(time.Duration(500+rand.Intn(1000)) * time.Millisecond)

	// 2. 模拟支付成功 (生成一个随机流水号)
	transactionId := fmt.Sprintf("ALIPAY_%d_%s", time.Now().UnixNano(), req.OrderNo)
	log.Printf("✅ [Payment] 第三方支付扣款成功，流水号: %s", transactionId)

	// 3. 关键步骤：调用 Order Service 修改订单状态
	log.Printf("🔄 [Payment] 正在回调订单服务更新状态...")
	_, err := s.orderClient.MarkOrderPaid(ctx, &order.MarkOrderPaidRequest{
		OrderNo: req.OrderNo,
	})

	if err != nil {
		log.Printf("❌ [Payment] 回调订单服务失败: %v", err)
		// 注意：在真实生产环境中，这里不能直接返回错误，否则用户扣了钱但订单显示没支付。
		// 应该写入本地消息表或发 MQ 消息，进行最终一致性重试。
		// 这里为了演示简单，先返回错误。
		return nil, status.Error(codes.Internal, "支付成功但同步订单状态失败")
	}

	log.Printf("🎉 [Payment] 订单 %s 流程全部完成 (状态已更新为已支付)", req.OrderNo)

	return &payment.PayResponse{
		TransactionId: transactionId,
	}, nil
}

func main() {
	// 1. 加载配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 环境变量适配
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}
	if v := os.Getenv("SERVICE_PORT"); v != "" {
		// 这里简单处理，实际应转换类型赋值，或者直接信赖 config 里的默认值
		// c.Service.Port = ...
	}

	// 2. 初始化 gRPC 连接 (连接 Order Service)
	// 使用 consul 解析器动态发现 order-service
	orderConn, err := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "order-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("Failed to connect to order service: %v", err)
	}
	defer orderConn.Close()

	orderClient := order.NewOrderServiceClient(orderConn)
	log.Println("🔗 已连接到 Order Service")

	// 3. 启动 Payment 服务
	addr := fmt.Sprintf(":%d", c.Service.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 注册到 Consul
	err = discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	if err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	s := grpc.NewServer()
	payment.RegisterPaymentServiceServer(s, &server{
		orderClient: orderClient,
	})
	reflection.Register(s)

	log.Printf("🚀 Payment Service listening on %s", addr)

	// 优雅退出处理
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down Payment Service...")
	s.GracefulStop()
}
