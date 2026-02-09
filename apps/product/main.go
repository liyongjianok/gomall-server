package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/product"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// 定义数据库模型 (跟数据库表结构对应)
type Product struct {
	ID          int64   `gorm:"primaryKey"`
	Name        string  `gorm:"type:varchar(100)"`
	Description string  `gorm:"type:text"`
	CategoryID  int64   `gorm:"index"`
	Picture     string  `gorm:"type:varchar(255)"`
	Price       float64 `gorm:"type:decimal(10,2)"`
}

type Sku struct {
	ID        int64   `gorm:"primaryKey"`
	ProductID int64   `gorm:"index"`
	Name      string  `gorm:"type:varchar(100)"`
	Price     float64 `gorm:"type:decimal(10,2)"`
	Stock     int     `gorm:"type:int"`
	Picture   string  `gorm:"type:varchar(255)"`
}

type server struct {
	product.UnimplementedProductServiceServer
	db *gorm.DB
}

func (s *server) ListProducts(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	var products []Product
	var total int64

	query := s.db.Model(&Product{})
	if req.CategoryId > 0 {
		query = query.Where("category_id = ?", req.CategoryId)
	}

	query.Count(&total)

	offset := (req.Page - 1) * req.PageSize
	if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&products).Error; err != nil {
		return nil, status.Error(codes.Internal, "Database error")
	}

	var pbProducts []*product.Product
	for _, p := range products {
		pbProducts = append(pbProducts, &product.Product{
			Id:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Picture:     p.Picture,
			Price:       float32(p.Price),
			CategoryId:  p.CategoryID,
		})
	}

	return &product.ListProductsResponse{Products: pbProducts, Total: total}, nil
}

func (s *server) GetProduct(ctx context.Context, req *product.GetProductRequest) (*product.GetProductResponse, error) {
	// 这里的逻辑是：req.Id 实际上是 SKU ID
	var sku Sku
	if err := s.db.First(&sku, req.Id).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Sku not found: %d", req.Id)
	}

	var p Product
	if err := s.db.First(&p, sku.ProductID).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Product not found: %d", sku.ProductID)
	}

	return &product.GetProductResponse{
		Id:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Picture:     p.Picture,          // 商品图
		Price:       float32(sku.Price), // 使用 SKU 的价格
		CategoryId:  p.CategoryID,
		SkuName:     sku.Name,
		SkuId:       sku.ID,
	}, nil
}

func (s *server) DecreaseStock(ctx context.Context, req *product.DecreaseStockRequest) (*product.DecreaseStockResponse, error) {
	// 开启事务
	tx := s.db.Begin()

	var sku Sku
	// 锁定行 (FOR UPDATE) 防止并发超卖
	if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&sku, req.SkuId).Error; err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.NotFound, "Sku not found")
	}

	if sku.Stock < int(req.Count) {
		tx.Rollback()
		return nil, status.Error(codes.FailedPrecondition, "Stock not sufficient")
	}

	// 扣减
	sku.Stock -= int(req.Count)
	if err := tx.Model(&sku).Update("stock", sku.Stock).Error; err != nil {
		tx.Rollback()
		return nil, status.Error(codes.Internal, "Failed to update stock")
	}

	tx.Commit()
	return &product.DecreaseStockResponse{Success: true}, nil
}

// RollbackStock 归还库存
func (s *server) RollbackStock(ctx context.Context, req *product.RollbackStockRequest) (*product.RollbackStockResponse, error) {
	tx := s.db.Begin()

	var sku Sku
	// 同样先锁定行，防止并发问题
	if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&sku, req.SkuId).Error; err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.NotFound, "Sku not found")
	}

	// 归还库存 (加法)
	sku.Stock += int(req.Count)
	if err := tx.Model(&sku).Update("stock", sku.Stock).Error; err != nil {
		tx.Rollback()
		return nil, status.Error(codes.Internal, "Failed to rollback stock")
	}

	tx.Commit()
	return &product.RollbackStockResponse{Success: true}, nil
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Failed to init mysql: %v", err)
	}
	db.AutoMigrate(&Product{}, &Sku{})

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
	product.RegisterProductServiceServer(s, &server{db: db})
	reflection.Register(s)

	log.Printf("Product Service listening on %s", addr)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
