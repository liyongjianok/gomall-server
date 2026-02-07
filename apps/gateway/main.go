package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"go-ecommerce/pkg/config"
	"go-ecommerce/proto/cart"
	"go-ecommerce/proto/order"
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

	// ==========================================
	// 初始化 gRPC 连接 (User Service)
	// ==========================================
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

	// ==========================================
	// 初始化 gRPC 连接 (Product Service)
	// ==========================================
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

	// ==========================================
	// 初始化 gRPC 连接 (Cart Service) [新增]
	// ==========================================
	cartTarget := fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "cart-service")
	cartConn, err := grpc.Dial(
		cartTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("did not connect cart-service: %v", err)
	}
	defer cartConn.Close()
	cartClient := cart.NewCartServiceClient(cartConn)

	// ==========================================
	// 初始化 gRPC 连接 (Order Service) [新增]
	// ==========================================
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

	// ==========================================
	// 启动 Gin 路由
	// ==========================================
	r := gin.Default()
	v1 := r.Group("/api/v1")
	{
		// ----------- 用户相关 -----------
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

		// ----------- 商品相关 -----------
		v1.GET("/product/list", func(ctx *gin.Context) {
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
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch products"})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		v1.GET("/product/detail", func(ctx *gin.Context) {
			idStr := ctx.Query("id")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product ID"})
				return
			}
			rpcCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			resp, err := productClient.GetProduct(rpcCtx, &product.GetProductRequest{Id: id})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch product detail"})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		// ----------- 购物车相关 [新增] -----------

		// 添加商品到购物车
		v1.POST("/cart/add", func(ctx *gin.Context) {
			var req struct {
				UserId   int64 `json:"user_id" binding:"required"` // 暂时手动传 UserID，后续应从 Token 解析
				SkuId    int64 `json:"sku_id" binding:"required"`
				Quantity int32 `json:"quantity" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			rpcCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			resp, err := cartClient.AddItem(rpcCtx, &cart.AddItemRequest{
				UserId: req.UserId,
				Item: &cart.CartItem{
					SkuId:    req.SkuId,
					Quantity: req.Quantity,
				},
			})
			if err != nil {
				log.Printf("Cart Add Error: %v", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add to cart"})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		// 获取购物车列表
		v1.GET("/cart/list", func(ctx *gin.Context) {
			userIdStr := ctx.Query("user_id") // 暂时从 Query 获取
			userId, _ := strconv.ParseInt(userIdStr, 10, 64)
			if userId == 0 {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
				return
			}

			rpcCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			resp, err := cartClient.GetCart(rpcCtx, &cart.GetCartRequest{
				UserId: userId,
			})
			if err != nil {
				log.Printf("Cart Get Error: %v", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get cart"})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		// ----------- 订单相关 [新增] -----------
		v1.POST("/order/create", func(ctx *gin.Context) {
			var req struct {
				UserId int64 `json:"user_id" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			rpcCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // 下单可能慢，超时设置长一点
			defer cancel()

			resp, err := orderClient.CreateOrder(rpcCtx, &order.CreateOrderRequest{
				UserId: req.UserId,
			})
			if err != nil {
				log.Printf("Create Order Error: %v", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create order"})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		v1.GET("/order/list", func(ctx *gin.Context) {
			userIdStr := ctx.Query("user_id")
			userId, _ := strconv.ParseInt(userIdStr, 10, 64)

			rpcCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			resp, err := orderClient.ListOrders(rpcCtx, &order.ListOrdersRequest{
				UserId: userId,
			})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list orders"})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})
	}

	addr := fmt.Sprintf(":%d", c.Service.Port)
	log.Printf("Gateway running on %s", addr)
	r.Run(addr)
}
