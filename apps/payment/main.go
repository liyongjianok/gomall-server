package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/payment"

	"github.com/google/uuid"
	_ "github.com/mbobakov/grpc-consul-resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

type server struct {
	payment.UnimplementedPaymentServiceServer
	orderClient order.OrderServiceClient
}

func (s *server) Pay(ctx context.Context, req *payment.PayRequest) (*payment.PayResponse, error) {
	log.Printf("Processing payment for Order: %s, Amount: %.2f", req.OrderNo, req.Amount)

	// 1. 模拟与银行交互的耗时
	time.Sleep(500 * time.Millisecond)

	// 2. 假设银行扣款成功，生成流水号
	txID := "TX-" + uuid.New().String()

	// 3. 回调 Order Service 修改状态
	// 在实际系统中，这一步通常是异步的消息队列，或者由前端轮询，这里简化为同步调用
	_, err := s.orderClient.MarkOrderPaid(ctx, &order.MarkOrderPaidRequest{
		OrderNo: req.OrderNo,
	})
	if err != nil {
		log.Printf("Failed to update order status: %v", err)
		// 支付成功了但订单没改状态，这在生产环境需要重试机制
		return nil, err
	}

	log.Printf("Payment Success! Transaction ID: %s", txID)
	return &payment.PayResponse{
		Success:       true,
		TransactionId: txID,
	}, nil
}

func main() {
	// 1. 加载配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 连接 Order Service
	orderConn, err := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "order-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer orderConn.Close()
	orderClient := order.NewOrderServiceClient(orderConn)

	// 3. 启动服务
	addr := fmt.Sprintf(":%d", c.Service.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	err = discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	if err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	s := grpc.NewServer()
	payment.RegisterPaymentServiceServer(s, &server{orderClient: orderClient})
	reflection.Register(s)

	log.Printf("Payment Service listening on %s", addr)

	go func() {
		s.Serve(lis)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	s.GracefulStop()
}
