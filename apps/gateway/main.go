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

	sentinel "github.com/alibaba/sentinel-golang/api" // [新增]
	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/gin-gonic/gin"
	_ "github.com/mbobakov/grpc-consul-resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// 定义资源名称
const ResSeckill = "seckill_api"

// initSentinel 初始化限流规则
func initSentinel() {
	err := sentinel.InitDefault()
	if err != nil {
		log.Fatalf("初始化 Sentinel 失败: %v", err)
	}

	// 配置流控规则
	_, err = flow.LoadRules([]*flow.Rule{
		{
			Resource:               ResSeckill,  // 资源名称
			TokenCalculateStrategy: flow.Direct, // 直接计数
			ControlBehavior:        flow.Reject, // 直接拒绝
			Threshold:              5,           // [关键] QPS 限制为 5 (每秒只许通过 5 个请求)
			StatIntervalInMs:       1000,        // 统计周期 1秒
		},
	})
	if err != nil {
		log.Fatalf("加载 Sentinel 规则失败: %v", err)
	}
	log.Println("Sentinel 限流规则已加载: 秒杀接口 QPS Limit = 5")
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 环境变量适配
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}
	if v := os.Getenv("SERVICE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Service.Port = p
		}
	}

	// [新增] 初始化限流器
	initSentinel()

	// 初始化 gRPC 连接
	connOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	}

	userConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "user-service"), connOpts...)
	userClient := user.NewUserServiceClient(userConn)

	prodConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "product-service"), connOpts...)
	productClient := product.NewProductServiceClient(prodConn)

	cartConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "cart-service"), connOpts...)
	cartClient := cart.NewCartServiceClient(cartConn)

	orderConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "order-service"), connOpts...)
	orderClient := order.NewOrderServiceClient(orderConn)

	payConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "payment-service"), connOpts...)
	paymentClient := payment.NewPaymentServiceClient(payConn)

	addrConn, err := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "address-service"), connOpts...)
	if err != nil {
		log.Fatalf("did not connect address-service: %v", err)
	}
	addressClient := address.NewAddressServiceClient(addrConn)

	// 启动 Gin
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

		v1.GET("/product/list", func(ctx *gin.Context) {
			page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
			catId, _ := strconv.ParseInt(ctx.DefaultQuery("category_id", "0"), 10, 64)
			query := ctx.Query("query")

			resp, err := productClient.ListProducts(context.Background(), &product.ListProductsRequest{
				Page:       int32(page),
				PageSize:   int32(pageSize),
				CategoryId: catId,
				Query:      query,
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

	// 受保护接口
	authed := v1.Group("/")
	authed.Use(middleware.AuthMiddleware())
	{
		// Address
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

		// Cart
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

		// Order
		authed.POST("/order/create", func(ctx *gin.Context) {
			var req struct {
				AddressId int64 `json:"address_id" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, "必须选择收货地址 (address_id)")
				return
			}
			userId := ctx.MustGet("userId").(int64)
			resp, err := orderClient.CreateOrder(context.Background(), &order.CreateOrderRequest{UserId: userId, AddressId: req.AddressId})
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

		// [修改] Seckill 增加 Sentinel 限流埋点
		authed.POST("/product/seckill", func(ctx *gin.Context) {
			// 1. Sentinel 入口检查
			e, b := sentinel.Entry(ResSeckill, sentinel.WithTrafficType(base.Inbound))
			if b != nil {
				// 被限流了
				response.Error(ctx, http.StatusTooManyRequests, "系统繁忙，请稍后再试")
				return
			}
			defer e.Exit() // 务必退出

			// 2. 正常业务逻辑
			var req struct {
				SkuId int64 `json:"sku_id" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			userId := ctx.MustGet("userId").(int64)

			resp, err := productClient.SeckillProduct(context.Background(), &product.SeckillProductRequest{UserId: userId, SkuId: req.SkuId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// Payment
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
