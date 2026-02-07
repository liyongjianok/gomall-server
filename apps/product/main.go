package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go-ecommerce/apps/product/model"
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

type server struct {
	product.UnimplementedProductServiceServer
	db *gorm.DB
}

// ListProducts 获取商品列表 (支持分页和分类筛选)
func (s *server) ListProducts(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	var products []model.Product
	var total int64

	query := s.db.Model(&model.Product{})

	// 1. 如果传了分类ID，进行筛选
	if req.CategoryId > 0 {
		query = query.Where("category_id = ?", req.CategoryId)
	}

	// 2. 统计总数
	query.Count(&total)

	// 3. 分页查询
	offset := (req.Page - 1) * req.PageSize
	if err := query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&products).Error; err != nil {
		return nil, status.Error(codes.Internal, "Database error")
	}

	// 4. 转换为 Protobuf 格式
	var protoProducts []*product.Product
	for _, p := range products {
		protoProducts = append(protoProducts, &product.Product{
			Id:          int64(p.ID),
			Name:        p.Name,
			Description: p.Description,
			Picture:     p.Picture,
			Price:       p.Price,
			CategoryId:  p.CategoryID,
		})
	}

	return &product.ListProductsResponse{
		Products: protoProducts,
		Total:    total,
	}, nil
}

// GetProduct 获取商品详情 (包含 SKU)
func (s *server) GetProduct(ctx context.Context, req *product.GetProductRequest) (*product.GetProductResponse, error) {
	// 1. 查商品主体 (SPU)
	var p model.Product
	if err := s.db.First(&p, req.Id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, status.Error(codes.NotFound, "Product not found")
		}
		return nil, status.Error(codes.Internal, "Database error")
	}

	// 2. 查该商品下的所有规格 (SKUs)
	var skus []model.Sku
	if err := s.db.Where("product_id = ?", p.ID).Find(&skus).Error; err != nil {
		return nil, status.Error(codes.Internal, "Database error fetching SKUs")
	}

	// 3. 组装响应
	protoProduct := &product.Product{
		Id:          int64(p.ID),
		Name:        p.Name,
		Description: p.Description,
		Picture:     p.Picture,
		Price:       p.Price,
		CategoryId:  p.CategoryID,
	}

	var protoSkus []*product.Sku
	for _, sku := range skus {
		protoSkus = append(protoSkus, &product.Sku{
			Id:        int64(sku.ID),
			ProductId: int64(sku.ProductID),
			Name:      sku.Name,
			Price:     sku.Price,
			Stock:     int32(sku.Stock),
			Picture:   sku.Picture,
		})
	}

	return &product.GetProductResponse{
		Product: protoProduct,
		Skus:    protoSkus,
	}, nil
}

// DecreaseStock 扣减库存 (事务处理)
func (s *server) DecreaseStock(ctx context.Context, req *product.DecreaseStockRequest) (*product.DecreaseStockResponse, error) {
	// 开启事务
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, item := range req.Requests {
		var sku model.Sku
		// 悲观锁 (FOR UPDATE) 锁住这行记录，防止并发超卖
		if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&sku, item.SkuId).Error; err != nil {
			tx.Rollback()
			return nil, status.Error(codes.NotFound, "Sku not found")
		}

		if sku.Stock < int(item.Count) {
			tx.Rollback()
			return nil, status.Error(codes.FailedPrecondition, "Stock not enough")
		}

		// 扣减
		sku.Stock -= int(item.Count)
		if err := tx.Save(&sku).Error; err != nil {
			tx.Rollback()
			return nil, status.Error(codes.Internal, "Failed to update stock")
		}
	}

	tx.Commit()
	return &product.DecreaseStockResponse{Success: true}, nil
}

func main() {
	// 1. 加载配置 (假设 product 和 user 用类似的配置结构，或复用)
	// 这里为了方便，我们直接读取 config.yaml，你需要在 apps/product 下创建它
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 初始化数据库 (连接 db_product)
	// 注意：这里需要去修改 config.yaml 里的 dbname 为 db_product
	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Failed to init mysql: %v", err)
	}

	// 3. 监听端口
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

	// 5. 启动 gRPC Server
	grpcServer := grpc.NewServer()
	product.RegisterProductServiceServer(grpcServer, &server{db: db})
	reflection.Register(grpcServer)

	log.Printf("Product Service listening on %s", addr)

	// 6. 优雅退出
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	grpcServer.GracefulStop()
}
