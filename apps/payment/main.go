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
	"go-ecommerce/proto/order" // [新增] 引入 Order Proto
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
	orderClient order.OrderServiceClient // [新增] 持有 Order 客户端
}

func (s *server) Pay(ctx context.Context, req *payment.PayRequest) (*payment.PayResponse, error) {
	log.Printf("Received payment request for Order: %s, Amount: %.2f", req.OrderNo, req.Amount)

	// 1. 模拟调用第三方支付 (支付宝/微信)
	// 这里我们简单 sleep 1秒，假设支付成功
	time.Sleep(1 * time.Second)

	// 2. 支付成功，调用 Order Service 更新订单状态
	// 注意：这里需要传入 context，最好带超时
	rpcCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := s.orderClient.MarkOrderPaid(rpcCtx, &order.MarkOrderPaidRequest{
		OrderNo: req.OrderNo,
	})

	if err != nil {
		log.Printf("CRITICAL: Payment successful but failed to update order status: %v", err)
		// 在真实系统中，这里需要重试机制 (MQ) 或人工介入
		return nil, status.Errorf(codes.Internal, "Payment processed but order status update failed")
	}

	log.Printf("Order %s paid successfully", req.OrderNo)

	return &payment.PayResponse{
		Success: true,
		TxId:    fmt.Sprintf("tx_%d", time.Now().UnixNano()), // 生成一个模拟的流水号
	}, nil
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// =====================================
	// 连接 Order Service
	// =====================================
	orderTarget := fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "order-service")
	orderConn, err := grpc.Dial(
		orderTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("did not connect order-service: %v", err)
	}
	defer orderConn.Close()
	orderClient := order.NewOrderServiceClient(orderConn)

	// =====================================
	// 启动 Payment Service
	// =====================================
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
		orderClient: orderClient, // 注入 Order Client
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
