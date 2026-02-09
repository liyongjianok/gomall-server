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
	amqp "github.com/rabbitmq/amqp091-go" // RabbitMQ 客户端
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
	OrderDelayQueue   = "order.delay.queue" // 延迟缓冲队列 (设置消息 TTL)
	OrderDeadLetterEx = "order.dlx"         // 死信交换机 (DLX)
	OrderCloseQueue   = "order.close.queue" // 实际消费队列 (监听死信)
	OrderCloseRouting = "order.close"       // 死信路由 Key
	OrderTTL          = 60 * 1000           // 订单超时时间 (毫秒)，测试用 60秒
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

// initRabbitMQ 初始化 RabbitMQ 死信队列拓扑结构
func (s *server) initRabbitMQ() error {
	var err error
	// 1. 建立 RabbitMQ 连接
	mqUrl := os.Getenv("RABBITMQ_URL")
	if mqUrl == "" {
		mqUrl = MQUrl
	}
	s.mqConn, err = amqp.Dial(mqUrl)
	if err != nil {
		return fmt.Errorf("连接 RabbitMQ 失败: %v", err)
	}
	s.mqCh, err = s.mqConn.Channel()
	if err != nil {
		return fmt.Errorf("打开 Channel 失败: %v", err)
	}

	// 2. 声明死信交换机 (DLX)
	// 当消息过期后，会被转发到这个交换机
	err = s.mqCh.ExchangeDeclare(
		OrderDeadLetterEx, // 交换机名称
		"direct",          // 类型
		true,              // 持久化
		false,             // 自动删除
		false,             // 内部使用
		false,             // 无需等待
		nil,               // 参数
	)
	if err != nil {
		return fmt.Errorf("声明死信交换机失败: %v", err)
	}

	// 3. 声明实际消费队列 (OrderCloseQueue)
	// 这个队列用来接收超时的订单消息，消费者监听这个队列
	qClose, err := s.mqCh.QueueDeclare(
		OrderCloseQueue, // 队列名称
		true,            // 持久化
		false,           // 未使用时删除
		false,           // 独占
		false,           // 无需等待
		nil,             // 参数
	)
	if err != nil {
		return fmt.Errorf("声明消费队列失败: %v", err)
	}

	// 4. 将消费队列绑定到死信交换机
	err = s.mqCh.QueueBind(
		qClose.Name,       // 队列名称
		OrderCloseRouting, // 路由 Key
		OrderDeadLetterEx, // 交换机
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("绑定消费队列失败: %v", err)
	}

	// 5. 声明缓冲队列 (Delay Queue) - 设置 TTL 和 DLX
	// 这是一个“死胡同”队列，没有消费者监听它。
	// 消息在这里待满 TTL 时间后，变成死信，被转发到 DLX。
	args := amqp.Table{
		"x-dead-letter-exchange":    OrderDeadLetterEx, // 过期后发送到哪个交换机
		"x-dead-letter-routing-key": OrderCloseRouting, // 过期后携带的路由 Key
		"x-message-ttl":             OrderTTL,          // 消息存活时间 (毫秒)
	}
	_, err = s.mqCh.QueueDeclare(
		OrderDelayQueue, // 队列名称
		true,            // 持久化
		false,           // 未使用时删除
		false,           // 独占
		false,           // 无需等待
		args,            // [关键] 设置死信参数
	)
	if err != nil {
		return fmt.Errorf("声明延迟队列失败: %v", err)
	}

	log.Println("RabbitMQ 初始化成功 (死信队列模式)")
	return nil
}

// publishDelayMessage 发送延迟消息到缓冲队列
func (s *server) publishDelayMessage(orderNo string) error {
	// 发送到 Delay Queue。不需要指定路由逻辑，因为它完全依赖 TTL 过期机制。
	return s.mqCh.PublishWithContext(context.Background(),
		"",              // 默认交换机
		OrderDelayQueue, // 路由到缓冲队列
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType:  "text/plain",
			Body:         []byte(orderNo),
			DeliveryMode: amqp.Persistent, // 消息持久化
		})
}

