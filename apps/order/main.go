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
	"go-ecommerce/proto/address"
	"go-ecommerce/proto/cart"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/product"

	_ "github.com/mbobakov/grpc-consul-resolver"
	amqp "github.com/rabbitmq/amqp091-go" // [新增] RabbitMQ 客户端
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// RabbitMQ 配置常量
const (
	MQUrl             = "amqp://guest:guest@rabbitmq:5672/"
	OrderDelayQueue   = "order.delay.queue" // 缓冲队列 (设置 TTL)
	OrderDeadLetterEx = "order.dlx"         // 死信交换机
	OrderCloseQueue   = "order.close.queue" // 实际消费队列
	OrderCloseRouting = "order.close"       // 路由 Key
	OrderTTL          = 60 * 1000           // 超时时间 (毫秒)，测试用 60秒
)

type server struct {
	order.UnimplementedOrderServiceServer
	db            *gorm.DB
	mqConn        *amqp.Connection // RabbitMQ 连接
	mqCh          *amqp.Channel    // RabbitMQ 通道
	productClient product.ProductServiceClient
	cartClient    cart.CartServiceClient
	addressClient address.AddressServiceClient
}

// initRabbitMQ 初始化死信队列拓扑结构
func (s *server) initRabbitMQ() error {
	var err error
	// 1. 建立连接
	mqUrl := os.Getenv("RABBITMQ_URL")
	if mqUrl == "" {
		mqUrl = MQUrl
	}
	s.mqConn, err = amqp.Dial(mqUrl)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %v", err)
	}
	s.mqCh, err = s.mqConn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %v", err)
	}

	// 2. 声明死信交换机 (DLX)
	err = s.mqCh.ExchangeDeclare(
		OrderDeadLetterEx, // name
		"direct",          // type
		true,              // durable
		false,             // auto-deleted
		false,             // internal
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare DLX: %v", err)
	}

	// 3. 声明实际消费队列 (OrderCloseQueue)
	qClose, err := s.mqCh.QueueDeclare(
		OrderCloseQueue, // name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare close queue: %v", err)
	}

	// 4. 将消费队列绑定到死信交换机
	err = s.mqCh.QueueBind(
		qClose.Name,       // queue name
		OrderCloseRouting, // routing key
		OrderDeadLetterEx, // exchange
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to bind close queue: %v", err)
	}

	// 5. 声明缓冲队列 (Delay Queue) - 设置 TTL 和 DLX
	args := amqp.Table{
		"x-dead-letter-exchange":    OrderDeadLetterEx, // 过期后发给谁
		"x-dead-letter-routing-key": OrderCloseRouting, // 过期后带什么Key
		"x-message-ttl":             OrderTTL,          // 过期时间 (毫秒)
	}
	_, err = s.mqCh.QueueDeclare(
		OrderDelayQueue, // name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		args,            // [关键] 参数设置
	)
	if err != nil {
		return fmt.Errorf("failed to declare delay queue: %v", err)
	}

	log.Println("RabbitMQ initialized successfully (DLX Mode)")
	return nil
}

// publishDelayMessage 发送延迟消息
func (s *server) publishDelayMessage(orderNo string) error {
	// 发送到 Delay Queue，不做任何路由 Key，因为它靠 TTL 过期后自动路由
	return s.mqCh.PublishWithContext(context.Background(),
		"",              // exchange (默认交换机)
		OrderDelayQueue, // routing key (直接发给 delay queue)
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType:  "text/plain",
			Body:         []byte(orderNo),
			DeliveryMode: amqp.Persistent, // 持久化消息
		})
}

// startConsumer 启动消费者
func (s *server) startConsumer() {
	msgs, err := s.mqCh.Consume(
		OrderCloseQueue, // 监听 Close Queue
		"",              // consumer
		false,           // auto-ack (改为手动ACK，保证可靠性)
		false,           // exclusive
		false,           // no-local
		false,           // no-wait
		nil,             // args
	)
	if err != nil {
		log.Fatalf("Failed to register consumer: %v", err)
	}

	log.Println("Waiting for messages in OrderCloseQueue...")

	go func() {
		for d := range msgs {
			orderNo := string(d.Body)
			log.Printf("Received timeout order: %s", orderNo)

			// 调用取消逻辑
			// 创建一个新的 context，因为 RPC 调用需要
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := s.cancelOrderLogic(ctx, orderNo)
			cancel()

			if err != nil {
				log.Printf("Error canceling order %s: %v", orderNo, err)
				// 如果是严重错误(比如DB挂了)，可以用 d.Reject(true) 重试
				// 这里为了简单，如果已经取消过或者不存在，我们直接 ACK
				d.Ack(false)
			} else {
				// 成功处理，确认消息
				d.Ack(false)
				log.Printf("Order %s auto-cancelled and ACKed", orderNo)
			}
		}
	}()
}

