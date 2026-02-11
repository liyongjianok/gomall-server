package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"go-ecommerce/apps/user/model"
	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/user"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt" // [恢复] 加密库
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// [关键] 必须与 Gateway 保持一致
var jwtSecret = []byte("my_secret_key")

type server struct {
	user.UnimplementedUserServiceServer
	db *gorm.DB
}

func (s *server) Register(ctx context.Context, req *user.RegisterRequest) (*user.RegisterResponse, error) {
	var cnt int64
	s.db.Model(&model.User{}).Where("username = ?", req.Username).Count(&cnt)
	if cnt > 0 {
		return nil, status.Error(codes.AlreadyExists, "Username already exists")
	}

	// [修复] 密码加密存储
	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to encrypt password")
	}

	u := model.User{
		Username: req.Username,
		Password: string(hashedPwd), // 存入加密后的哈希值
		Mobile:   req.Mobile,
	}

	if err := s.db.Create(&u).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to create user")
	}

	return &user.RegisterResponse{Id: int64(u.ID)}, nil
}

func (s *server) Login(ctx context.Context, req *user.LoginRequest) (*user.LoginResponse, error) {
	var u model.User
	if err := s.db.Where("username = ?", req.Username).First(&u).Error; err != nil {
		return nil, status.Error(codes.NotFound, "User not found")
	}

	// 密码比对逻辑 (数据库里的Hash vs 输入的明文)
	err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.Password))
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "Invalid password")
	}

	// 生成 JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": u.ID,
		"exp":     time.Now().Add(time.Hour * 24 * 7).Unix(),
		"iss":     "go-ecommerce",
	})

	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to generate token")
	}

	return &user.LoginResponse{UserId: int64(u.ID), Token: tokenString}, nil
}

// GetUserInfo 获取用户信息
func (s *server) GetUserInfo(ctx context.Context, req *user.GetUserInfoRequest) (*user.GetUserInfoResponse, error) {
	var u model.User
	if err := s.db.First(&u, req.Id).Error; err != nil {
		return nil, status.Error(codes.NotFound, "用户不存在")
	}

	// 如果头像为空，给一个默认头像
	avatar := u.Avatar
	if avatar == "" {
		avatar = "https://cube.elemecdn.com/3/7c/3ea6beec64369c2642b92c6726f1epng.png"
	}

	return &user.GetUserInfoResponse{
		Id:       int64(u.ID),
		Username: u.Username,
		Mobile:   u.Mobile,
		Nickname: u.Nickname,
		Avatar:   avatar,
	}, nil
}

// UpdateUser 更新用户信息
func (s *server) UpdateUser(ctx context.Context, req *user.UpdateUserRequest) (*user.UpdateUserResponse, error) {
	var u model.User
	if err := s.db.First(&u, req.Id).Error; err != nil {
		return nil, status.Error(codes.NotFound, "用户不存在")
	}

	// 更新字段 (只更新非空字段)
	updates := make(map[string]interface{})
	if req.Nickname != "" {
		updates["nickname"] = req.Nickname
	}
	if req.Avatar != "" {
		updates["avatar"] = req.Avatar
	}
	if req.Mobile != "" {
		updates["mobile"] = req.Mobile
	}

	if err := s.db.Model(&u).Updates(updates).Error; err != nil {
		return nil, status.Error(codes.Internal, "更新失败")
	}

	return &user.UpdateUserResponse{Success: true}, nil
}

// 🔥 新增：修改密码
func (s *server) UpdatePassword(ctx context.Context, req *user.UpdatePasswordRequest) (*user.UpdatePasswordResponse, error) {
	var u model.User
	if err := s.db.First(&u, req.UserId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "用户不存在")
	}

	// 1. 验证旧密码
	err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.OldPassword))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "旧密码错误")
	}

	// 2. 加密新密码
	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.Internal, "加密失败")
	}

	// 3. 更新数据库
	if err := s.db.Model(&u).Update("password", string(hashedPwd)).Error; err != nil {
		return nil, status.Error(codes.Internal, "数据库更新失败")
	}

	return &user.UpdatePasswordResponse{Success: true}, nil
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 环境变量适配
	if v := os.Getenv("MYSQL_HOST"); v != "" {
		c.Mysql.Host = v
	}
	if v := os.Getenv("MYSQL_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Mysql.Port = p
		}
	}
	if v := os.Getenv("MYSQL_USER"); v != "" {
		c.Mysql.User = v
	}
	if v := os.Getenv("MYSQL_PASSWORD"); v != "" {
		c.Mysql.Password = v
	}
	if v := os.Getenv("MYSQL_DBNAME"); v != "" {
		c.Mysql.DbName = v
	}
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}

	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Failed to init mysql: %v", err)
	}
	db.AutoMigrate(&model.User{})

	addr := fmt.Sprintf(":%d", c.Service.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	err = discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	if err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	s := grpc.NewServer()
	user.RegisterUserServiceServer(s, &server{db: db})
	reflection.Register(s)

	log.Printf("User Service listening on %s", addr)

	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	s.GracefulStop()
}