// startConsumer 启动消费者监听超时订单
func (s *server) startConsumer() {
	msgs, err := s.mqCh.Consume(
		OrderCloseQueue, // 监听的是死信转发后的队列
		"",              // 消费者名称
		false,           // 关闭自动 ACK (手动确认更安全)
		false,           // 独占
		false,           // no-local
		false,           // no-wait
		nil,             // args
	)
	if err != nil {
		log.Fatalf("注册消费者失败: %v", err)
	}

	log.Println("正在监听订单超时队列 (OrderCloseQueue)...")

	go func() {
		for d := range msgs {
			orderNo := string(d.Body)
			log.Printf("[MQ] 收到超时订单: %s", orderNo)

			// 调用取消订单的核心逻辑
			// 创建一个新的 Context，并设置超时，防止处理卡死
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := s.cancelOrderLogic(ctx, orderNo)
			cancel()

			if err != nil {
				log.Printf("[MQ] 自动取消订单 %s 失败: %v", orderNo, err)
				// 如果是严重错误(如DB挂了)，可以用 d.Reject(true) 重试
				// 这里为了演示简单，如果订单已取消或不存在，我们直接 ACK 确认掉，防止消息堆积
				d.Ack(false)
			} else {
				// 处理成功，发送 ACK 确认消息已被消费
				d.Ack(false)
				log.Printf("[MQ] 订单 %s 已自动取消并确认", orderNo)
			}
		}
	}()
}

// CreateOrder 创建订单
func (s *server) CreateOrder(ctx context.Context, req *order.CreateOrderRequest) (*order.CreateOrderResponse, error) {
	// 0. 校验地址信息
	if req.AddressId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "必须填写收货地址 ID")
	}
	addrResp, err := s.addressClient.GetAddress(ctx, &address.GetAddressRequest{AddressId: req.AddressId})
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "地址查询失败: %v", err)
	}
	if addrResp.Address.UserId != req.UserId {
		return nil, status.Error(codes.PermissionDenied, "地址不属于该用户")
	}
	// 拼接详细地址字符串
	fullAddress := fmt.Sprintf("%s%s%s%s", addrResp.Address.Province, addrResp.Address.City, addrResp.Address.District, addrResp.Address.DetailAddress)

	// 1. 获取购物车商品
	cartResp, err := s.cartClient.GetCart(ctx, &cart.GetCartRequest{UserId: req.UserId})
	if err != nil || len(cartResp.Items) == 0 {
		return nil, status.Error(codes.Unknown, "购物车为空")
	}

	// 2. 开启数据库事务
	tx := s.db.Begin()
	var totalAmount float32
	var orderItems []model.OrderItem

	// 3. 遍历购物车 & 扣减库存
	for _, item := range cartResp.Items {
		// 查询商品详情 (获取价格和名称)
		prodResp, err := s.productClient.GetProduct(ctx, &product.GetProductRequest{Id: item.SkuId})
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.NotFound, "商品不存在: SKU %d", item.SkuId)
		}

		// 远程调用扣减库存
		_, err = s.productClient.DecreaseStock(ctx, &product.DecreaseStockRequest{SkuId: item.SkuId, Count: item.Quantity})
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.Unknown, "扣减库存失败: %v", err)
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
	// 生成唯一的订单号 (简单模拟：时间戳+用户ID)
	orderNo := fmt.Sprintf("%d%d", time.Now().UnixNano(), req.UserId)
	newOrder := model.Order{
		OrderNo:         orderNo,
		UserID:          req.UserId,
		TotalAmount:     float64(totalAmount),
		Status:          0, // 0: 待支付
		Items:           orderItems,
		ReceiverName:    addrResp.Address.Name,
		ReceiverMobile:  addrResp.Address.Mobile,
		ReceiverAddress: fullAddress,
	}

	if err := tx.Create(&newOrder).Error; err != nil {
		tx.Rollback()
		return nil, status.Error(codes.Internal, "创建订单失败")
	}
	tx.Commit()

	// 5. 清空购物车
	_, _ = s.cartClient.EmptyCart(ctx, &cart.EmptyCartRequest{UserId: req.UserId})

	// 6. [新增] 发送 RabbitMQ 延迟消息
	err = s.publishDelayMessage(orderNo)
	if err != nil {
		// 生产环境这里需要降级处理 (例如写入本地重试表)，防止消息丢失
		log.Printf("[Error] 订单 %s 发送延迟消息失败: %v", orderNo, err)
	} else {
		log.Printf("订单 %s 已发送至延迟队列 (TTL: %dms)", orderNo, OrderTTL)
	}

	return &order.CreateOrderResponse{OrderNo: orderNo, TotalAmount: totalAmount}, nil
}

