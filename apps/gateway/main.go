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
	"go-ecommerce/pkg/tracer"
	"go-ecommerce/proto/address"
	"go-ecommerce/proto/admin"
	"go-ecommerce/proto/cart"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/payment"
	"go-ecommerce/proto/product"
	"go-ecommerce/proto/review"
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
	"google.golang.org/grpc/status"
)

// Sentinel 限流资源名常量
const ResSeckill = "seckill_api"

// initSentinel 初始化 Sentinel 限流规则
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
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),            // 添加 OTEL 拦截器：自动把 Trace ID 注入到 gRPC Metadata 中传给下游
	}

	// 连接 Admin Service
	adminConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "admin-service"), connOpts...)
	adminClient := admin.NewAdminServiceClient(adminConn)

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

	// 连接 Review Service
	reviewConn, err := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "review-service"), connOpts...)
	if err != nil {
		log.Fatalf("连接评价服务失败: %v", err)
	}
	reviewClient := review.NewReviewServiceClient(reviewConn)

	// ==========================================
	// 5. 启动 HTTP 服务 (Gin)
	// ==========================================
	r := gin.Default()

	// 添加 Gin 追踪中间件：拦截所有 HTTP 请求，生成 Trace ID
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
			// 1. 在结构体中增加 Nickname 字段
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
				Mobile   string `json:"mobile"`
				Nickname string `json:"nickname"` // 接收前端传来的昵称
			}

			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}

			// 2. 在调用 gRPC 客户端时，将 Nickname 传递给下游的 user-service
			// 注意：请确保你的 user.RegisterRequest 定义中包含 Nickname 字段（通常在 .proto 文件中定义）
			resp, err := userClient.Register(ctx.Request.Context(), &user.RegisterRequest{
				Username: req.Username,
				Password: req.Password,
				Mobile:   req.Mobile,
				Nickname: req.Nickname, // 透传给 user 核心服务
			})

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

		// 获取商品评价列表
		v1.GET("/review/list", func(c *gin.Context) {
			productId := c.Query("product_id")
			if productId == "" {
				response.Error(c, http.StatusBadRequest, "product_id 不能为空")
				return
			}
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
			pid, _ := strconv.ParseInt(productId, 10, 64)

			resp, err := reviewClient.ListReviews(c.Request.Context(), &review.ListReviewsRequest{
				ProductId: pid,
				Page:      int32(page),
				PageSize:  int32(pageSize),
			})
			if err != nil {
				response.Error(c, http.StatusInternalServerError, "查询评价失败")
				return
			}
			response.Success(c, resp)
		})
	}

	// ---------------------------
	// 受保护接口 (需 Bearer Token)
	// ---------------------------
	// 把 "/" 改成了 ""，解决了双斜杠 404 问题
	authed := v1.Group("")
	authed.Use(middleware.AuthMiddleware()) // 鉴权中间件
	{
		// 获取用户信息
		authed.GET("/user/info", func(ctx *gin.Context) {
			userId := ctx.MustGet("userId").(int64)
			resp, err := userClient.GetUserInfo(ctx, &user.GetUserInfoRequest{Id: userId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// 更新用户信息
		authed.POST("/user/update", func(ctx *gin.Context) {
			var req user.UpdateUserRequest
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, "参数错误")
				return
			}
			// 强制使用当前登录用户ID
			req.Id = ctx.MustGet("userId").(int64)

			resp, err := userClient.UpdateUser(ctx, &req)
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// 修改密码
		authed.POST("/user/password", func(ctx *gin.Context) {
			var req struct {
				OldPassword string `json:"old_password" binding:"required"`
				NewPassword string `json:"new_password" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}

			userId := ctx.MustGet("userId").(int64)
			resp, err := userClient.UpdatePassword(ctx.Request.Context(), &user.UpdatePasswordRequest{
				UserId:      userId,
				OldPassword: req.OldPassword,
				NewPassword: req.NewPassword,
			})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// --- 地址管理 ---
		authed.POST("/address/create", func(ctx *gin.Context) {
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

		// 设置默认地址
		authed.POST("/address/set_default", func(ctx *gin.Context) {
			var req struct {
				AddressId int64 `json:"address_id" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, "参数错误")
				return
			}

			userId := ctx.MustGet("userId").(int64)
			resp, err := addressClient.SetDefaultAddress(ctx.Request.Context(), &address.SetDefaultAddressRequest{
				AddressId: req.AddressId,
				UserId:    userId,
			})

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

		// 删除购物车商品
		authed.POST("/cart/delete", func(ctx *gin.Context) {
			var req struct {
				SkuId int64 `json:"sku_id" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}
			userId := ctx.MustGet("userId").(int64)
			_, err := cartClient.DeleteItem(ctx.Request.Context(), &cart.DeleteItemRequest{UserId: userId, SkuId: req.SkuId})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, nil)
		})

		// --- 订单管理 ---
		authed.POST("/order/create", func(ctx *gin.Context) {
			var req struct {
				AddressId int64   `json:"address_id" binding:"required"`
				SkuIds    []int64 `json:"sku_ids" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, "必须选择收货地址 (address_id)"+err.Error())
				return
			}
			userId := ctx.MustGet("userId").(int64)
			resp, err := orderClient.CreateOrder(ctx.Request.Context(), &order.CreateOrderRequest{
				UserId:    userId,
				AddressId: req.AddressId,
				SkuIds:    req.SkuIds,
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

		// 受保护的评价接口
		reviewGroup := authed.Group("/review")
		{
			// 新增评价 (需登录)
			reviewGroup.POST("/add", func(c *gin.Context) {
				userId := c.MustGet("userId").(int64)
				var req struct {
					OrderNo      string   `json:"order_no"`
					SkuId        int64    `json:"sku_id"`
					ProductId    int64    `json:"product_id"`
					Content      string   `json:"content"`
					Star         int32    `json:"star"`
					Images       []string `json:"images"`
					UserNickname string   `json:"user_nickname"`
					UserAvatar   string   `json:"user_avatar"`
					SkuName      string   `json:"sku_name"`
				}
				if err := c.ShouldBindJSON(&req); err != nil {
					response.Error(c, http.StatusBadRequest, "参数错误")
					return
				}

				// 拿着 SkuId 去商品服务反查真实的 ProductId
				productResp, err := productClient.GetProduct(c.Request.Context(), &product.GetProductRequest{Id: req.SkuId})
				if err == nil && productResp != nil {
					req.ProductId = productResp.Id // 获取真正的 product_id
				} else {
					req.ProductId = req.SkuId // 查不到就降级
				}

				resp, err := reviewClient.CreateReview(c.Request.Context(), &review.CreateReviewRequest{
					UserId:       userId,
					OrderNo:      req.OrderNo,
					SkuId:        req.SkuId,
					ProductId:    req.ProductId, // 使用反查回来的真实ID
					Content:      req.Content,
					Star:         req.Star,
					Images:       req.Images,
					UserNickname: req.UserNickname,
					UserAvatar:   req.UserAvatar,
					SkuName:      req.SkuName,
				})
				if err != nil {
					response.Error(c, http.StatusInternalServerError, status.Convert(err).Message())
					return
				}
				response.Success(c, resp)
			})

			// 检查是否已评价 (需登录)
			reviewGroup.GET("/status", func(c *gin.Context) {
				userId := c.MustGet("userId").(int64)
				orderNo := c.Query("order_no")
				skuIdStr := c.Query("sku_id")
				skuId, _ := strconv.ParseInt(skuIdStr, 10, 64)

				resp, err := reviewClient.CheckReviewStatus(c.Request.Context(), &review.CheckReviewStatusRequest{
					UserId:  userId,
					OrderNo: orderNo,
					SkuId:   skuId,
				})
				if err != nil {
					response.Error(c, http.StatusInternalServerError, "查询状态失败")
					return
				}
				response.Success(c, resp)
			})
		}

		adminGroup := authed.Group("/admin")
		{
			// 仪表盘统计
			adminGroup.GET("/dashboard/stats", func(ctx *gin.Context) {
				// 调用 admin-service 的 GetDashboardStats 接口
				resp, err := adminClient.GetDashboardStats(ctx.Request.Context(), &admin.StatsRequest{})
				if err != nil {
					response.Error(ctx, 500, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 用户管理列表
			adminGroup.GET("/users", func(ctx *gin.Context) {
				page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
				pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
				resp, err := adminClient.ListUsers(ctx.Request.Context(), &admin.ListUsersRequest{
					Page: int32(page), PageSize: int32(pageSize),
				})
				if err != nil {
					response.Error(ctx, 500, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 禁用/启用用户
			adminGroup.POST("/user/toggle", func(ctx *gin.Context) {
				// 修正后的结构体：字段之间需要换行，Tag 格式必须正确
				var req struct {
					UserId   int64 `json:"user_id"`
					Disabled bool  `json:"disabled"`
				}

				if err := ctx.ShouldBindJSON(&req); err != nil {
					response.Error(ctx, 400, "参数错误: "+err.Error())
					return
				}

				// 调用 adminClient
				resp, err := adminClient.ToggleUserStatus(ctx.Request.Context(), &admin.ToggleStatusRequest{
					UserId:   req.UserId,
					Disabled: req.Disabled,
				})

				if err != nil {
					response.Error(ctx, 500, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 删除用户
			adminGroup.DELETE("/user/:id", func(ctx *gin.Context) {
				idStr := ctx.Param("id")
				id, _ := strconv.ParseInt(idStr, 10, 64)

				resp, err := adminClient.DeleteUser(ctx.Request.Context(), &admin.DeleteUserRequest{UserId: id})
				if err != nil {
					response.Error(ctx, 500, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 商品管理列表
			adminGroup.GET("/products", func(ctx *gin.Context) {
				page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
				pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
				resp, err := adminClient.ListAllProducts(ctx, &admin.ListAllProductsRequest{
					Page: int32(page), PageSize: int32(pageSize),
				})
				if err != nil {
					response.Error(ctx, 500, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 更新商品库存/价格
			adminGroup.POST("/product/update", func(ctx *gin.Context) {
				var req admin.UpdateProductRequest
				if err := ctx.ShouldBindJSON(&req); err != nil {
					response.Error(ctx, 400, "参数错误")
					return
				}
				resp, err := adminClient.UpdateProduct(ctx, &req)
				if err != nil {
					response.Error(ctx, 500, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 订单发货
			adminGroup.POST("/order/ship", func(ctx *gin.Context) {
				var req struct {
					OrderNo string `json:"order_no"`
				}
				if err := ctx.ShouldBindJSON(&req); err != nil {
					response.Error(ctx, 400, "参数错误")
					return
				}
				resp, err := adminClient.ShipOrder(ctx, &admin.ShipOrderRequest{OrderNo: req.OrderNo})
				if err != nil {
					response.Error(ctx, 500, err.Error())
					return
				}
				response.Success(ctx, resp)
			})
		}
	}

	addr := fmt.Sprintf(":%d", c.Service.Port)
	log.Printf("Gateway 网关启动成功: %s", addr)
	r.Run(addr)
}
