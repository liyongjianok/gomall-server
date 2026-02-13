package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/pkg/tracer"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/review"

	_ "github.com/mbobakov/grpc-consul-resolver"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// Review 数据库模型映射
type Review struct {
	ID           int64  `gorm:"primaryKey"`
	UserID       int64  `gorm:"index"`
	OrderNo      string `gorm:"type:varchar(64);uniqueIndex:uni_order_sku"`
	SkuID        int64  `gorm:"uniqueIndex:uni_order_sku"`
	ProductID    int64  `gorm:"index"`
	Content      string `gorm:"type:text"`
	Images       string `gorm:"type:json"` // JSON数组字符串
	Star         int32  `gorm:"type:tinyint(1);default:5"`
	IsAnonymous  bool   `gorm:"type:tinyint(1);default:0"`
	UserNickname string `gorm:"type:varchar(255)"`
	UserAvatar   string `gorm:"type:mediumtext"`
	SkuName      string `gorm:"type:varchar(255)"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

type server struct {
	review.UnimplementedReviewServiceServer
	db          *gorm.DB
	orderClient order.OrderServiceClient // 增加订单客户端引用
}

// CreateReview 创建评价
func (s *server) CreateReview(ctx context.Context, req *review.CreateReviewRequest) (*review.CreateReviewResponse, error) {
	imagesBytes, _ := json.Marshal(req.Images)
	rev := Review{
		UserID:       req.UserId,
		OrderNo:      req.OrderNo,
		SkuID:        req.SkuId,
		ProductID:    req.ProductId,
		Content:      req.Content,
		Star:         req.Star,
		Images:       string(imagesBytes),
		UserNickname: req.UserNickname,
		UserAvatar:   req.UserAvatar,
		SkuName:      req.SkuName,
	}

	if err := s.db.Create(&rev).Error; err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "创建评价失败: %v", err)
	}

	// 同步调用 Order Service 更新状态，方便排查错误
	log.Printf("[Review] 评价成功，准备更新订单状态: %s", req.OrderNo)
	_, err := s.orderClient.UpdateItemReviewStatus(ctx, &order.UpdateItemReviewStatusRequest{
		OrderNo:    req.OrderNo,
		SkuId:      req.SkuId,
		IsReviewed: true,
	})

	if err != nil {
		// 即使更新状态失败，我们也不拦截评价结果，但要打印出来看为什么失败
		log.Printf("[Critical] 调用 OrderService 失败: %v", err)
	}

	return &review.CreateReviewResponse{ReviewId: rev.ID}, nil
}

// ListReviews 获取商品评价列表
func (s *server) ListReviews(ctx context.Context, req *review.ListReviewsRequest) (*review.ListReviewsResponse, error) {
	var reviews []Review
	var total int64

	query := s.db.Model(&Review{}).Where("product_id = ?", req.ProductId)
	query.Count(&total)

	offset := (req.Page - 1) * req.PageSize
	query.Order("created_at desc").Offset(int(offset)).Limit(int(req.PageSize)).Find(&reviews)

	var pbReviews []*review.ReviewInfo
	var totalStar int64

	for _, r := range reviews {
		totalStar += int64(r.Star)
		var imgs []string
		_ = json.Unmarshal([]byte(r.Images), &imgs)

		pbReviews = append(pbReviews, &review.ReviewInfo{
			Id:           r.ID,
			UserId:       r.UserID,
			UserNickname: r.UserNickname,
			UserAvatar:   r.UserAvatar,
			Content:      r.Content,
			Star:         r.Star,
			Images:       imgs,
			CreatedAt:    r.CreatedAt.Format("2006-01-02 15:04:05"),
			SkuName:      r.SkuName,
		})
	}

	var avg float32 = 5.0
	if total > 0 {
		avg = float32(totalStar) / float32(total)
	}

	return &review.ListReviewsResponse{
		Reviews:     pbReviews,
		Total:       total,
		AverageStar: avg,
	}, nil
}

// CheckReviewStatus 检查评价状态
func (s *server) CheckReviewStatus(ctx context.Context, req *review.CheckReviewStatusRequest) (*review.CheckReviewStatusResponse, error) {
	var count int64
	err := s.db.Model(&Review{}).
		Where("user_id = ? AND order_no = ? AND sku_id = ?", req.UserId, req.OrderNo, req.SkuId).
		Count(&count).Error

	if err != nil {
		log.Printf("查询评价状态失败: %v", err)
		return nil, status.Error(codes.Internal, "查询数据库失败")
	}

	return &review.CheckReviewStatusResponse{
		HasReviewed: count > 0,
		ReviewId:    0,
	}, nil
}

func main() {
	jaegerAddr := "jaeger:4318"
	if os.Getenv("JAEGER_HOST") != "" {
		jaegerAddr = os.Getenv("JAEGER_HOST")
	}
	tp, err := tracer.InitTracer("review-service", jaegerAddr)
	if err != nil {
		log.Printf("Init tracer failed: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	if v := os.Getenv("MYSQL_HOST"); v != "" {
		c.Mysql.Host = v
	}
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}

	c.Mysql.DbName = "db_review"
	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("初始化 MySQL 失败: %v", err)
	}
	db.AutoMigrate(&Review{})

	// 初始化订单服务客户端
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	}
	orderConn, err := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "order-service"), opts...)
	if err != nil {
		log.Fatalf("连接订单服务失败: %v", err)
	}

	s := grpc.NewServer(grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()))
	review.RegisterReviewServiceServer(s, &server{
		db:          db,
		orderClient: order.NewOrderServiceClient(orderConn),
	})
	reflection.Register(s)

	lis, _ := net.Listen("tcp", fmt.Sprintf(":%d", c.Service.Port))
	discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)

	log.Printf("Review Service listening on :%d", c.Service.Port)
	s.Serve(lis)
}
