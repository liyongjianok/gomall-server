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
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/cart"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

type server struct {
	cart.UnimplementedCartServiceServer
	rdb *redis.Client
}

func (s *server) AddItem(ctx context.Context, req *cart.AddItemRequest) (*cart.AddItemResponse, error) {
	key := fmt.Sprintf("cart:%d", req.UserId)
	field := fmt.Sprintf("%d", req.Item.SkuId)

	err := s.rdb.HSet(ctx, key, field, req.Item.Quantity).Err()
	if err != nil {
		return nil, status.Error(codes.Internal, "Redis error")
	}

	return &cart.AddItemResponse{Code: 0, Msg: "Success"}, nil
}

func (s *server) GetCart(ctx context.Context, req *cart.GetCartRequest) (*cart.GetCartResponse, error) {
	key := fmt.Sprintf("cart:%d", req.UserId)

	val, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, status.Error(codes.Internal, "Redis error")
	}

	var items []*cart.CartItem
	for k, v := range val {
		skuId, _ := strconv.ParseInt(k, 10, 64)
		quantity, _ := strconv.Atoi(v)
		items = append(items, &cart.CartItem{
			SkuId:    skuId,
			Quantity: int32(quantity),
		})
	}

	return &cart.GetCartResponse{Items: items}, nil
}

func (s *server) EmptyCart(ctx context.Context, req *cart.EmptyCartRequest) (*cart.EmptyCartResponse, error) {
	key := fmt.Sprintf("cart:%d", req.UserId)
	if err := s.rdb.Del(ctx, key).Err(); err != nil {
		return nil, status.Error(codes.Internal, "Redis delete error")
	}
	return &cart.EmptyCartResponse{}, nil
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 环境变量覆盖
	if v := os.Getenv("REDIS_ADDRESS"); v != "" {
		c.Redis.Address = v
		log.Println("Config Override: REDIS_ADDRESS used from env")
	}
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}

	// 连接 Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Address,
		Password: c.Redis.Password,
		DB:       c.Redis.Db,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

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
	cart.RegisterCartServiceServer(s, &server{rdb: rdb})
	reflection.Register(s)

	log.Printf("Cart Service listening on %s", addr)

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
