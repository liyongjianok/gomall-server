package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"go-ecommerce/pkg/config"
	"go-ecommerce/proto/user"

	"github.com/gin-gonic/gin"
	_ "github.com/mbobakov/grpc-consul-resolver" // 必须导入，否则无法解析 consul://
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// 1. 加载配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 初始化 gRPC 连接 (使用 Consul 服务发现)
	// 格式: consul://[consul_address]/[service_name]?wait=14s
	target := fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "user-service")

	log.Printf("Connecting to User Service via: %s", target)

	conn, err := grpc.Dial(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	userClient := user.NewUserServiceClient(conn)

	// 3. 启动 Gin
	r := gin.Default()
	v1 := r.Group("/api/v1")
	{
		// ----------------------
		// 登录接口
		// ----------------------
		v1.POST("/user/login", func(ctx *gin.Context) {
			var req struct {
				Username string `json:"username" binding:"required"`
				Password string `json:"password" binding:"required"`
			}
			if err := ctx.ShouldBindJSON(&req); err != nil {
				ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// 调用 gRPC
			rpcCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			resp, err := userClient.Login(rpcCtx, &user.LoginRequest{
				Username: req.Username,
				Password: req.Password,
			})
			if err != nil {
				log.Printf("Login RPC Error: %v", err)
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Login Service Unavailable"})
				return
			}

			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})

		// ----------------------
		// 注册接口 (之前缺少的)
		// ----------------------
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
				log.Printf("Register RPC Error: %v", err)
				// 区分是重复注册还是系统错误
				ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// 如果业务逻辑返回非0 (例如用户名已存在)
			if resp.Code != 0 {
				ctx.JSON(http.StatusOK, gin.H{"code": resp.Code, "msg": resp.Msg})
				return
			}

			ctx.JSON(http.StatusOK, gin.H{"data": resp})
		})
	}

	addr := fmt.Sprintf(":%d", c.Service.Port)
	log.Printf("Gateway running on %s", addr)
	r.Run(addr)
}
