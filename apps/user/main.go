package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go-ecommerce/apps/user/model"
	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/pkg/jwt" // [新增] 引入 JWT 工具包
	"go-ecommerce/pkg/utils"
	"go-ecommerce/proto/user"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type server struct {
	user.UnimplementedUserServiceServer
	db *gorm.DB // 持有 DB 连接
}

// Login 登录逻辑
func (s *server) Login(ctx context.Context, req *user.LoginRequest) (*user.LoginResponse, error) {
	var u model.User

	// 1. 查询用户
	if err := s.db.Where("username = ?", req.Username).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &user.LoginResponse{Code: 1, Msg: "User not found"}, nil
		}
		return nil, status.Error(codes.Internal, "Database error")
	}

	// 2. 校验密码
	if !utils.CheckPassword(req.Password, u.Password) {
		return &user.LoginResponse{Code: 2, Msg: "Incorrect password"}, nil
	}

	// 3. [修改] 生成真实的 JWT Token
	token, err := jwt.GenerateToken(int64(u.ID), u.Username)
	if err != nil {
		log.Printf("Error generating token: %v", err)
		return nil, status.Error(codes.Internal, "Failed to generate token")
	}

	return &user.LoginResponse{
		Code:   0,
		Msg:    "Login Success",
		Token:  token, // 返回真实的 Token
		UserId: int64(u.ID),
	}, nil
}

// Register 注册逻辑
func (s *server) Register(ctx context.Context, req *user.RegisterRequest) (*user.RegisterResponse, error) {
	// 1. 检查用户是否存在
	var count int64
	s.db.Model(&model.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		return &user.RegisterResponse{Code: 1, Msg: "Username already exists"}, nil
	}

	// 2. 密码加密
	hashedPwd, err := utils.HashPassword(req.Password)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to hash password")
	}

	// 3. 创建用户
	newUser := model.User{
		Username: req.Username,
		Password: hashedPwd,
		Mobile:   req.Mobile,
	}

	if err := s.db.Create(&newUser).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to create user")
	}

	return &user.RegisterResponse{
		Code: 0,
		Msg:  "Register Success",
	}, nil
}

func main() {
	// 1. 加载配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 初始化数据库
	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Failed to init mysql: %v", err)
	}

	// 自动建表 (Auto Migrate)
	// GORM 会根据 model 自动在 MySQL 中创建 users 表
	if err := db.AutoMigrate(&model.User{}); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	// 3. 监听端口
	addr := fmt.Sprintf(":%d", c.Service.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 4. 注册到 Consul
	// 注意：在 Docker 环境中，确保 RegisterService 内部获取的是容器 IP
	err = discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	if err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	// 5. 启动 gRPC Server
	s := grpc.NewServer()
	// 将 db 注入到 server 结构体中
	user.RegisterUserServiceServer(s, &server{db: db})
	reflection.Register(s)

	log.Printf("User Service listening on %s", addr)

	// 6. 优雅退出
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	s.GracefulStop()
}
