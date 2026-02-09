package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"

	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/product"

	"github.com/olivere/elastic/v7"
	amqp "github.com/rabbitmq/amqp091-go" // [新增] RabbitMQ
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

const (
	ProductIndex = "products"
	MQUrl        = "amqp://guest:guest@rabbitmq:5672/"
	SeckillQueue = "seckill.order.queue" // 秒杀队列名称
)

// Redis Lua 脚本 (保持不变)
const seckillScript = `
local stockKey = KEYS[1]
local userKey = KEYS[2]
local userId = ARGV[1]
if redis.call("SISMEMBER", userKey, userId) == 1 then
    return -1
end
local stock = tonumber(redis.call("GET", stockKey))
if stock == nil then
    return -2
end
if stock <= 0 then
    return 0
end
redis.call("DECR", stockKey)
redis.call("SADD", userKey, userId)
return 1
`

// 数据库模型 (保持不变)
type Product struct {
	ID          int64   `gorm:"primaryKey" json:"id"`
	Name        string  `gorm:"type:varchar(100)" json:"name"`
	Description string  `gorm:"type:text" json:"description"`
	CategoryID  int64   `gorm:"index" json:"category_id"`
	Picture     string  `gorm:"type:varchar(255)" json:"picture"`
	Price       float64 `gorm:"type:decimal(10,2)" json:"price"`
}

type Sku struct {
	ID        int64   `gorm:"primaryKey"`
	ProductID int64   `gorm:"index"`
	Name      string  `gorm:"type:varchar(100)"`
	Price     float64 `gorm:"type:decimal(10,2)"`
	Stock     int     `gorm:"type:int"`
	Picture   string  `gorm:"type:varchar(255)"`
}

// 秒杀消息结构体 (发送给 MQ)
type SeckillMessage struct {
	UserId int64 `json:"user_id"`
	SkuId  int64 `json:"sku_id"`
}

type server struct {
	product.UnimplementedProductServiceServer
	db     *gorm.DB
	esCli  *elastic.Client
	rdb    *redis.Client
	mqConn *amqp.Connection // [新增]
	mqCh   *amqp.Channel    // [新增]
}

// 初始化 RabbitMQ
func (s *server) initRabbitMQ() error {
	mqUrl := os.Getenv("RABBITMQ_URL")
	if mqUrl == "" {
		mqUrl = MQUrl
	}
	var err error
	s.mqConn, err = amqp.Dial(mqUrl)
	if err != nil {
		return err
	}
	s.mqCh, err = s.mqConn.Channel()
	if err != nil {
		return err
	}
	// 声明秒杀队列
	_, err = s.mqCh.QueueDeclare(
		SeckillQueue, // name
		true,         // durable
		false,        // delete when unused
		false,        // exclusive
		false,        // no-wait
		nil,          // args
	)
	return err
}

// 发送秒杀成功消息
func (s *server) sendSeckillMessage(userId, skuId int64) {
	msg := SeckillMessage{UserId: userId, SkuId: skuId}
	body, _ := json.Marshal(msg)

	err := s.mqCh.PublishWithContext(context.Background(),
		"",           // exchange
		SeckillQueue, // routing key
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
		})
	if err != nil {
		log.Printf("[MQ Error] Failed to send seckill message: %v", err)
	} else {
		log.Printf("[MQ] Sent seckill success message for User %d", userId)
	}
}

// syncProductsToES (保持不变，省略内容)
func (s *server) syncProductsToES() {
	log.Println("[ES] 开始全量同步商品数据...")
	var products []Product
	if err := s.db.Find(&products).Error; err != nil {
		log.Printf("[ES] 读取数据库失败: %v", err)
		return
	}
	for _, p := range products {
		_, err := s.esCli.Index().Index(ProductIndex).Id(fmt.Sprintf("%d", p.ID)).BodyJson(p).Do(context.Background())
		if err != nil {
			log.Printf("[ES] 同步商品 %d 失败: %v", p.ID, err)
		}
	}
	log.Printf("[ES] 同步完成，共 %d 条商品", len(products))
}

// ListProducts (保持不变)
func (s *server) ListProducts(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	if req.Query != "" {
		return s.searchFromES(ctx, req)
	}
	return s.listFromMySQL(ctx, req)
}

// listFromMySQL (保持不变)
func (s *server) listFromMySQL(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	var products []Product
	var total int64
	query := s.db.Model(&Product{})
	if req.CategoryId > 0 {
		query = query.Where("category_id = ?", req.CategoryId)
	}
	query.Count(&total)
	offset := (req.Page - 1) * req.PageSize
	query.Offset(int(offset)).Limit(int(req.PageSize)).Find(&products)
	var pbProducts []*product.Product
	for _, p := range products {
		pbProducts = append(pbProducts, &product.Product{Id: p.ID, Name: p.Name, Description: p.Description, Picture: p.Picture, Price: float32(p.Price), CategoryId: p.CategoryID})
	}
	return &product.ListProductsResponse{Products: pbProducts, Total: total}, nil
}

