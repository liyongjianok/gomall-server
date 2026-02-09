package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-ecommerce/apps/order/model"
	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/cart"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/product"

	_ "github.com/mbobakov/grpc-consul-resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type server struct {
	order.UnimplementedOrderServiceServer
	db            *gorm.DB
	productClient product.ProductServiceClient
	cartClient    cart.CartServiceClient
}

// CreateOrder 下单逻辑
func (s *server) CreateOrder(ctx context.Context, req *order.CreateOrderRequest) (*order.CreateOrderResponse, error) {
	// 1. 获取购物车
	cartResp, err := s.cartClient.GetCart(ctx, &cart.GetCartRequest{UserId: req.UserId})
	if err != nil || len(cartResp.Items) == 0 {
		return nil, status.Error(codes.Unknown, "cart is empty or failed to retrieve")
	}

	// 2. 开启数据库事务
	tx := s.db.Begin()

	var totalAmount float32
	var orderItems []model.OrderItem

	// 3. 遍历购物车
	for _, item := range cartResp.Items {
		// 3.1 查商品详情
		prodResp, err := s.productClient.GetProduct(ctx, &product.GetProductRequest{Id: item.SkuId})
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.NotFound, "Sku not found: %d", item.SkuId)
		}

		// 3.2 扣减库存
		_, err = s.productClient.DecreaseStock(ctx, &product.DecreaseStockRequest{
			SkuId: item.SkuId,
			Count: item.Quantity,
		})
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.Unknown, "failed to decrease stock: %v", err)
		}

		// 3.3 累加金额
		totalAmount += prodResp.Price * float32(item.Quantity)

		// 3.4 构建订单项
		// [修正] 这里修正了字段名 UserID/ProductID/SkuID，并进行了类型转换
		orderItems = append(orderItems, model.OrderItem{
			ProductID:   prodResp.Id,    // 对应 model.ProductID
			SkuID:       prodResp.SkuId, // 对应 model.SkuID
			ProductName: prodResp.Name,
			SkuName:     prodResp.SkuName,
			Price:       float64(prodResp.Price),
			Quantity:    int(item.Quantity), // [修正] int32 转 int
			Picture:     prodResp.Picture,
		})
	}

	// 4. 创建订单记录
	orderNo := fmt.Sprintf("%d%d", time.Now().UnixNano(), req.UserId)
	newOrder := model.Order{
		OrderNo:     orderNo,
		UserID:      req.UserId, // [修正] 对应 model.UserID
		TotalAmount: float64(totalAmount),
		Status:      0, // 0: 待支付
		Items:       orderItems,
	}

	if err := tx.Create(&newOrder).Error; err != nil {
		tx.Rollback()
		return nil, status.Error(codes.Internal, "Failed to create order record")
	}

	// 5. 提交事务
	tx.Commit()

	// 6. 清空购物车
	_, _ = s.cartClient.EmptyCart(ctx, &cart.EmptyCartRequest{UserId: req.UserId})

	return &order.CreateOrderResponse{
		OrderNo:     orderNo,
		TotalAmount: totalAmount,
	}, nil
}

// ListOrders 订单列表
func (s *server) ListOrders(ctx context.Context, req *order.ListOrdersRequest) (*order.ListOrdersResponse, error) {
	var orders []model.Order
	// Preload 加载关联的 Items 数据
	// [修正] Where条件字段必须跟数据库列名一致 (user_id)
	if err := s.db.Preload("Items").Where("user_id = ?", req.UserId).Order("created_at desc").Find(&orders).Error; err != nil {
		return nil, status.Error(codes.Internal, "Database error")
	}

	var respOrders []*order.OrderInfo
	for _, o := range orders {
		var items []*order.OrderItem
		for _, item := range o.Items {
			items = append(items, &order.OrderItem{
				ProductName: item.ProductName,
				SkuName:     item.SkuName,
				Price:       float32(item.Price),
				Quantity:    int32(item.Quantity), // [修正] int 转 int32
				Picture:     item.Picture,
			})
		}

		respOrders = append(respOrders, &order.OrderInfo{
			OrderNo:     o.OrderNo,
			TotalAmount: float32(o.TotalAmount),
			Status:      int32(o.Status),
			CreatedAt:   o.CreatedAt.Format("2006-01-02 15:04:05"),
			Items:       items,
		})
	}

	return &order.ListOrdersResponse{Orders: respOrders}, nil
}

// MarkOrderPaid 标记订单已支付
func (s *server) MarkOrderPaid(ctx context.Context, req *order.MarkOrderPaidRequest) (*order.MarkOrderPaidResponse, error) {
	var o model.Order
	if err := s.db.Where("order_no = ?", req.OrderNo).First(&o).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Order not found: %s", req.OrderNo)
	}

	if o.Status == 1 {
		return &order.MarkOrderPaidResponse{Success: true}, nil
	}

	if err := s.db.Model(&o).UpdateColumn("status", 1).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to update order status")
	}

	log.Printf("Order %s marked as PAID", req.OrderNo)
	return &order.MarkOrderPaidResponse{Success: true}, nil
}

// [新增] CancelOrder 取消订单
func (s *server) CancelOrder(ctx context.Context, req *order.CancelOrderRequest) (*order.CancelOrderResponse, error) {
	// 1. 查订单 (带上 Items，因为我们要知道还几个库存)
	var o model.Order
	if err := s.db.Preload("Items").Where("order_no = ? AND user_id = ?", req.OrderNo, req.UserId).First(&o).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Order not found or permission denied")
	}

	// 2. 校验状态 (只有 0:待支付 才能取消)
	// 如果已经支付(1)了，那就叫“退款”流程，这里先不做
	if o.Status != 0 {
		return nil, status.Error(codes.FailedPrecondition, "Order status is not pending payment")
	}

	// 3. 开启事务 (本地更新状态 + 远程归还库存最好在逻辑上是一体的)
	// 注意：在微服务中，这里其实涉及到分布式事务。
	// 如果本地取消成功，但远程归还失败，会导致数据不一致。
	// 简单起见，我们先更新本地，再调远程。如果远程失败，打印严重日志(实际需人工介入或重试)

	// 更新订单状态为 2:已取消
	if err := s.db.Model(&o).UpdateColumn("status", 2).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to update order status")
	}

	// 4. 遍历订单项，归还库存
	for _, item := range o.Items {
		_, err := s.productClient.RollbackStock(ctx, &product.RollbackStockRequest{
			SkuId: int64(item.SkuID), // 注意类型转换
			Count: int32(item.Quantity),
		})
		if err != nil {
			// 😱 严重错误：订单取消了，库存没还回去！
			// 生产环境这里需要发报警，或者写入一张“补偿表”后台重试
			log.Printf("CRITICAL: Failed to rollback stock for SKU %d: %v", item.SkuID, err)
		}
	}

	log.Printf("Order %s cancelled", req.OrderNo)
	return &order.CancelOrderResponse{Success: true}, nil
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
	db.AutoMigrate(&model.Order{}, &model.OrderItem{})

	prodTarget := fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "product-service")
	prodConn, err := grpc.Dial(
		prodTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("did not connect product-service: %v", err)
	}
	prodClient := product.NewProductServiceClient(prodConn)

	cartTarget := fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "cart-service")
	cartConn, err := grpc.Dial(
		cartTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("did not connect cart-service: %v", err)
	}
	cartClient := cart.NewCartServiceClient(cartConn)

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
	order.RegisterOrderServiceServer(s, &server{
		db:            db,
		productClient: prodClient,
		cartClient:    cartClient,
	})
	reflection.Register(s)

	log.Printf("Order Service listening on %s", addr)

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