// ListOrders 查询订单列表
func (s *server) ListOrders(ctx context.Context, req *order.ListOrdersRequest) (*order.ListOrdersResponse, error) {
	var orders []model.Order
	if err := s.db.Preload("Items").Where("user_id = ?", req.UserId).Order("created_at desc").Find(&orders).Error; err != nil {
		return nil, status.Error(codes.Internal, "查询数据库失败")
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
			OrderNo:         o.OrderNo,
			TotalAmount:     float32(o.TotalAmount),
			Status:          int32(o.Status),
			CreatedAt:       o.CreatedAt.Format("2006-01-02 15:04:05"),
			Items:           items,
			ReceiverName:    o.ReceiverName,
			ReceiverMobile:  o.ReceiverMobile,
			ReceiverAddress: o.ReceiverAddress,
		})
	}
	return &order.ListOrdersResponse{Orders: respOrders}, nil
}

// MarkOrderPaid 支付成功回调
func (s *server) MarkOrderPaid(ctx context.Context, req *order.MarkOrderPaidRequest) (*order.MarkOrderPaidResponse, error) {
	var o model.Order
	if err := s.db.Where("order_no = ?", req.OrderNo).First(&o).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "订单不存在")
	}
	if o.Status == 1 {
		return &order.MarkOrderPaidResponse{Success: true}, nil
	}

	if err := s.db.Model(&o).UpdateColumn("status", 1).Error; err != nil {
		return nil, status.Error(codes.Internal, "更新订单状态失败")
	}

	log.Printf("订单 %s 已标记为支付成功", req.OrderNo)
	// 注意：使用死信队列模式，支付成功后不需要手动移除消息。
	// 消息过期后被消费者收到时，cancelOrderLogic 会检查订单状态，如果是已支付，则忽略取消操作。
	return &order.MarkOrderPaidResponse{Success: true}, nil
}

// CancelOrder 取消订单 (RPC 接口)
func (s *server) CancelOrder(ctx context.Context, req *order.CancelOrderRequest) (*order.CancelOrderResponse, error) {
	return s.cancelOrderLogic(ctx, req.OrderNo)
}

// cancelOrderLogic 取消订单核心逻辑 (供 RPC 和 MQ 消费者复用)
func (s *server) cancelOrderLogic(ctx context.Context, orderNo string) (*order.CancelOrderResponse, error) {
	var o model.Order
	// 注意：这里需要 Preload Items 以便回滚库存
	if err := s.db.Preload("Items").Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "订单不存在")
	}

	// 关键：检查订单状态
	// 如果已经支付(1)或已取消(2)，则什么都不做
	if o.Status != 0 {
		log.Printf("订单 %s 状态为 %d，跳过取消操作", orderNo, o.Status)
		return &order.CancelOrderResponse{Success: true}, nil
	}

	// 开启事务进行取消
	if err := s.db.Model(&o).UpdateColumn("status", 2).Error; err != nil {
		return nil, status.Error(codes.Internal, "更新订单状态失败")
	}

	// 回滚库存
	for _, item := range o.Items {
		_, err := s.productClient.RollbackStock(ctx, &product.RollbackStockRequest{SkuId: int64(item.SkuID), Count: int32(item.Quantity)})
		if err != nil {
			log.Printf("[Error] 回滚库存失败: %v", err)
		}
	}

	log.Printf("订单 %s 已取消 (触发源: RabbitMQ/RPC)", orderNo)
	return &order.CancelOrderResponse{Success: true}, nil
}

func main() {
	// 加载配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// [关键] 适配 Docker 环境变量
	if v := os.Getenv("MYSQL_HOST"); v != "" {
		c.Mysql.Host = v
	}

	// 初始化数据库
	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("初始化 MySQL 失败: %v", err)
	}
	db.AutoMigrate(&model.Order{}, &model.OrderItem{})

	// 建立 gRPC 连接 (Product, Cart, Address)
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
	// 给予一定的重试机制，防止 Order Service 启动比 RabbitMQ 快导致连接失败
	for i := 0; i < 10; i++ {
		if err := srv.initRabbitMQ(); err != nil {
			log.Printf("等待 RabbitMQ 就绪... (%v)", err)
			time.Sleep(2 * time.Second)
		} else {
			break
		}
	}

	if srv.mqConn != nil {
		defer srv.mqConn.Close()
		defer srv.mqCh.Close()
		// 启动后台消费协程
		srv.startConsumer()
	} else {
		log.Println("[警告] RabbitMQ 初始化失败，自动取消订单功能将不可用！")
	}

	// 注册 gRPC 服务
	order.RegisterOrderServiceServer(s, srv)
	reflection.Register(s)

	// 启动监听
	lis, _ := net.Listen("tcp", fmt.Sprintf(":%d", c.Service.Port))
	discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	log.Printf("Order Service 正在监听端口 :%d", c.Service.Port)
	s.Serve(lis)
}