// CreateOrder 下单逻辑
func (s *server) CreateOrder(ctx context.Context, req *order.CreateOrderRequest) (*order.CreateOrderResponse, error) {
	// 0. 校验地址
	if req.AddressId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "Address ID is required")
	}
	addrResp, err := s.addressClient.GetAddress(ctx, &address.GetAddressRequest{AddressId: req.AddressId})
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Address not found: %v", err)
	}
	if addrResp.Address.UserId != req.UserId {
		return nil, status.Error(codes.PermissionDenied, "Address not belong to user")
	}
	fullAddress := fmt.Sprintf("%s%s%s%s", addrResp.Address.Province, addrResp.Address.City, addrResp.Address.District, addrResp.Address.DetailAddress)

	// 1. 获取购物车
	cartResp, err := s.cartClient.GetCart(ctx, &cart.GetCartRequest{UserId: req.UserId})
	if err != nil || len(cartResp.Items) == 0 {
		return nil, status.Error(codes.Unknown, "cart is empty")
	}

	// 2. 开启事务
	tx := s.db.Begin()
	var totalAmount float32
	var orderItems []model.OrderItem

	// 3. 遍历购物车 & 扣库存
	for _, item := range cartResp.Items {
		prodResp, err := s.productClient.GetProduct(ctx, &product.GetProductRequest{Id: item.SkuId})
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.NotFound, "Sku not found: %d", item.SkuId)
		}

		_, err = s.productClient.DecreaseStock(ctx, &product.DecreaseStockRequest{SkuId: item.SkuId, Count: item.Quantity})
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.Unknown, "failed to decrease stock: %v", err)
		}

		totalAmount += prodResp.Price * float32(item.Quantity)
		orderItems = append(orderItems, model.OrderItem{
			ProductID: prodResp.Id, SkuID: prodResp.SkuId, ProductName: prodResp.Name, SkuName: prodResp.SkuName,
			Price: float64(prodResp.Price), Quantity: int(item.Quantity), Picture: prodResp.Picture,
		})
	}

	// 4. 创建订单
	orderNo := fmt.Sprintf("%d%d", time.Now().UnixNano(), req.UserId)
	newOrder := model.Order{
		OrderNo: orderNo, UserID: req.UserId, TotalAmount: float64(totalAmount), Status: 0,
		Items: orderItems, ReceiverName: addrResp.Address.Name, ReceiverMobile: addrResp.Address.Mobile, ReceiverAddress: fullAddress,
	}

	if err := tx.Create(&newOrder).Error; err != nil {
		tx.Rollback()
		return nil, status.Error(codes.Internal, "Failed to create order")
	}
	tx.Commit()

	// 5. 清空购物车
	_, _ = s.cartClient.EmptyCart(ctx, &cart.EmptyCartRequest{UserId: req.UserId})

	// 6. [新增] 发送 RabbitMQ 延迟消息
	err = s.publishDelayMessage(orderNo)
	if err != nil {
		// 生产环境这里需要降级处理 (例如写入本地重试表)
		log.Printf("CRITICAL: Failed to publish delay message for order %s: %v", orderNo, err)
	} else {
		log.Printf("Order %s sent to RabbitMQ delay queue (TTL: %dms)", orderNo, OrderTTL)
	}

	return &order.CreateOrderResponse{OrderNo: orderNo, TotalAmount: totalAmount}, nil
}

