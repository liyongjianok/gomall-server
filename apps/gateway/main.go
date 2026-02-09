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
	"go-ecommerce/pkg/tracer" // [自研] 链路追踪初始化包
	"go-ecommerce/proto/address"
	"go-ecommerce/proto/cart"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/payment"
	"go-ecommerce/proto/product"
	"go-ecommerce/proto/user"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/base"
	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/gin-gonic/gin"
	_ "github.com/mbobakov/grpc-consul-resolver"

	// [OpenTelemetry] Gin 框架的追踪中间件
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	// [OpenTelemetry] gRPC 客户端的追踪拦截器
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Sentinel 限流资源名常量
const ResSeckill = "seckill_api"

// initSentinel 初始化 Sentinel 限流规则
// 作用：保护后端服务，防止高并发瞬间压垮系统
func initSentinel() {
	err := sentinel.InitDefault()
	if err != nil {
		log.Fatalf("Sentinel 初始化失败: %v", err)
	}

	// 加载流控规则
	_, err = flow.LoadRules([]*flow.Rule{
		{
			Resource:               ResSeckill,  // 针对秒杀接口
			TokenCalculateStrategy: flow.Direct, // 直接计数模式
			ControlBehavior:        flow.Reject, // 超出阈值直接拒绝
			Threshold:              5,           // [关键] QPS 限流阈值：每秒最多 5 个请求
			StatIntervalInMs:       1000,        // 统计时间窗口：1秒
		},
	})
	if err != nil {
		log.Fatalf("Sentinel 规则加载失败: %v", err)
	}
	log.Println("Sentinel 限流规则已加载 (QPS Limit: 5)")
}

