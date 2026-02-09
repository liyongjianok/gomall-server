package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"go-ecommerce/apps/gateway/middleware"
	"go-ecommerce/pkg/config"
	"go-ecommerce/proto/address" // [新增]
	"go-ecommerce/proto/cart"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/payment"
	"go-ecommerce/proto/product"
	"go-ecommerce/proto/user"

	"github.com/gin-gonic/gin"
	_ "github.com/mbobakov/grpc-consul-resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// ==========================================
	// 初始化 gRPC 连接 (User)
	// ==========================================
	userConn, _ := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "user-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	defer userConn.Close()
	userClient := user.NewUserServiceClient(userConn)

	// ==========================================
	// 初始化 gRPC 连接 (Product)
	// ==========================================
	prodConn, _ := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "product-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	defer prodConn.Close()
	productClient := product.NewProductServiceClient(prodConn)

	// ==========================================
	// 初始化 gRPC 连接 (Cart)
	// ==========================================
	cartConn, _ := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "cart-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	defer cartConn.Close()
	cartClient := cart.NewCartServiceClient(cartConn)

	// ==========================================
	// 初始化 gRPC 连接 (Order)
	// ==========================================
	orderConn, _ := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "order-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	defer orderConn.Close()
	orderClient := order.NewOrderServiceClient(orderConn)

	// ==========================================
	// 初始化 gRPC 连接 (Payment)
	// ==========================================
	payConn, _ := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "payment-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	defer payConn.Close()
	paymentClient := payment.NewPaymentServiceClient(payConn)

	// ==========================================
	// [新增] 初始化 gRPC 连接 (Address)
	// ==========================================
	addrConn, err := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "address-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("did not connect address-service: %v", err)
	}
	defer addrConn.Close()
	addressClient := address.NewAddressServiceClient(addrConn)

	// ==========================================
	// 启动 Gin 路由
	// ==========================================
	r := gin.Default()
	v1 := r.Group("/api/v1")

	// 公开接口
	{
		v1.POST("/user/login", func(ctx *gin.Context) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			resp, err := userClient.Login(context.Background(), &user.LoginRequest{Username: req.Username, Password: req.Password})
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
			resp, err := userClient.Register(context.Background(), &user.RegisterRequest{Username: req.Username, Password: req.Password, Mobile: req.Mobile})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		v1.GET("/product/list", func(ctx *gin.Context) {
			page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
			catId, _ := strconv.ParseInt(ctx.DefaultQuery("category_id", "0"), 10, 64)
			resp, err := productClient.ListProducts(context.Background(), &product.ListProductsRequest{Page: int32(page), PageSize: int32(pageSize), CategoryId: catId})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		v1.GET("/product/detail", func(ctx *gin.Context) {
			id, _ := strconv.ParseInt(ctx.Query("id"), 10, 64)
			resp, err := productClient.GetProduct(context.Background(), &product.GetProductRequest{Id: id})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})
	}

	// 受保护接口
	authed := v1.Group("/")
	authed.Use(middleware.AuthMiddleware())
	{
		// ----------- Address -----------
		authed.POST("/address/add", func(ctx *gin.Context) {
			var req address.CreateAddressRequest
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			req.UserId = ctx.MustGet("userId").(int64)

			resp, err := addressClient.CreateAddress(context.Background(), &req)
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		authed.GET("/address/list", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := addressClient.ListAddress(context.Background(), &address.ListAddressRequest{UserId: userId})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		authed.POST("/address/update", func(ctx *gin.Context) {
			var req address.UpdateAddressRequest
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			req.UserId = ctx.MustGet("userId").(int64)
			resp, err := addressClient.UpdateAddress(context.Background(), &req)
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		authed.POST("/address/delete", func(ctx *gin.Context) {
			var req struct {
				AddressId int64 `json:"address_id"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			resp, err := addressClient.DeleteAddress(context.Background(), &address.DeleteAddressRequest{AddressId: req.AddressId, UserId: ctx.MustGet("userId").(int64)})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		// ----------- Cart -----------
		authed.POST("/cart/add", func(ctx *gin.Context) {
			var req struct {
				SkuId    int64 `json:"sku_id" binding:"required"`
				Quantity int32 `json:"quantity" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			userId := ctx.MustGet("userId").(int64)
			resp, err := cartClient.AddItem(context.Background(), &cart.AddItemRequest{UserId: userId, Item: &cart.CartItem{SkuId: req.SkuId, Quantity: req.Quantity}})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		authed.GET("/cart/list", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := cartClient.GetCart(context.Background(), &cart.GetCartRequest{UserId: userId})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		// ----------- Order -----------
		authed.POST("/order/create", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			// 注意：这里暂时还没把 AddressId 加进去，下一步我们再加
			resp, err := orderClient.CreateOrder(context.Background(), &order.CreateOrderRequest{UserId: userId})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		authed.POST("/order/cancel", func(ctx *gin.Context) {
			var req struct {
				OrderNo string `json:"order_no" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			userId := ctx.MustGet("userId").(int64)
			resp, err := orderClient.CancelOrder(context.Background(), &order.CancelOrderRequest{OrderNo: req.OrderNo, UserId: userId})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		authed.GET("/order/list", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := orderClient.ListOrders(context.Background(), &order.ListOrdersRequest{UserId: userId})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		// ----------- Payment -----------
		authed.POST("/payment/pay", func(ctx *gin.Context) {
			var req struct {
				OrderNo string  `json:"order_no" binding:"required"`
				Amount  float32 `json:"amount"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			resp, err := paymentClient.Pay(context.Background(), &payment.PayRequest{OrderNo: req.OrderNo, Amount: req.Amount})
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})
	}

	addr := fmt.Sprintf(":%d", c.Service.Port)
	log.Printf("Gateway running on %s", addr)
	r.Run(addr)
}
