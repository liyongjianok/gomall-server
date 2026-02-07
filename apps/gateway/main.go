package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"go-ecommerce/pkg/config"
	"go-ecommerce/proto/product"
	"go-ecommerce/proto/user"

	"github.com/gin-gonic/gin"
	_ "github.com/mbobakov/grpc-consul-resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// 1. 加载配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// ------------------------------------------------------
	// 2. 初始化 gRPC 连接 (User Service)
	// ------------------------------------------------------
	userTarget := fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "user-service")
	userConn, err := grpc.Dial(
		userTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("did not connect user-service: %v", err)
	}
	defer userConn.Close()
	userClient := user.NewUserServiceClient(userConn)

	// ------------------------------------------------------
	// 3. 初始化 gRPC 连接 (Product Service)
	// ------------------------------------------------------
	// 注意：这里连接的是 product-service
	productTarget := fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "product-service")
	productConn, err := grpc.Dial(
		productTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("did not connect product-service: %v", err)
	}
	defer productConn.Close()
	productClient := product.NewProductServiceClient(productConn)

	// ------------------------------------------------------
	// 4. 启动 Gin 路由
	// ------------------------------------------------------
	r := gin.Default()
	v1 := r.Group("/api/v1")
	{
		// =========== 用户相关 ===========
		v1.POST("/user/login", func(ctx *gin.Context) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			rpcCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			resp, err := userClient.Login(rpcCtx, &user.LoginRequest{
				Username: req.Username,
				Password: req.Password,
			})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		v1.POST("/user/register", func(ctx *gin.Context) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
				Mobile   string `json:"mobile"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			rpcCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			resp, err := userClient.Register(rpcCtx, &user.RegisterRequest{
				Username: req.Username,
				Password: req.Password,
				Mobile:   req.Mobile,
			})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		// =========== 商品相关 (新增) ===========

		// 1. 商品列表接口
		v1.GET("/product/list", func(ctx *gin.Context) {
			// 获取 URL 参数 ?page=1&page_size=10&category_id=1
			page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
			categoryId, _ := strconv.ParseInt(ctx.DefaultQuery("category_id", "0"), 10, 64)

			rpcCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			resp, err := productClient.ListProducts(rpcCtx, &product.ListProductsRequest{
				Page:       int32(page),
				PageSize:   int32(pageSize),
				CategoryId: categoryId,
			})

			if err != nil {
				log.Printf("ListProducts RPC Error: %v", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch products"})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		// 2. 商品详情接口
		v1.GET("/product/detail", func(ctx *gin.Context) {
			// 获取参数 ?id=1
			idStr := ctx.Query("id")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product ID"})
				return
			}

			rpcCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			resp, err := productClient.GetProduct(rpcCtx, &product.GetProductRequest{
				Id: id,
			})

			if err != nil {
				log.Printf("GetProduct RPC Error: %v", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch product detail"})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})
	}

	addr := fmt.Sprintf(":%d", c.Service.Port)
	log.Printf("Gateway running on %s", addr)
	r.Run(addr)
}
