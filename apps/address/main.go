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

	_ "github.com/go-sql-driver/mysql"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// 定义数据库模型 (跟数据库表结构对应)
type Address struct {
	ID            int64  `gorm:"primaryKey"`
	UserID        int64  `gorm:"index"`
	Name          string `gorm:"type:varchar(50)"`
	Mobile        string `gorm:"type:varchar(20)"`
	Province      string `gorm:"type:varchar(50)"`
	City          string `gorm:"type:varchar(50)"`
	District      string `gorm:"type:varchar(50)"`
	DetailAddress string `gorm:"type:varchar(255)"`
	IsDefault     bool   `gorm:"default:false"`
}

type server struct {
	address.UnimplementedAddressServiceServer
	db *gorm.DB
}

// 1. 新增地址
func (s *server) CreateAddress(ctx context.Context, req *address.CreateAddressRequest) (*address.CreateAddressResponse, error) {
	var count int64
	s.db.Model(&Address{}).Where("user_id = ?", req.UserId).Count(&count)

	addr := Address{
		UserID:        req.UserId,
		Name:          req.Name,
		Mobile:        req.Mobile,
		Province:      req.Province,
		City:          req.City,
		District:      req.District,
		DetailAddress: req.DetailAddress,
		IsDefault:     count == 0, // 如果是首个地址，默认设为 true
	}
	if err := s.db.Create(&addr).Error; err != nil {
		return nil, status.Error(codes.Internal, "Database error")
	}
	return &address.CreateAddressResponse{AddressId: addr.ID}, nil
}

// 2. 获取地址列表
func (s *server) ListAddress(ctx context.Context, req *address.ListAddressRequest) (*address.ListAddressResponse, error) {
	var addrs []Address
	if err := s.db.Where("user_id = ?", req.UserId).Find(&addrs).Error; err != nil {
		return nil, status.Error(codes.Internal, "Database error")
	}

	var respAddrs []*address.AddressInfo
	for _, a := range addrs {
		respAddrs = append(respAddrs, &address.AddressInfo{
			Id:            a.ID,
			Name:          a.Name,
			Mobile:        a.Mobile,
			Province:      a.Province,
			City:          a.City,
			District:      a.District,
			DetailAddress: a.DetailAddress,
			IsDefault:     a.IsDefault,
		})
	}
	return &address.ListAddressResponse{Addresses: respAddrs}, nil
}

// 3. 获取单个地址 (下单时用)
func (s *server) GetAddress(ctx context.Context, req *address.GetAddressRequest) (*address.GetAddressResponse, error) {
	var a Address
	if err := s.db.First(&a, req.AddressId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Address not found")
	}
	return &address.GetAddressResponse{
		Address: &address.AddressInfo{
			Id:            a.ID,
			Name:          a.Name,
			Mobile:        a.Mobile,
			Province:      a.Province,
			City:          a.City,
			District:      a.District,
			DetailAddress: a.DetailAddress,
			IsDefault:     a.IsDefault,
		},
	}, nil
}

// 4. 🔥 修复重点：修改地址
func (s *server) UpdateAddress(ctx context.Context, req *address.UpdateAddressRequest) (*address.UpdateAddressResponse, error) {
	var addr Address
	// 先查询是否存在，且属于该用户 (安全检查)
	if err := s.db.Where("id = ? AND user_id = ?", req.Id, req.UserId).First(&addr).Error; err != nil {
		return nil, status.Error(codes.NotFound, "地址不存在或无权修改")
	}

	// 更新字段
	addr.Name = req.Name
	addr.Mobile = req.Mobile
	addr.Province = req.Province
	addr.City = req.City
	addr.District = req.District
	addr.DetailAddress = req.DetailAddress

	// 保存
	if err := s.db.Save(&addr).Error; err != nil {
		return nil, status.Error(codes.Internal, "更新数据库失败")
	}

	return &address.UpdateAddressResponse{Success: true}, nil
}

// 5. 🔥 修复重点：删除地址
func (s *server) DeleteAddress(ctx context.Context, req *address.DeleteAddressRequest) (*address.DeleteAddressResponse, error) {
	// 直接删除，带上 UserId 防止删错别人的
	result := s.db.Where("id = ? AND user_id = ?", req.AddressId, req.UserId).Delete(&Address{})
	if result.Error != nil {
		return nil, status.Error(codes.Internal, "数据库错误")
	}
	if result.RowsAffected == 0 {
		return nil, status.Error(codes.NotFound, "地址不存在或无权删除")
	}
	return &address.DeleteAddressResponse{Success: true}, nil
}

// 6. 设置默认地址 (核心逻辑：排他性更新)
func (s *server) SetDefaultAddress(ctx context.Context, req *address.SetDefaultAddressRequest) (*address.SetDefaultAddressResponse, error) {
	// 开启事务
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 1. 先把该用户下所有的地址都设为非默认
		if err := tx.Model(&Address{}).Where("user_id = ?", req.UserId).Update("is_default", false).Error; err != nil {
			return err
		}

		// 2. 把指定的地址设为默认
		result := tx.Model(&Address{}).Where("id = ? AND user_id = ?", req.AddressId, req.UserId).Update("is_default", true)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("地址不存在")
		}
		return nil
	})

	if err != nil {
		log.Printf("[Address] 设置默认地址失败: %v", err)
		return nil, status.Errorf(codes.Internal, "设置默认地址失败: %v", err)
	}

	return &address.SetDefaultAddressResponse{Success: true}, nil
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
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}

	// 数据库连接
	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Database init failed: %v", err)
	}
	db.AutoMigrate(&Address{})

	// 启动 gRPC
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", c.Service.Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	address.RegisterAddressServiceServer(s, &server{db: db})
	reflection.Register(s)

	// 注册 Consul
	err = discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	if err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	log.Printf("Address Service listening on :%d", c.Service.Port)

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
