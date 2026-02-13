package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/admin"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type server struct {
	admin.UnimplementedAdminServiceServer
	dbUser    *gorm.DB
	dbProduct *gorm.DB
	dbOrder   *gorm.DB
}

// --- 核心业务实现 ---

func (s *server) GetDashboardStats(ctx context.Context, req *admin.StatsRequest) (*admin.StatsResponse, error) {
	var sales float64
	var oCount, uCount, pCount int64
	s.dbOrder.Table("orders").Where("status = ?", 1).Select("SUM(total_amount)").Row().Scan(&sales)
	s.dbOrder.Table("orders").Count(&oCount)
	s.dbUser.Table("users").Count(&uCount)
	s.dbProduct.Table("products").Count(&pCount)
	return &admin.StatsResponse{
		TotalSales: float32(sales), OrderCount: int32(oCount),
		UserCount: int32(uCount), ProductCount: int32(pCount),
	}, nil
}

func (s *server) ListUsers(ctx context.Context, req *admin.ListUsersRequest) (*admin.ListUsersResponse, error) {
	var users []struct {
		ID                                          int64
		Username, Nickname, Mobile, Role, CreatedAt string
		IsDisabled                                  bool
	}
	var total int64
	s.dbUser.Table("users").Count(&total)
	s.dbUser.Table("users").Limit(int(req.PageSize)).Offset(int((req.Page - 1) * req.PageSize)).Find(&users)
	var res []*admin.UserInfo
	for _, u := range users {
		res = append(res, &admin.UserInfo{
			Id: u.ID, Username: u.Username, Nickname: u.Nickname,
			Mobile: u.Mobile, Role: u.Role, IsDisabled: u.IsDisabled, CreatedAt: u.CreatedAt,
		})
	}
	return &admin.ListUsersResponse{Users: res, Total: int32(total)}, nil
}

func (s *server) DeleteUser(ctx context.Context, req *admin.DeleteUserRequest) (*admin.DeleteUserResponse, error) {
	// 管理员操作，直接从库中删除（或者你可以改为软删除）
	err := s.dbUser.Table("users").Delete(&struct{ ID int64 }{ID: req.UserId}).Error
	if err != nil {
		return nil, status.Errorf(codes.Internal, "删除用户失败: %v", err)
	}
	return &admin.DeleteUserResponse{Success: true}, nil
}

func (s *server) ToggleUserStatus(ctx context.Context, req *admin.ToggleStatusRequest) (*admin.ToggleStatusResponse, error) {
	err := s.dbUser.Table("users").Where("id = ?", req.UserId).Update("is_disabled", req.Disabled).Error
	return &admin.ToggleStatusResponse{Success: err == nil}, err
}

func (s *server) ListAllProducts(ctx context.Context, req *admin.ListAllProductsRequest) (*admin.ListAllProductsResponse, error) {
	var prods []struct {
		ID    int64
		Name  string
		Price float32
		Stock int32
	}
	var total int64
	s.dbProduct.Table("products").Count(&total)
	s.dbProduct.Table("products").Limit(int(req.PageSize)).Offset(int((req.Page - 1) * req.PageSize)).Find(&prods)
	var res []*admin.AdminProductInfo
	for _, p := range prods {
		res = append(res, &admin.AdminProductInfo{Id: p.ID, Name: p.Name, Price: p.Price, Stock: p.Stock})
	}
	return &admin.ListAllProductsResponse{Products: res, Total: int32(total)}, nil
}

func (s *server) UpdateProduct(ctx context.Context, req *admin.UpdateProductRequest) (*admin.UpdateProductResponse, error) {
	err := s.dbProduct.Table("products").Where("id = ?", req.Id).Updates(map[string]interface{}{
		"price": req.Price, "stock": req.Stock,
	}).Error
	return &admin.UpdateProductResponse{Success: err == nil}, err
}

func (s *server) ShipOrder(ctx context.Context, req *admin.ShipOrderRequest) (*admin.ShipOrderResponse, error) {
	err := s.dbOrder.Table("orders").Where("order_no = ? AND status = 1", req.OrderNo).Update("status", 3).Error
	return &admin.ShipOrderResponse{Success: err == nil}, err
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 根据配置初始化三个数据库连接
	// 这里的 pkg/database.InitMySQL 应支持传入不同的库名，或者我们手动构建 DSN
	// 简单起见，我们基于 c.Mysql 基础配置构建不同库的连接
	connect := func(dbName string) *gorm.DB {
		baseCfg := c.Mysql
		baseCfg.DbName = dbName
		db, err := database.InitMySQL(baseCfg)
		if err != nil {
			log.Fatalf("连接数据库 %s 失败: %v", dbName, err)
		}
		return db
	}

	dbU := connect("db_user")
	dbP := connect("db_product")
	dbO := connect("db_order")

	lis, err := net.Listen("tcp", ":50058")
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}

	s := grpc.NewServer()
	admin.RegisterAdminServiceServer(s, &server{dbUser: dbU, dbProduct: dbP, dbOrder: dbO})
	reflection.Register(s)

	// 注册到 Consul
	consulAddr := os.Getenv("CONSUL_ADDRESS")
	if consulAddr == "" {
		consulAddr = c.Consul.Address
	}
	discovery.RegisterService("admin-service", 50058, consulAddr)

	log.Println("Admin Service 启动成功: :50058")

	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("运行失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	s.GracefulStop()
}