func main() {
	// ==========================================
	// 1. 基础设施配置加载
	// ==========================================
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 适配 Docker 环境变量 (用于覆盖配置文件)
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}
	if v := os.Getenv("SERVICE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Service.Port = p
		}
	}

	// ==========================================
	// 2. 初始化链路追踪 (Jaeger)
	// ==========================================
	// 默认 Jaeger 地址 (Docker 内网)，如果是本地调试需改为 localhost
	jaegerAddr := "jaeger:4318"
	if v := os.Getenv("JAEGER_HOST"); v != "" {
		jaegerAddr = v
	}

	// 调用我们封装的 tracer 包
	tp, err := tracer.InitTracer("gateway", jaegerAddr)
	if err != nil {
		log.Printf("[Warning] 链路追踪初始化失败: %v", err)
	} else {
		log.Println("Jaeger 链路追踪已启动")
		// 程序退出时关闭 Tracer，确保数据上传完毕
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				log.Printf("关闭 Tracer 失败: %v", err)
			}
		}()
	}

	// ==========================================
	// 3. 初始化 Sentinel 限流器
	// ==========================================
	initSentinel()

	// ==========================================
	// 4. 初始化 gRPC 客户端 (连接微服务)
	// ==========================================
	connOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),                // 禁用 TLS (内网通信)
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`), // 轮询负载均衡

		// [核心] 添加 OTEL 拦截器：自动把 Trace ID 注入到 gRPC Metadata 中传给下游
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
	}

	// 连接 User Service
	userConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "user-service"), connOpts...)
	userClient := user.NewUserServiceClient(userConn)

	// 连接 Product Service
	prodConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "product-service"), connOpts...)
	productClient := product.NewProductServiceClient(prodConn)

	// 连接 Cart Service
	cartConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "cart-service"), connOpts...)
	cartClient := cart.NewCartServiceClient(cartConn)

	// 连接 Order Service
	orderConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "order-service"), connOpts...)
	orderClient := order.NewOrderServiceClient(orderConn)

	// 连接 Payment Service
	payConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "payment-service"), connOpts...)
	paymentClient := payment.NewPaymentServiceClient(payConn)

	// 连接 Address Service
	addrConn, err := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "address-service"), connOpts...)
	if err != nil {
		log.Fatalf("连接地址服务失败: %v", err)
	}
	addressClient := address.NewAddressServiceClient(addrConn)

	// ==========================================
	// 5. 启动 HTTP 服务 (Gin)
	// ==========================================
	r := gin.Default()

	// [核心] 添加 Gin 追踪中间件：拦截所有 HTTP 请求，生成 Trace ID
	r.Use(otelgin.Middleware("gateway"))

	v1 := r.Group("/api/v1")

	// ---------------------------
	// 公开接口 (无需 Token)
	// ---------------------------
	{
		// 用户登录
		v1.POST("/user/login", func(ctx *gin.Context) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			// 必须使用 ctx.Request.Context() 才能把 Trace 传递给 gRPC
			resp, err := userClient.Login(ctx.Request.Context(), &user.LoginRequest{Username: req.Username, Password: req.Password})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// 用户注册
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
			resp, err := userClient.Register(ctx.Request.Context(), &user.RegisterRequest{Username: req.Username, Password: req.Password, Mobile: req.Mobile})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// 商品列表 (支持搜索)
		v1.GET("/product/list", func(ctx *gin.Context) {
			page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
			catId, _ := strconv.ParseInt(ctx.DefaultQuery("category_id", "0"), 10, 64)
			query := ctx.Query("query")

			resp, err := productClient.ListProducts(ctx.Request.Context(), &product.ListProductsRequest{
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

		// 商品详情
		v1.GET("/product/detail", func(ctx *gin.Context) {
			id, _ := strconv.ParseInt(ctx.Query("id"), 10, 64)
			resp, err := productClient.GetProduct(ctx.Request.Context(), &product.GetProductRequest{Id: id})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})
	}

	// ---------------------------
	// 受保护接口 (需 Bearer Token)
	// ---------------------------
	authed := v1.Group("/")
	authed.Use(middleware.AuthMiddleware()) // 鉴权中间件
	{
		// --- 地址管理 ---
		authed.POST("/address/add", func(ctx *gin.Context) {
			var req address.CreateAddressRequest
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			req.UserId = ctx.MustGet("userId").(int64)
			resp, err := addressClient.CreateAddress(ctx.Request.Context(), &req)
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		authed.GET("/address/list", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := addressClient.ListAddress(ctx.Request.Context(), &address.ListAddressRequest{UserId: userId})
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
			resp, err := addressClient.UpdateAddress(ctx.Request.Context(), &req)
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
			resp, err := addressClient.DeleteAddress(ctx.Request.Context(), &address.DeleteAddressRequest{AddressId: req.AddressId, UserId: ctx.MustGet("userId").(int64)})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// --- 购物车 ---
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
			_, err := cartClient.AddItem(ctx.Request.Context(), &cart.AddItemRequest{UserId: userId, Item: &cart.CartItem{SkuId: req.SkuId, Quantity: req.Quantity}})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, nil)
		})

		authed.GET("/cart/list", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := cartClient.GetCart(ctx.Request.Context(), &cart.GetCartRequest{UserId: userId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// --- 订单管理 ---
		authed.POST("/order/create", func(ctx *gin.Context) {
			var req struct {
				AddressId int64 `json:"address_id" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, "必须选择收货地址 (address_id)")
				return
			}
			userId := ctx.MustGet("userId").(int64)
			resp, err := orderClient.CreateOrder(ctx.Request.Context(), &order.CreateOrderRequest{UserId: userId, AddressId: req.AddressId})
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
			resp, err := orderClient.CancelOrder(ctx.Request.Context(), &order.CancelOrderRequest{OrderNo: req.OrderNo, UserId: userId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		authed.GET("/order/list", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := orderClient.ListOrders(ctx.Request.Context(), &order.ListOrdersRequest{UserId: userId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// --- 秒杀接口 (带限流) ---
		authed.POST("/product/seckill", func(ctx *gin.Context) {
			// 1. Sentinel 限流埋点
			e, b := sentinel.Entry(ResSeckill, sentinel.WithTrafficType(base.Inbound))
			if b != nil {
				// 触发限流，直接返回 429
				response.Error(ctx, http.StatusTooManyRequests, "系统繁忙，请稍后再试")
				return
			}
			defer e.Exit()

			// 2. 正常业务逻辑
			var req struct {
				SkuId int64 `json:"sku_id" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			userId := ctx.MustGet("userId").(int64)

			resp, err := productClient.SeckillProduct(ctx.Request.Context(), &product.SeckillProductRequest{UserId: userId, SkuId: req.SkuId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// --- 支付接口 ---
		authed.POST("/payment/pay", func(ctx *gin.Context) {
			var req struct {
				OrderNo string  `json:"order_no" binding:"required"`
				Amount  float32 `json:"amount"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			resp, err := paymentClient.Pay(ctx.Request.Context(), &payment.PayRequest{OrderNo: req.OrderNo, Amount: req.Amount})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})
	}

	addr := fmt.Sprintf(":%d", c.Service.Port)
	log.Printf("Gateway 网关启动成功: %s", addr)
	r.Run(addr)
}
