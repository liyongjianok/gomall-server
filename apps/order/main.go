package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"go-ecommerce/apps/order/model"
	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/address" // [新增]
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
	addressClient address.AddressServiceClient // [新增]
}

// CreateOrder 下单逻辑
func (s *server) CreateOrder(ctx context.Context, req *order.CreateOrderRequest) (*order.CreateOrderResponse, error) {
	// 0. [新增] 校验并获取地址快照
	// 必须传入 AddressId
	if req.AddressId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "Address ID is required")
	}

	addrResp, err := s.addressClient.GetAddress(ctx, &address.GetAddressRequest{AddressId: req.AddressId})
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Address not found or invalid: %v", err)
	}

	// 校验地址归属
	if addrResp.Address.UserId != req.UserId {
		return nil, status.Error(codes.PermissionDenied, "Address does not belong to user")
	}

	// 拼接详细地址字符串 (例如: 山东省潍坊市寿光市xxx路)
	fullAddress := fmt.Sprintf("%s%s%s%s",
		addrResp.Address.Province,
		addrResp.Address.City,
		addrResp.Address.District,
		addrResp.Address.DetailAddress,
	)

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

		totalAmount += prodResp.Price * float32(item.Quantity)

		orderItems = append(orderItems, model.OrderItem{
			ProductID:   prodResp.Id,
			SkuID:       prodResp.SkuId,
			ProductName: prodResp.Name,
			SkuName:     prodResp.SkuName,
			Price:       float64(prodResp.Price),
			Quantity:    int(item.Quantity),
			Picture:     prodResp.Picture,
		})
	}

	// 4. 创建订单记录
	orderNo := fmt.Sprintf("%d%d", time.Now().UnixNano(), req.UserId)
	newOrder := model.Order{
		OrderNo:     orderNo,
		UserID:      req.UserId,
		TotalAmount: float64(totalAmount),
		Status:      0, // 0: 待支付
		Items:       orderItems,
		// [新增] 存入快照
		ReceiverName:    addrResp.Address.Name,
		ReceiverMobile:  addrResp.Address.Mobile,
		ReceiverAddress: fullAddress,
	}

	if err := tx.Create(&newOrder).Error; err != nil {
		tx.Rollback()
		return nil, status.Error(codes.Internal, "Failed to create order record")
	}

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
				Quantity:    int32(item.Quantity),
				Picture:     item.Picture,
			})
		}

		respOrders = append(respOrders, &order.OrderInfo{
			OrderNo:     o.OrderNo,
			TotalAmount: float32(o.TotalAmount),
			Status:      int32(o.Status),
			CreatedAt:   o.CreatedAt.Format("2006-01-02 15:04:05"),
			Items:       items,
			// [新增] 返回地址快照
			ReceiverName:    o.ReceiverName,
			ReceiverMobile:  o.ReceiverMobile,
			ReceiverAddress: o.ReceiverAddress,
		})
	}

	return &order.ListOrdersResponse{Orders: respOrders}, nil
}

// MarkOrderPaid (保持不变)
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

// CancelOrder (保持不变)
func (s *server) CancelOrder(ctx context.Context, req *order.CancelOrderRequest) (*order.CancelOrderResponse, error) {
	var o model.Order
	if err := s.db.Preload("Items").Where("order_no = ? AND user_id = ?", req.OrderNo, req.UserId).First(&o).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Order not found")
	}
	if o.Status != 0 {
		return nil, status.Error(codes.FailedPrecondition, "Order status is not pending payment")
	}
	if err := s.db.Model(&o).UpdateColumn("status", 2).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to update order status")
	}
	for _, item := range o.Items {
		_, _ = s.productClient.RollbackStock(ctx, &product.RollbackStockRequest{SkuId: int64(item.SkuID), Count: int32(item.Quantity)})
	}
	log.Printf("Order %s cancelled", req.OrderNo)
	return &order.CancelOrderResponse{Success: true}, nil
}

func main() {
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 适配环境变量
	if v := os.Getenv("MYSQL_HOST"); v != "" {
		c.Mysql.Host = v
	}

	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Failed to init mysql: %v", err)
	}
	db.AutoMigrate(&model.Order{}, &model.OrderItem{})

	prodConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "product-service"), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`))
	cartConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "cart-service"), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`))

	// [新增] 连接 Address Service
	addrConn, err := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "address-service"), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`))
	if err != nil {
		log.Fatalf("did not connect address-service: %v", err)
	}

	s := grpc.NewServer()
	order.RegisterOrderServiceServer(s, &server{
		db:            db,
		productClient: product.NewProductServiceClient(prodConn),
		cartClient:    cart.NewCartServiceClient(cartConn),
		addressClient: address.NewAddressServiceClient(addrConn), // [新增]
	})

	// ... (监听端口等代码保持不变，请确保有反射注册)
	reflection.Register(s)
	lis, _ := net.Listen("tcp", fmt.Sprintf(":%d", c.Service.Port))
	discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	log.Printf("Order Service listening on :%d", c.Service.Port)
	s.Serve(lis)
}
