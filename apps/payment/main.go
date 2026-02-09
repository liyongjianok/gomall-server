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

// Pay 支付接口
func (s *server) Pay(ctx context.Context, req *payment.PayRequest) (*payment.PayResponse, error) {
	log.Printf("收到支付请求: 订单号 %s, 金额 %.2f", req.OrderNo, req.Amount)

	// 1. 模拟与第三方支付网关（支付宝/微信）的交互延迟
	time.Sleep(time.Duration(500+rand.Intn(1000)) * time.Millisecond)

	// 2. 模拟支付成功 (生成一个随机流水号)
	// 实际场景中这里需要验证签名、金额等
	transactionId := fmt.Sprintf("ALIPAY_%d", time.Now().UnixNano())
	log.Printf("第三方支付成功，流水号: %s", transactionId)

	// 3. 关键步骤：调用 Order Service 修改订单状态
	// 这是微服务间典型的 RPC 调用
	_, err := s.orderClient.MarkOrderPaid(ctx, &order.MarkOrderPaidRequest{
		OrderNo: req.OrderNo,
	})
	if err != nil {
		log.Printf("回调订单服务失败: %v", err)
		// 实际场景这里需要重试机制 (或写入消息队列进行最终一致性保障)
		return nil, status.Error(codes.Internal, "支付成功但更新订单状态失败")
	}

	log.Printf("订单 %s 状态已更新为[已支付]", req.OrderNo)

	return &payment.PayResponse{
		Success:       true,
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

	// 2. 初始化 gRPC 连接 (连接 Order Service)
	// 因为 Payment 服务需要回调 Order 服务
	orderConn, err := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "order-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("Failed to connect to order service: %v", err)
	}
	orderClient := order.NewOrderServiceClient(orderConn)

	// 3. 启动服务
	addr := fmt.Sprintf(":%d", c.Service.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	err = discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	if err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	s := grpc.NewServer()
	payment.RegisterPaymentServiceServer(s, &server{
		orderClient: orderClient,
	})
	reflection.Register(s)

	log.Printf("Payment Service listening on %s", addr)
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	s.GracefulStop()
}
