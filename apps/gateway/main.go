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

// Sentinel 限流资源名常量，用于秒杀接口的流量控制
const ResSeckill = "seckill_api"

// initSentinel 初始化 Sentinel 流控组件并加载硬编码规则
func initSentinel() {
	err := sentinel.InitDefault()
	if err != nil {
		log.Fatalf("Sentinel 初始化失败: %v", err)
	}

	// 加载流控规则：每秒最多允许 5 个请求进入秒杀接口
	_, err = flow.LoadRules([]*flow.Rule{
		{
			Resource:               ResSeckill,  // 针对秒杀接口
			TokenCalculateStrategy: flow.Direct, // 直接计数模式
			ControlBehavior:        flow.Reject, // 超出阈值直接拒绝
			Threshold:              5,           // QPS 限流阈值：每秒最多 5 个请求
			StatIntervalInMs:       1000,        // 统计时间窗口：1秒
		},
	})
	if err != nil {
		log.Fatalf("Sentinel 规则加载失败: %v", err)
	}
	log.Println("Sentinel 限流规则已加载 (QPS Limit: 5)")
}

func main() {
	// 1. 加载系统配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 适配 Docker 环境，优先从环境变量获取 Consul 地址和端口
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}
	if v := os.Getenv("SERVICE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Service.Port = p
		}
	}

	// 2. 初始化全链路追踪 (Jaeger)
	jaegerAddr := "jaeger:4318"
	if v := os.Getenv("JAEGER_HOST"); v != "" {
		jaegerAddr = v
	}
	tp, err := tracer.InitTracer("gateway", jaegerAddr)
	if err != nil {
		log.Printf("[Warning] 追踪系统启动失败: %v", err)
	} else {
		log.Println("Jaeger 链路追踪已启动")
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				log.Printf("关闭 Tracer 失败: %v", err)
			}
		}()
	}

	// 3. 启动流量哨兵
	initSentinel()

	// 4. 初始化 gRPC 拨号配置 (包含 OTEL 追踪与轮询负载均衡)
	connOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()), // 关键：注入 Trace 上下文
	}

	// 建立各微服务的拨号连接 (使用 Consul 服务发现)
	dial := func(serviceName string) *grpc.ClientConn {
		conn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, serviceName), connOpts...)
		return conn
	}

	adminClient := admin.NewAdminServiceClient(dial("admin-service"))
	userClient := user.NewUserServiceClient(dial("user-service"))
	productClient := product.NewProductServiceClient(dial("product-service"))
	cartClient := cart.NewCartServiceClient(dial("cart-service"))
	orderClient := order.NewOrderServiceClient(dial("order-service"))
	paymentClient := payment.NewPaymentServiceClient(dial("payment-service"))
	addressClient := address.NewAddressServiceClient(dial("address-service"))
	reviewClient := review.NewReviewServiceClient(dial("review-service"))

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

		// 注册 (包含昵称字段透传)
		v1.POST("/user/register", func(ctx *gin.Context) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
				Mobile   string `json:"mobile"`
				Nickname string `json:"nickname"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, err.Error())
				return
			}

			// 2. 在调用 gRPC 客户端时，将 Nickname 传递给下游的 user-service
			// 注意：请确保你的 user.RegisterRequest 定义中包含 Nickname 字段（通常在 .proto 文件中定义）
			resp, err := userClient.Register(ctx.Request.Context(), &user.RegisterRequest{
				Username: req.Username, Password: req.Password, Mobile: req.Mobile, Nickname: req.Nickname,
			})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, resp)
		})

		// 商城前端商品展示与搜索
		v1.GET("/product/list", func(ctx *gin.Context) {
			page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
			catId, _ := strconv.ParseInt(ctx.DefaultQuery("category_id", "0"), 10, 64)
			query := ctx.Query("query")
			resp, err := productClient.ListProducts(ctx.Request.Context(), &product.ListProductsRequest{
				Page: int32(page), PageSize: int32(pageSize), CategoryId: catId, Query: query,
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
			pid, _ := strconv.ParseInt(c.Query("product_id"), 10, 64)
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
			resp, err := reviewClient.ListReviews(c.Request.Context(), &review.ListReviewsRequest{
				ProductId: pid, Page: int32(page), PageSize: int32(pageSize),
			})
			if err != nil {
				response.Error(c, http.StatusInternalServerError, "查询评价失败")
				return
			}
			response.Success(c, resp)
		})
	}

	// ---------------------------
	// 受保护接口 (需 Bearer Token 即 JWT Token)
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
			resp, err := userClient.UpdatePassword(ctx.Request.Context(), &user.UpdatePasswordRequest{
				UserId: ctx.MustGet("userId").(int64), OldPassword: req.OldPassword, NewPassword: req.NewPassword,
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
			resp, err := addressClient.ListAddress(ctx.Request.Context(), &address.ListAddressRequest{UserId: ctx.MustGet("userId").(int64)})
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
			_, err := cartClient.AddItem(ctx.Request.Context(), &cart.AddItemRequest{UserId: ctx.MustGet("userId").(int64), Item: &cart.CartItem{SkuId: req.SkuId, Quantity: req.Quantity}})
			if err != nil {
				response.Error(ctx, http.StatusInternalServerError, err.Error())
				return
			}
			response.Success(ctx, nil)
		})

		authed.GET("/cart/list", func(ctx *gin.Context) {
			resp, err := cartClient.GetCart(ctx.Request.Context(), &cart.GetCartRequest{UserId: ctx.MustGet("userId").(int64)})
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

		// --- 交易子组 (秒杀、下单、支付) ---
		authed.POST("/order/create", func(ctx *gin.Context) {
			var req struct {
				AddressId int64   `json:"address_id" binding:"required"`
				SkuIds    []int64 `json:"sku_ids" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				response.Error(ctx, http.StatusBadRequest, "必须选择收货地址 (address_id)"+err.Error())
				return
			}
			resp, err := orderClient.CreateOrder(ctx.Request.Context(), &order.CreateOrderRequest{
				UserId: ctx.MustGet("userId").(int64), AddressId: req.AddressId, SkuIds: req.SkuIds,
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
			resp, err := productClient.SeckillProduct(ctx.Request.Context(), &product.SeckillProductRequest{UserId: ctx.MustGet("userId").(int64), SkuId: req.SkuId})
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

		// --- [管理后台专属接口组] ---
		adminGroup := authed.Group("/admin")
		{
			// 获取后台仪表盘统计数据 (成交额、GMV、品类占比等)
			adminGroup.GET("/dashboard/stats", func(ctx *gin.Context) {
				resp, err := adminClient.GetDashboardStats(ctx.Request.Context(), &admin.StatsRequest{})
				if err != nil {
					response.Error(ctx, http.StatusInternalServerError, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 平台用户管理列表
			adminGroup.GET("/users", func(ctx *gin.Context) {
				page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
				pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
				resp, err := adminClient.ListUsers(ctx.Request.Context(), &admin.ListUsersRequest{
					Page: int32(page), PageSize: int32(pageSize),
				})
				if err != nil {
					response.Error(ctx, http.StatusInternalServerError, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 修改用户状态 (禁用/启用)
			adminGroup.POST("/user/toggle", func(ctx *gin.Context) {
				var req struct {
					UserId   int64 `json:"user_id"`
					Disabled bool  `json:"disabled"`
				}
				if err := ctx.ShouldBindJSON(&req); err != nil {
					response.Error(ctx, http.StatusBadRequest, "参数解析错误")
					return
				}
				resp, err := adminClient.ToggleUserStatus(ctx.Request.Context(), &admin.ToggleStatusRequest{
					UserId: req.UserId, Disabled: req.Disabled,
				})
				if err != nil {
					response.Error(ctx, http.StatusInternalServerError, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 删除指定用户
			adminGroup.DELETE("/user/:id", func(ctx *gin.Context) {
				id, _ := strconv.ParseInt(ctx.Param("id"), 10, 64)
				resp, err := adminClient.DeleteUser(ctx.Request.Context(), &admin.DeleteUserRequest{UserId: id})
				if err != nil {
					response.Error(ctx, http.StatusInternalServerError, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 商品管理列表 (支持按 category 过滤转发)
			adminGroup.GET("/products", func(ctx *gin.Context) {
				page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
				pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "100"))
				category := ctx.Query("category") // 从 URL 查询参数获取分类名
				resp, err := adminClient.ListAllProducts(ctx.Request.Context(), &admin.ListAllProductsRequest{
					Page: int32(page), PageSize: int32(pageSize), Category: category,
				})
				if err != nil {
					response.Error(ctx, http.StatusInternalServerError, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 单个商品更新 (调价/修改库存)
			adminGroup.POST("/product/update", func(ctx *gin.Context) {
				var req admin.UpdateProductRequest
				if err := ctx.ShouldBindJSON(&req); err != nil {
					response.Error(ctx, http.StatusBadRequest, "参数错误")
					return
				}
				resp, err := adminClient.UpdateProduct(ctx, &req)
				if err != nil {
					response.Error(ctx, http.StatusInternalServerError, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 下架/彻底删除商品
			adminGroup.POST("/product/delete", func(ctx *gin.Context) {
				var req admin.DeleteProductRequest
				if err := ctx.ShouldBindJSON(&req); err != nil {
					response.Error(ctx, http.StatusBadRequest, "参数错误")
					return
				}
				resp, err := adminClient.DeleteProduct(ctx.Request.Context(), &req)
				if err != nil {
					response.Error(ctx, http.StatusInternalServerError, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 寿光市场调价：按分类批量修改价格系数
			adminGroup.POST("/product/batch-price", func(ctx *gin.Context) {
				var req admin.BatchPriceRequest
				if err := ctx.ShouldBindJSON(&req); err != nil {
					response.Error(ctx, http.StatusBadRequest, "参数格式错误")
					return
				}
				resp, err := adminClient.BatchUpdatePrice(ctx.Request.Context(), &req)
				if err != nil {
					response.Error(ctx, http.StatusInternalServerError, err.Error())
					return
				}
				response.Success(ctx, resp)
			})

			// 平台订单发货操作
			adminGroup.POST("/order/ship", func(ctx *gin.Context) {
				var req struct {
					OrderNo string `json:"order_no"`
				}
				if err := ctx.ShouldBindJSON(&req); err != nil {
					response.Error(ctx, http.StatusBadRequest, "参数错误")
					return
				}
				resp, err := adminClient.ShipOrder(ctx, &admin.ShipOrderRequest{OrderNo: req.OrderNo})
				if err != nil {
					response.Error(ctx, http.StatusInternalServerError, err.Error())
					return
				}
				response.Success(ctx, resp)
			})
		}
	}

	// 6. 启动网关监听
	addr := fmt.Sprintf(":%d", c.Service.Port)
	log.Printf("Gateway 网关启动成功: %s", addr)
	r.Run(addr)
}