// searchFromES (保持不变)
func (s *server) searchFromES(ctx context.Context, req *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	q := elastic.NewMultiMatchQuery(req.Query, "name", "description")
	offset := (req.Page - 1) * req.PageSize
	searchResult, err := s.esCli.Search().Index(ProductIndex).Query(q).From(int(offset)).Size(int(req.PageSize)).Do(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "ES Error")
	}
	var pbProducts []*product.Product
	for _, hit := range searchResult.Hits.Hits {
		var p Product
		if err := json.Unmarshal(hit.Source, &p); err == nil {
			pbProducts = append(pbProducts, &product.Product{Id: p.ID, Name: p.Name, Description: p.Description, Picture: p.Picture, Price: float32(p.Price), CategoryId: p.CategoryID})
		}
	}
	return &product.ListProductsResponse{Products: pbProducts, Total: searchResult.TotalHits()}, nil
}

// GetProduct (保持不变)
func (s *server) GetProduct(ctx context.Context, req *product.GetProductRequest) (*product.GetProductResponse, error) {
	var sku Sku
	if err := s.db.First(&sku, req.Id).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Sku not found")
	}
	var p Product
	if err := s.db.First(&p, sku.ProductID).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Product not found")
	}
	return &product.GetProductResponse{Id: p.ID, Name: p.Name, Description: p.Description, Picture: p.Picture, Price: float32(sku.Price), CategoryId: p.CategoryID, SkuName: sku.Name, SkuId: sku.ID}, nil
}

// DecreaseStock (保持不变)
func (s *server) DecreaseStock(ctx context.Context, req *product.DecreaseStockRequest) (*product.DecreaseStockResponse, error) {
	tx := s.db.Begin()
	var sku Sku
	if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&sku, req.SkuId).Error; err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.NotFound, "Sku not found")
	}
	if sku.Stock < int(req.Count) {
		tx.Rollback()
		return nil, status.Error(codes.FailedPrecondition, "No stock")
	}
	sku.Stock -= int(req.Count)
	tx.Model(&sku).Update("stock", sku.Stock)
	tx.Commit()
	return &product.DecreaseStockResponse{Success: true}, nil
}

// RollbackStock (保持不变)
func (s *server) RollbackStock(ctx context.Context, req *product.RollbackStockRequest) (*product.RollbackStockResponse, error) {
	tx := s.db.Begin()
	var sku Sku
	if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&sku, req.SkuId).Error; err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.NotFound, "Sku not found")
	}
	sku.Stock += int(req.Count)
	tx.Model(&sku).Update("stock", sku.Stock)
	tx.Commit()
	return &product.RollbackStockResponse{Success: true}, nil
}

// SeckillProduct (核心修改：Redis 成功后 -> 发 MQ)
func (s *server) SeckillProduct(ctx context.Context, req *product.SeckillProductRequest) (*product.SeckillProductResponse, error) {
	stockKey := fmt.Sprintf("seckill:stock:%d", req.SkuId)
	userKey := fmt.Sprintf("seckill:user:%d", req.SkuId)

	// 1. Lua 脚本扣减 Redis 库存
	res, err := s.rdb.Eval(ctx, seckillScript, []string{stockKey, userKey}, req.UserId).Int()
	if err != nil {
		log.Printf("Redis error: %v", err)
		return nil, status.Error(codes.Internal, "Redis error")
	}

	switch res {
	case 1:
		log.Printf("[Seckill] User %d won SKU %d! Sending to MQ...", req.UserId, req.SkuId)
		// 2. [新增] 抢购成功，发送异步消息创建订单
		s.sendSeckillMessage(req.UserId, req.SkuId)
		return &product.SeckillProductResponse{Success: true}, nil
	case 0:
		return nil, status.Error(codes.ResourceExhausted, "手慢了，已被抢光")
	case -1:
		return nil, status.Error(codes.AlreadyExists, "您已经抢购过了")
	case -2:
		return nil, status.Error(codes.FailedPrecondition, "秒杀活动未开始")
	default:
		return nil, status.Error(codes.Unknown, "未知错误")
	}
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if v := os.Getenv("MYSQL_HOST"); v != "" {
		c.Mysql.Host = v
	}
	if v := os.Getenv("REDIS_ADDRESS"); v != "" {
		c.Redis.Address = v
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

	rdb := redis.NewClient(&redis.Options{Addr: c.Redis.Address, Password: c.Redis.Password, DB: c.Redis.Db})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	esCli, err := elastic.NewClient(elastic.SetURL(esAddr), elastic.SetSniff(false))
	if err != nil {
		log.Printf("Warning: Failed to connect to ES: %v", err)
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
	srv := &server{db: db, esCli: esCli, rdb: rdb}

	// [新增] 初始化 RabbitMQ
	if err := srv.initRabbitMQ(); err != nil {
		log.Printf("Warning: RabbitMQ init failed in Product Service: %v", err)
	} else {
		log.Println("RabbitMQ (Producer) initialized")
		defer srv.mqConn.Close()
		defer srv.mqCh.Close()
	}

	product.RegisterProductServiceServer(s, srv)
	reflection.Register(s)

	if esCli != nil {
		go srv.syncProductsToES()
	}

	log.Printf("Product Service listening on %s", addr)
	s.Serve(lis)
}
