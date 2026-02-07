package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/cart"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type server struct {
	cart.UnimplementedCartServiceServer
	rdb *redis.Client
}

// AddItem 添加商品 (使用 Redis Hash: HINCRBY)
func (s *server) AddItem(ctx context.Context, req *cart.AddItemRequest) (*cart.AddItemResponse, error) {
	// Redis Key: cart:1001
	key := fmt.Sprintf("cart:%d", req.UserId)
	// Field: sku_id
	field := fmt.Sprintf("%d", req.Item.SkuId)

	// 如果 quantity 是负数，表示减少数量
	err := s.rdb.HIncrBy(ctx, key, field, int64(req.Item.Quantity)).Err()
	if err != nil {
		return &cart.AddItemResponse{Code: 500, Msg: err.Error()}, nil
	}

	return &cart.AddItemResponse{Code: 0, Msg: "Success"}, nil
}

// GetCart 获取购物车 (HGETALL)
func (s *server) GetCart(ctx context.Context, req *cart.GetCartRequest) (*cart.GetCartResponse, error) {
	key := fmt.Sprintf("cart:%d", req.UserId)

	// 获取所有 field (sku_id) 和 value (quantity)
	result, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var items []*cart.CartItem
	for skuIdStr, qtyStr := range result {
		skuId, _ := strconv.ParseInt(skuIdStr, 10, 64)
		qty, _ := strconv.Atoi(qtyStr)

		// 注意：这里为了简化，没有回查 Product Service 获取商品名
		// 实际生产中，通常由前端拿着 ID 去查详情，或者由 Gateway 聚合
		items = append(items, &cart.CartItem{
			SkuId:    skuId,
			Quantity: int32(qty),
			// ProductId 这里暂时拿不到，因为 Redis Hash key 只有 sku_id
			// 如果需要 ProductID，可以在 Key 设计上做优化，或者存 JSON
		})
	}

	return &cart.GetCartResponse{Items: items}, nil
}

// EmptyCart 清空购物车 (DEL)
func (s *server) EmptyCart(ctx context.Context, req *cart.EmptyCartRequest) (*cart.EmptyCartResponse, error) {
	key := fmt.Sprintf("cart:%d", req.UserId)
	err := s.rdb.Del(ctx, key).Err()
	if err != nil {
		return &cart.EmptyCartResponse{Success: false}, nil
	}
	return &cart.EmptyCartResponse{Success: true}, nil
}

func main() {
	// 1. 加载配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 初始化 Redis
	rdb := database.InitRedis(c.Redis)

	// 3. 监听端口
	addr := fmt.Sprintf(":%d", c.Service.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 4. 注册到 Consul
	err = discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	if err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	// 5. 启动 gRPC Server
	grpcServer := grpc.NewServer()
	cart.RegisterCartServiceServer(grpcServer, &server{rdb: rdb})
	reflection.Register(grpcServer)

	log.Printf("Cart Service listening on %s", addr)

	// 6. 优雅退出
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	grpcServer.GracefulStop()
}
