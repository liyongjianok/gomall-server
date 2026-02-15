package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/admin"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"gorm.io/gorm"
)

type server struct {
	admin.UnimplementedAdminServiceServer
	dbUser    *gorm.DB
	dbProduct *gorm.DB
	dbOrder   *gorm.DB
}

// --- 数据大屏统计 ---
func (s *server) GetDashboardStats(ctx context.Context, req *admin.StatsRequest) (*admin.StatsResponse, error) {
	var totalSales, actualSales float64
	var oCount, uCount, pCount int64

	// 1. 总订单金额 (GMV)：不管什么状态，所有产生的订单总额
	s.dbOrder.Table("orders").Select("COALESCE(SUM(total_amount), 0)").Row().Scan(&totalSales)

	// 2. 实际成交额：只有状态 status >= 1 (已支付) 的订单额
	s.dbOrder.Table("orders").Where("status = ?", 1).Select("COALESCE(SUM(total_amount), 0)").Row().Scan(&actualSales)

	// 3. 基础计数
	s.dbOrder.Table("orders").Count(&oCount)
	s.dbUser.Table("users").Count(&uCount)
	s.dbProduct.Table("products").Count(&pCount)

	// 4. 统计品类分布
	var catStats []*admin.CategoryStat
	s.dbProduct.Table("products").Select("category as name, count(*) as value").Group("category").Scan(&catStats)

	// 5. 统计销售趋势 (以实际成交为准)
	var trendStats []*admin.TrendStat
	s.dbOrder.Table("orders").
		Select("DATE_FORMAT(created_at, '%m-%d') as date, SUM(total_amount) as amount").
		Where("created_at > ?", time.Now().AddDate(0, 0, -7)).
		Where("status >= ?", 1).
		Group("date").Order("date asc").Scan(&trendStats)

	return &admin.StatsResponse{
		TotalSales:    float32(totalSales),
		ActualSales:   float32(actualSales),
		OrderCount:    int32(oCount),
		UserCount:     int32(uCount),
		ProductCount:  int32(pCount),
		CategoryStats: catStats,
		SalesTrend:    trendStats,
	}, nil
}

// --- 用户管理 ---
func (s *server) ListUsers(ctx context.Context, req *admin.ListUsersRequest) (*admin.ListUsersResponse, error) {
	var users []struct {
		ID         int64
		Username   string
		Nickname   string
		Mobile     string
		Role       string
		CreatedAt  time.Time
		IsDisabled bool
	}
	var total int64
	s.dbUser.Table("users").Count(&total)
	s.dbUser.Table("users").Limit(int(req.PageSize)).Offset(int((req.Page - 1) * req.PageSize)).Find(&users)

	var res []*admin.UserInfo
	for _, u := range users {
		res = append(res, &admin.UserInfo{
			Id:         u.ID,
			Username:   u.Username,
			Nickname:   u.Nickname,
			Mobile:     u.Mobile,
			Role:       u.Role,
			IsDisabled: u.IsDisabled,
			CreatedAt:  u.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return &admin.ListUsersResponse{Users: res, Total: int32(total)}, nil
}

func (s *server) DeleteUser(ctx context.Context, req *admin.DeleteUserRequest) (*admin.DeleteUserResponse, error) {
	err := s.dbUser.Table("users").Where("id = ?", req.UserId).Delete(nil).Error
	return &admin.DeleteUserResponse{Success: err == nil}, err
}

func (s *server) ToggleUserStatus(ctx context.Context, req *admin.ToggleStatusRequest) (*admin.ToggleStatusResponse, error) {
	err := s.dbUser.Table("users").Where("id = ?", req.UserId).Update("is_disabled", req.Disabled).Error
	return &admin.ToggleStatusResponse{Success: err == nil}, err
}

// --- 商品管理 ---
func (s *server) ListAllProducts(ctx context.Context, req *admin.ListAllProductsRequest) (*admin.ListAllProductsResponse, error) {
	var prods []struct {
		ID      int64
		Name    string
		Price   float32
		Stock   int32
		Picture string
	}
	var total int64
	s.dbProduct.Table("products").Count(&total)
	s.dbProduct.Table("products").Limit(int(req.PageSize)).Offset(int((req.Page - 1) * req.PageSize)).Find(&prods)

	var res []*admin.AdminProductInfo
	for _, p := range prods {
		res = append(res, &admin.AdminProductInfo{Id: p.ID, Name: p.Name, Price: p.Price, Stock: p.Stock, Picture: p.Picture})
	}
	return &admin.ListAllProductsResponse{Products: res, Total: int32(total)}, nil
}

func (s *server) UpdateProduct(ctx context.Context, req *admin.UpdateProductRequest) (*admin.UpdateProductResponse, error) {
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	updates["price"] = req.Price
	updates["stock"] = req.Stock

	err := s.dbProduct.Table("products").Where("id = ?", req.Id).Updates(updates).Error
	return &admin.UpdateProductResponse{Success: err == nil}, err
}

func (s *server) DeleteProduct(ctx context.Context, req *admin.DeleteProductRequest) (*admin.DeleteProductResponse, error) {
	err := s.dbProduct.Table("products").Where("id = ?", req.Id).Delete(nil).Error
	return &admin.DeleteProductResponse{Success: err == nil}, err
}

func (s *server) BatchUpdatePrice(ctx context.Context, req *admin.BatchPriceRequest) (*admin.BatchPriceResponse, error) {
	// GORM 表达式更新：price = price * ratio
	err := s.dbProduct.Table("products").
		Where("category = ?", req.Category).
		Update("price", gorm.Expr("price * ?", req.Ratio)).Error
	return &admin.BatchPriceResponse{Success: err == nil}, err
}

// --- 订单管理 ---
func (s *server) ShipOrder(ctx context.Context, req *admin.ShipOrderRequest) (*admin.ShipOrderResponse, error) {
	err := s.dbOrder.Table("orders").Where("order_no = ? AND status = 1", req.OrderNo).Update("status", 3).Error
	return &admin.ShipOrderResponse{Success: err == nil}, err
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

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
