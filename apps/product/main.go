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
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// ES 索引名称
const ProductIndex = "products"

// Lua 脚本：原子扣减库存
// KEYS[1]: 库存Key (e.g., seckill:stock:1001)
// KEYS[2]: 用户去重Key (e.g., seckill:user:1001)
// ARGV[1]: 用户ID
const seckillScript = `
local stockKey = KEYS[1]
local userKey = KEYS[2]
local userId = ARGV[1]

-- 1. 检查用户是否已抢购 (去重)
if redis.call("SISMEMBER", userKey, userId) == 1 then
    return -1 -- 重复抢购
end

-- 2. 检查库存
local stock = tonumber(redis.call("GET", stockKey))
if stock == nil then
    return -2 -- 库存未预热
end
if stock <= 0 then
    return 0 -- 库存不足
end

-- 3. 扣减库存并记录用户
redis.call("DECR", stockKey)
redis.call("SADD", userKey, userId)
return 1 -- 抢购成功
`

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
	rdb   *redis.Client
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

// ListProducts 商品列表 (混合查询：有 query 走 ES，无 query 走 MySQL)
func (s *server) ListProducts(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	if req.Query != "" {
		return s.searchFromES(ctx, req)
	}
	return s.listFromMySQL(ctx, req)
}

// listFromMySQL 从 MySQL 查询列表
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
			SkuName:     "", // 列表页暂不展示 SKU
			SkuId:       0,
		})
	}

	return &product.ListProductsResponse{Products: pbProducts, Total: total}, nil
}

// searchFromES 从 ES 搜索
func (s *server) searchFromES(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	// 在 name 和 description 字段中搜索
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
		// 反序列化 JSON
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

// GetProduct 获取商品详情 (通过 SKU ID)
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
		Price:       float32(sku.Price), // 使用 SKU 价格
		CategoryId:  p.CategoryID,
		SkuName:     sku.Name,
		SkuId:       sku.ID,
	}, nil
}

// DecreaseStock 扣减库存 (DB 事务) - 用于普通下单
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

// RollbackStock 回滚库存 - 用于取消订单
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

// SeckillProduct 秒杀接口 (Redis Lua 脚本)
func (s *server) SeckillProduct(ctx context.Context, req *product.SeckillProductRequest) (*product.SeckillProductResponse, error) {
	stockKey := fmt.Sprintf("seckill:stock:%d", req.SkuId)
	userKey := fmt.Sprintf("seckill:user:%d", req.SkuId)

	// 执行 Lua 脚本
	// 结果: 1=成功, 0=库存不足, -1=重复抢购, -2=未预热
	res, err := s.rdb.Eval(ctx, seckillScript, []string{stockKey, userKey}, req.UserId).Int()
	if err != nil {
		log.Printf("Redis Seckill Error: %v", err)
		return nil, status.Error(codes.Internal, "Redis error")
	}

	switch res {
	case 1:
		log.Printf("[Seckill] User %d 抢到了 SKU %d!", req.UserId, req.SkuId)
		// 🚀 TODO: 这里应该发送 MQ 消息给 Order Service 异步创建订单
		// 为了演示方便，我们这里只返回成功，视为“抢购资格获取成功”
		return &product.SeckillProductResponse{Success: true}, nil
	case 0:
		return nil, status.Error(codes.ResourceExhausted, "手慢了，已被抢光")
	case -1:
		return nil, status.Error(codes.AlreadyExists, "您已经抢购过了")
	case -2:
		return nil, status.Error(codes.FailedPrecondition, "秒杀活动未开始 (库存未预热)")
	default:
		return nil, status.Error(codes.Unknown, "未知错误")
	}
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 1. 环境变量适配
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
	// Redis 地址
	if v := os.Getenv("REDIS_ADDRESS"); v != "" {
		c.Redis.Address = v
	}
	// ES 地址
	esAddr := "http://127.0.0.1:9200"
	if v := os.Getenv("ES_ADDRESS"); v != "" {
		esAddr = v
	}

	// 2. 初始化 MySQL
	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Failed to init mysql: %v", err)
	}
	db.AutoMigrate(&Product{}, &Sku{})

	// 3. 初始化 Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Address,
		Password: c.Redis.Password,
		DB:       c.Redis.Db,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	// 4. 初始化 ES 客户端
	esCli, err := elastic.NewClient(
		elastic.SetURL(esAddr),
		elastic.SetSniff(false),
	)
	if err != nil {
		log.Printf("[Warning] Failed to connect to ES: %v", err)
	} else {
		log.Println("Elasticsearch connected successfully")
	}

	// 5. 启动服务
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
	// 注入依赖
	srv := &server{db: db, esCli: esCli, rdb: rdb}
	product.RegisterProductServiceServer(s, srv)
	reflection.Register(s)

	// 启动时同步 ES 数据
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