// ListOrders (保持不变)
func (s *server) ListOrders(ctx context.Context, req *order.ListOrdersRequest) (*order.ListOrdersResponse, error) {
	var orders []model.Order
	if err := s.db.Preload("Items").Where("user_id = ?", req.UserId).Order("created_at desc").Find(&orders).Error; err != nil {
		return nil, status.Error(codes.Internal, "Database error")
	}
	var respOrders []*order.OrderInfo
	for _, o := range orders {
		var items []*order.OrderItem
		for _, item := range o.Items {
			items = append(items, &order.OrderItem{ProductName: item.ProductName, SkuName: item.SkuName, Price: float32(item.Price), Quantity: int32(item.Quantity), Picture: item.Picture})
		}
		respOrders = append(respOrders, &order.OrderInfo{OrderNo: o.OrderNo, TotalAmount: float32(o.TotalAmount), Status: int32(o.Status), CreatedAt: o.CreatedAt.Format("2006-01-02 15:04:05"), Items: items, ReceiverName: o.ReceiverName, ReceiverMobile: o.ReceiverMobile, ReceiverAddress: o.ReceiverAddress})
	}
	return &order.ListOrdersResponse{Orders: respOrders}, nil
}

// MarkOrderPaid (保持不变，RabbitMQ 不需要像 Redis 那样手动移除，等它过期了消费时判断一下状态即可)
func (s *server) MarkOrderPaid(ctx context.Context, req *order.MarkOrderPaidRequest) (*order.MarkOrderPaidResponse, error) {
	var o model.Order
	if err := s.db.Where("order_no = ?", req.OrderNo).First(&o).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "Order not found")
	}
	if o.Status == 1 {
		return &order.MarkOrderPaidResponse{Success: true}, nil
	}

	if err := s.db.Model(&o).UpdateColumn("status", 1).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to update status")
	}

	log.Printf("Order %s marked as PAID", req.OrderNo)
	return &order.MarkOrderPaidResponse{Success: true}, nil
}

// CancelOrder (RPC接口)
func (s *server) CancelOrder(ctx context.Context, req *order.CancelOrderRequest) (*order.CancelOrderResponse, error) {
	return s.cancelOrderLogic(ctx, req.OrderNo)
}

// [复用] 核心取消逻辑
func (s *server) cancelOrderLogic(ctx context.Context, orderNo string) (*order.CancelOrderResponse, error) {
	var o model.Order
	// 注意：这里需要 Preload Items
	if err := s.db.Preload("Items").Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		// 如果订单不存在，可能是数据库还没同步完，或者查错了
		return nil, status.Errorf(codes.NotFound, "Order not found")
	}

	// 关键：如果已经支付(1)或已取消(2)，则什么都不做
	if o.Status != 0 {
		log.Printf("Order %s is status %d, skipping cancellation", orderNo, o.Status)
		return &order.CancelOrderResponse{Success: true}, nil
	}

	// 开启事务进行取消
	if err := s.db.Model(&o).UpdateColumn("status", 2).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to update status")
	}

	// 回滚库存
	for _, item := range o.Items {
		_, err := s.productClient.RollbackStock(ctx, &product.RollbackStockRequest{SkuId: int64(item.SkuID), Count: int32(item.Quantity)})
		if err != nil {
			log.Printf("CRITICAL: Failed to rollback stock: %v", err)
		}
	}

	log.Printf("Order %s cancelled successfully (RabbitMQ DLX)", orderNo)
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
	addrConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "address-service"), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`))

	s := grpc.NewServer()
	srv := &server{
		db:            db,
		productClient: product.NewProductServiceClient(prodConn),
		cartClient:    cart.NewCartServiceClient(cartConn),
		addressClient: address.NewAddressServiceClient(addrConn),
	}

	// [新增] 初始化 RabbitMQ 并启动消费者
	// 给一点重试机制，因为 RabbitMQ 启动可能比 Order Service 慢
	for i := 0; i < 10; i++ {
		if err := srv.initRabbitMQ(); err != nil {
			log.Printf("Waiting for RabbitMQ... (%v)", err)
			time.Sleep(2 * time.Second)
		} else {
			break
		}
	}
	if srv.mqConn != nil {
		defer srv.mqConn.Close()
		defer srv.mqCh.Close()
		// 启动消费协程
		srv.startConsumer()
	} else {
		log.Println("WARNING: RabbitMQ init failed, auto-cancellation will NOT work.")
	}

	order.RegisterOrderServiceServer(s, srv)
	reflection.Register(s)

	lis, _ := net.Listen("tcp", fmt.Sprintf(":%d", c.Service.Port))
	discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	log.Printf("Order Service listening on :%d", c.Service.Port)
	s.Serve(lis)
}
