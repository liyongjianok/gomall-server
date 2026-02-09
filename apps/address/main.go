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

	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/address"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// Address 数据库模型
type Address struct {
	ID            int64  `gorm:"primaryKey"`
	UserID        int64  `gorm:"index"`
	Name          string `gorm:"type:varchar(50)"`
	Mobile        string `gorm:"type:varchar(20)"`
	Province      string `gorm:"type:varchar(50)"`
	City          string `gorm:"type:varchar(50)"`
	District      string `gorm:"type:varchar(50)"`
	DetailAddress string `gorm:"type:varchar(200)"`
}

type server struct {
	address.UnimplementedAddressServiceServer
	db *gorm.DB
}

// CreateAddress 新增
func (s *server) CreateAddress(ctx context.Context, req *address.CreateAddressRequest) (*address.CreateAddressResponse, error) {
	addr := Address{
		UserID:        req.UserId,
		Name:          req.Name,
		Mobile:        req.Mobile,
		Province:      req.Province,
		City:          req.City,
		District:      req.District,
		DetailAddress: req.DetailAddress,
	}

	if err := s.db.Create(&addr).Error; err != nil {
		log.Printf("CreateAddress DB Error: %v", err)
		return nil, status.Error(codes.Internal, "Failed to create address")
	}

	return &address.CreateAddressResponse{AddressId: addr.ID}, nil
}

// ListAddress 列表
func (s *server) ListAddress(ctx context.Context, req *address.ListAddressRequest) (*address.ListAddressResponse, error) {
	var addrs []Address
	if err := s.db.Where("user_id = ?", req.UserId).Find(&addrs).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to list addresses")
	}

	var pbAddrs []*address.Address
	for _, a := range addrs {
		pbAddrs = append(pbAddrs, &address.Address{
			Id:            a.ID,
			UserId:        a.UserID,
			Name:          a.Name,
			Mobile:        a.Mobile,
			Province:      a.Province,
			City:          a.City,
			District:      a.District,
			DetailAddress: a.DetailAddress,
		})
	}

	return &address.ListAddressResponse{Addresses: pbAddrs}, nil
}

// GetAddress 详情
func (s *server) GetAddress(ctx context.Context, req *address.GetAddressRequest) (*address.GetAddressResponse, error) {
	var a Address
	if err := s.db.First(&a, req.AddressId).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Address not found")
	}

	return &address.GetAddressResponse{
		Address: &address.Address{
			Id:            a.ID,
			UserId:        a.UserID,
			Name:          a.Name,
			Mobile:        a.Mobile,
			Province:      a.Province,
			City:          a.City,
			District:      a.District,
			DetailAddress: a.DetailAddress,
		},
	}, nil
}

// UpdateAddress 修改
func (s *server) UpdateAddress(ctx context.Context, req *address.UpdateAddressRequest) (*address.UpdateAddressResponse, error) {
	var a Address
	if err := s.db.First(&a, req.AddressId).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Address not found")
	}

	if a.UserID != req.UserId {
		return nil, status.Error(codes.PermissionDenied, "Not your address")
	}

	a.Name = req.Name
	a.Mobile = req.Mobile
	a.Province = req.Province
	a.City = req.City
	a.District = req.District
	a.DetailAddress = req.DetailAddress

	if err := s.db.Save(&a).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to update address")
	}

	return &address.UpdateAddressResponse{Success: true}, nil
}

// DeleteAddress 删除
func (s *server) DeleteAddress(ctx context.Context, req *address.DeleteAddressRequest) (*address.DeleteAddressResponse, error) {
	var a Address
	if err := s.db.First(&a, req.AddressId).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Address not found")
	}

	if a.UserID != req.UserId {
		return nil, status.Error(codes.PermissionDenied, "Not your address")
	}

	if err := s.db.Delete(&a).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to delete address")
	}

	return &address.DeleteAddressResponse{Success: true}, nil
}

func main() {
	// 1. 加载配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 环境变量覆盖配置 (适配 Docker)
	if v := os.Getenv("MYSQL_HOST"); v != "" {
		c.Mysql.Host = v
		log.Printf("Config Override: MYSQL_HOST used (%s)", v)
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
		c.Mysql.DbName = v // [修正] 这里改成了 DbName (大写 N)
	}
	// 覆盖 Service 配置
	if v := os.Getenv("SERVICE_NAME"); v != "" {
		c.Service.Name = v
	}
	if v := os.Getenv("SERVICE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Service.Port = p
		}
	}
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}

	// 3. 初始化 MySQL
	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Failed to init mysql: %v", err)
	}
	// 自动建表
	db.AutoMigrate(&Address{})

	addr := fmt.Sprintf(":%d", c.Service.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 4. 注册到 Consul
	err = discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	if err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	s := grpc.NewServer()
	address.RegisterAddressServiceServer(s, &server{db: db})
	reflection.Register(s)

	log.Printf("Address Service listening on %s", addr)

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
