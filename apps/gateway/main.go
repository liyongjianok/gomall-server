package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"go-ecommerce/apps/gateway/middleware"
	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/response"
	"go-ecommerce/proto/address"
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

	// 适配 Docker 环境变量
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}
	if v := os.Getenv("SERVICE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Service.Port = p
		}
	}

	// ==========================================
	// 初始化 gRPC 连接
	// ==========================================
	connOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	}

	// User Service
	userConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "user-service"), connOpts...)
	userClient := user.NewUserServiceClient(userConn)

	// Product Service
	prodConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "product-service"), connOpts...)
	productClient := product.NewProductServiceClient(prodConn)

	// Cart Service
	cartConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "cart-service"), connOpts...)
	cartClient := cart.NewCartServiceClient(cartConn)

	// Order Service
	orderConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "order-service"), connOpts...)
	orderClient := order.NewOrderServiceClient(orderConn)

	// Payment Service
	payConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "payment-service"), connOpts...)
	paymentClient := payment.NewPaymentServiceClient(payConn)

	// Address Service
	addrConn, err := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "address-service"), connOpts...)
	if err != nil {
		log.Fatalf("did not connect address-service: %v", err)
	}
	addressClient := address.NewAddressServiceClient(addrConn)

	// ==========================================
	// 启动 Gin 路由
	// ==========================================
	r := gin.Default()
	v1 := r.Group("/api/v1")

	// ------------------------------------------
	// 公开接口
	// ------------------------------------------
	{
		v1.POST("/user/login", func(ctx *gin.Context) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			resp, err := userClient.Login(context.Background(), &user.LoginRequest{Username: req.Username, Password: req.Password})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		v1.POST("/user/register", func(ctx *gin.Context) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
				Mobile   string `json:"mobile"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			resp, err := userClient.Register(context.Background(), &user.RegisterRequest{Username: req.Username, Password: req.Password, Mobile: req.Mobile})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// 获取商品列表 (支持搜索 query)
		v1.GET("/product/list", func(ctx *gin.Context) {
			page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
			catId, _ := strconv.ParseInt(ctx.DefaultQuery("category_id", "0"), 10, 64)
			query := ctx.Query("query") // 接收 query 参数

			resp, err := productClient.ListProducts(context.Background(), &product.ListProductsRequest{
				Page:       int32(page),
				PageSize:   int32(pageSize),
				CategoryId: catId,
				Query:      query, // 传递给 RPC
			})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		v1.GET("/product/detail", func(ctx *gin.Context) {
			id, _ := strconv.ParseInt(ctx.Query("id"), 10, 64)
			resp, err := productClient.GetProduct(context.Background(), &product.GetProductRequest{Id: id})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})
	}

	// ------------------------------------------
	// 受保护接口
	// ------------------------------------------
	authed := v1.Group("/")
	authed.Use(middleware.AuthMiddleware())
	{
		// ----------- Address -----------
		authed.POST("/address/add", func(ctx *gin.Context) {
			var req address.CreateAddressRequest
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			req.UserId = ctx.MustGet("userId").(int64)

			resp, err := addressClient.CreateAddress(context.Background(), &req)
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		authed.GET("/address/list", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := addressClient.ListAddress(context.Background(), &address.ListAddressRequest{UserId: userId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		authed.POST("/address/update", func(ctx *gin.Context) {
			var req address.UpdateAddressRequest
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			req.UserId = ctx.MustGet("userId").(int64)
			resp, err := addressClient.UpdateAddress(context.Background(), &req)
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		authed.POST("/address/delete", func(ctx *gin.Context) {
			var req struct {
				AddressId int64 `json:"address_id"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			resp, err := addressClient.DeleteAddress(context.Background(), &address.DeleteAddressRequest{AddressId: req.AddressId, UserId: ctx.MustGet("userId").(int64)})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// ----------- Cart -----------
		authed.POST("/cart/add", func(ctx *gin.Context) {
			var req struct {
				SkuId    int64 `json:"sku_id" binding:"required"`
				Quantity int32 `json:"quantity" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			userId := ctx.MustGet("userId").(int64)
			_, err := cartClient.AddItem(context.Background(), &cart.AddItemRequest{UserId: userId, Item: &cart.CartItem{SkuId: req.SkuId, Quantity: req.Quantity}})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, nil)
		})

		authed.GET("/cart/list", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := cartClient.GetCart(context.Background(), &cart.GetCartRequest{UserId: userId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// ----------- Order -----------
		authed.POST("/order/create", func(ctx *gin.Context) {
			var req struct {
				AddressId int64 `json:"address_id" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, "必须选择收货地址 (address_id)")
				return
			}

			userId := ctx.MustGet("userId").(int64)
			resp, err := orderClient.CreateOrder(context.Background(), &order.CreateOrderRequest{
				UserId:    userId,
				AddressId: req.AddressId,
			})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		authed.POST("/order/cancel", func(ctx *gin.Context) {
			var req struct {
				OrderNo string `json:"order_no" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			userId := ctx.MustGet("userId").(int64)
			resp, err := orderClient.CancelOrder(context.Background(), &order.CancelOrderRequest{OrderNo: req.OrderNo, UserId: userId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		authed.GET("/order/list", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := orderClient.ListOrders(context.Background(), &order.ListOrdersRequest{UserId: userId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// ----------- Payment -----------
		authed.POST("/payment/pay", func(ctx *gin.Context) {
			var req struct {
				OrderNo string  `json:"order_no" binding:"required"`
				Amount  float32 `json:"amount"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			resp, err := paymentClient.Pay(context.Background(), &payment.PayRequest{OrderNo: req.OrderNo, Amount: req.Amount})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})
	}

	addr := fmt.Sprintf(":%d", c.Service.Port)
	log.Printf("Gateway running on %s", addr)
	r.Run(addr)
}
