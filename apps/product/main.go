package main

import (
	"context"
	"encoding/json"
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
	"go-ecommerce/proto/product"

	"github.com/olivere/elastic/v7"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// ES 索引名称
const ProductIndex = "products"

// Product 数据库模型
type Product struct {
	ID          int64   `gorm:"primaryKey" json:"id"`
	Name        string  `gorm:"type:varchar(100)" json:"name"`
	Description string  `gorm:"type:text" json:"description"`
	CategoryID  int64   `gorm:"index" json:"category_id"`
	Picture     string  `gorm:"type:varchar(255)" json:"picture"`
	Price       float64 `gorm:"type:decimal(10,2)" json:"price"`
}

// Sku 数据库模型
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
	db    *gorm.DB
	esCli *elastic.Client
}

// syncProductsToES 将 MySQL 数据同步到 ES
func (s *server) syncProductsToES() {
	log.Println("[ES] 开始全量同步商品数据...")
	var products []Product
	if err := s.db.Find(&products).Error; err != nil {
		log.Printf("[ES] 读取数据库失败: %v", err)
		return
	}

	for _, p := range products {
		_, err := s.esCli.Index().
			Index(ProductIndex).
			Id(fmt.Sprintf("%d", p.ID)).
			BodyJson(p).
			Do(context.Background())
		if err != nil {
			log.Printf("[ES] 同步商品 %d 失败: %v", p.ID, err)
		}
	}
	log.Printf("[ES] 同步完成，共 %d 条商品", len(products))
}

func (s *server) ListProducts(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	// 如果有搜索词，走 ES
	if req.Query != "" {
		return s.searchFromES(ctx, req)
	}
	// 否则走 MySQL
	return s.listFromMySQL(ctx, req)
}

func (s *server) listFromMySQL(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
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
			SkuName:     "",
			SkuId:       0,
		})
	}

	return &product.ListProductsResponse{Products: pbProducts, Total: total}, nil
}

func (s *server) searchFromES(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	q := elastic.NewMultiMatchQuery(req.Query, "name", "description")
	offset := (req.Page - 1) * req.PageSize

	searchResult, err := s.esCli.Search().
		Index(ProductIndex).
		Query(q).
		From(int(offset)).Size(int(req.PageSize)).
		Do(ctx)

	if err != nil {
		log.Printf("ES Search Error: %v", err)
		return nil, status.Error(codes.Internal, "Search engine error")
	}

	var pbProducts []*product.Product
	for _, hit := range searchResult.Hits.Hits {
		var p Product
		if err := json.Unmarshal(hit.Source, &p); err == nil {
			pbProducts = append(pbProducts, &product.Product{
				Id:          p.ID,
				Name:        p.Name,
				Description: p.Description,
				Picture:     p.Picture,
				Price:       float32(p.Price),
				CategoryId:  p.CategoryID,
				SkuName:     "",
				SkuId:       0,
			})
		}
	}

	return &product.ListProductsResponse{
		Products: pbProducts,
		Total:    searchResult.TotalHits(),
	}, nil
}

func (s *server) GetProduct(ctx context.Context, req *product.GetProductRequest) (*product.GetProductResponse, error) {
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
		Picture:     p.Picture,
		Price:       float32(sku.Price),
		CategoryId:  p.CategoryID,
		SkuName:     sku.Name,
		SkuId:       sku.ID,
	}, nil
}

func (s *server) DecreaseStock(ctx context.Context, req *product.DecreaseStockRequest) (*product.DecreaseStockResponse, error) {
	tx := s.db.Begin()
	var sku Sku
	if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&sku, req.SkuId).Error; err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.NotFound, "Sku not found")
	}
	if sku.Stock < int(req.Count) {
		tx.Rollback()
		return nil, status.Error(codes.FailedPrecondition, "Stock not sufficient")
	}
	sku.Stock -= int(req.Count)
	if err := tx.Model(&sku).Update("stock", sku.Stock).Error; err != nil {
		tx.Rollback()
		return nil, status.Error(codes.Internal, "Failed to update stock")
	}
	tx.Commit()
	return &product.DecreaseStockResponse{Success: true}, nil
}

func (s *server) RollbackStock(ctx context.Context, req *product.RollbackStockRequest) (*product.RollbackStockResponse, error) {
	tx := s.db.Begin()
	var sku Sku
	if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&sku, req.SkuId).Error; err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.NotFound, "Sku not found")
	}
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

	esAddr := "http://127.0.0.1:9200"
	if v := os.Getenv("ES_ADDRESS"); v != "" {
		esAddr = v
	}

	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Failed to init mysql: %v", err)
	}
	db.AutoMigrate(&Product{}, &Sku{})

	// 此时本地 go.mod 必须已经包含 olivere/elastic/v7
	esCli, err := elastic.NewClient(
		elastic.SetURL(esAddr),
		elastic.SetSniff(false),
	)
	if err != nil {
		log.Printf("Warning: Failed to connect to ES: %v", err)
	} else {
		log.Println("Elasticsearch connected successfully")
	}

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
	srv := &server{db: db, esCli: esCli}
	product.RegisterProductServiceServer(s, srv)
	reflection.Register(s)

	if esCli != nil {
		go srv.syncProductsToES()
	}

	log.Printf("Product Service listening on %s", addr)
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
