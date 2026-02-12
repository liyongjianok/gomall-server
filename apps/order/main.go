package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"go-ecommerce/apps/order/model"
	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/pkg/tracer"
	"go-ecommerce/proto/address"
	"go-ecommerce/proto/cart"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/product"

	_ "github.com/mbobakov/grpc-consul-resolver"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// RabbitMQ 配置常量
const (
	MQUrl = "amqp://guest:guest@rabbitmq:5672/"

	// 死信队列配置 (用于订单超时取消)
	OrderDelayQueue   = "order.delay.queue" // 延迟缓冲队列
	OrderDeadLetterEx = "order.dlx"         // 死信交换机
	OrderCloseQueue   = "order.close.queue" // 实际消费队列
	OrderCloseRouting = "order.close"       // 路由Key
	OrderTTL          = 60 * 1000           // 超时时间 60秒

	// 秒杀队列配置 (用于削峰填谷)
	SeckillQueue = "seckill.order.queue"
)

// 秒杀消息结构体 (必须与 Product Service 发送的格式一致)
type SeckillMessage struct {
	UserId int64 `json:"user_id"`
	SkuId  int64 `json:"sku_id"`
}

type server struct {
	order.UnimplementedOrderServiceServer
	db            *gorm.DB
	mqConn        *amqp.Connection
	mqCh          *amqp.Channel
	productClient product.ProductServiceClient
	cartClient    cart.CartServiceClient
	addressClient address.AddressServiceClient
}

// initRabbitMQ 初始化 RabbitMQ 所有队列和交换机
func (s *server) initRabbitMQ() error {
	var err error
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

	// -------------------------------------------------------
	// 1. 声明死信队列结构 (用于超时取消)
	// -------------------------------------------------------

	// A. 声明死信交换机 (DLX)
	err = s.mqCh.ExchangeDeclare(OrderDeadLetterEx, "direct", true, false, false, false, nil)
	if err != nil {
		return err
	}

	// B. 声明实际消费队列 (OrderCloseQueue)
	qClose, err := s.mqCh.QueueDeclare(OrderCloseQueue, true, false, false, false, nil)
	if err != nil {
		return err
	}

	// C. 绑定消费队列到 DLX
	err = s.mqCh.QueueBind(qClose.Name, OrderCloseRouting, OrderDeadLetterEx, false, nil)
	if err != nil {
		return err
	}

	// D. 声明延迟队列 (设置 TTL 和 DLX)
	args := amqp.Table{
		"x-dead-letter-exchange":    OrderDeadLetterEx,
		"x-dead-letter-routing-key": OrderCloseRouting,
		"x-message-ttl":             OrderTTL,
	}
	_, err = s.mqCh.QueueDeclare(OrderDelayQueue, true, false, false, false, args)
	if err != nil {
		return err
	}

	// -------------------------------------------------------
	// 2. 声明秒杀队列 (用于异步下单)
	// -------------------------------------------------------
	_, err = s.mqCh.QueueDeclare(
		SeckillQueue, // name
		true,         // durable
		false,        // delete when unused
		false,        // exclusive
		false,        // no-wait
		nil,          // args
	)
	if err != nil {
		return fmt.Errorf("声明秒杀队列失败: %v", err)
	}

	log.Println("RabbitMQ 初始化成功 (包含 DLX 和 秒杀队列)")
	return nil
}

// publishDelayMessage 发送延迟消息 (用于超时控制)
func (s *server) publishDelayMessage(orderNo string) error {
	return s.mqCh.PublishWithContext(context.Background(),
		"",              // exchange
		OrderDelayQueue, // routing key
		false,
		false,
		amqp.Publishing{
			ContentType:  "text/plain",
			Body:         []byte(orderNo),
			DeliveryMode: amqp.Persistent,
		})
}

// startConsumer 启动消费者协程
func (s *server) startConsumer() {
	// -------------------------------------------------------
	// 消费者 1: 监听超时订单 (OrderCloseQueue)
	// -------------------------------------------------------
	msgsClose, err := s.mqCh.Consume(OrderCloseQueue, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("无法监听关闭队列: %v", err)
	}

	go func() {
		for d := range msgsClose {
			orderNo := string(d.Body)
			log.Printf("[MQ] 收到超时订单需处理: %s", orderNo)

			// 执行取消逻辑
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := s.cancelOrderLogic(ctx, orderNo)
			cancel()

			if err != nil {
				log.Printf("[MQ] 自动取消失败: %v", err)
				// 生产环境可以考虑 d.Reject(true) 重试
				d.Ack(false)
			} else {
				d.Ack(false)
				log.Printf("[MQ] 订单 %s 已自动取消", orderNo)
			}
		}
	}()

	// -------------------------------------------------------
	// 消费者 2: 监听秒杀成功消息 (SeckillQueue) -> 异步创建订单
	// -------------------------------------------------------
	msgsSeckill, err := s.mqCh.Consume(SeckillQueue, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("无法监听秒杀队列: %v", err)
	}

	go func() {
		for d := range msgsSeckill {
			var msg SeckillMessage
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				log.Printf("[MQ] 秒杀消息解析失败: %v", err)
				d.Ack(false) // 格式错误，丢弃
				continue
			}

			log.Printf("[MQ] 开始处理秒杀订单: User=%d SKU=%d", msg.UserId, msg.SkuId)

			// 执行创建订单逻辑
			err := s.createSeckillOrder(msg.UserId, msg.SkuId)
			if err != nil {
				log.Printf("[MQ] 秒杀下单失败: %v", err)
				// 这里不 Ack 或 Reject(true) 会导致消息死循环，
				// 在真实场景中，如果是因为数据库唯一键冲突（重复消费），应该 Ack 掉
				// 简单起见，我们 Ack 掉防止堵塞，实际应记录到死信或错误表
			} else {
				log.Printf("[MQ] 秒杀下单成功: User=%d SKU=%d", msg.UserId, msg.SkuId)
			}
			d.Ack(false)
		}
	}()
}

// 创建秒杀订单 (保证幂等性)
func (s *server) createSeckillOrder(userId, skuId int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 幂等性检查：生成唯一订单号
	// 规则：SK-{用户ID}-{商品ID}
	// 这样同一个用户对同一个商品只能生成一个订单号，数据库的 UNIQUE KEY 会阻止重复插入
	orderNo := fmt.Sprintf("SK-%d-%d", userId, skuId)

	// 先检查数据库是否已有该订单 (可选，DB 唯一索引也会挡住)
	var exist int64
	s.db.Model(&model.Order{}).Where("order_no = ?", orderNo).Count(&exist)
	if exist > 0 {
		log.Printf("[Info] 订单 %s 已存在，忽略重复消息", orderNo)
		return nil
	}

	var receiverName, receiverMobile, fullAddr string

	// 2. 获取用户地址 (兜底逻辑)
	addrResp, err := s.addressClient.ListAddress(ctx, &address.ListAddressRequest{UserId: userId})
	if err != nil || len(addrResp.Addresses) == 0 {
		log.Printf("[Info] 用户 %d 无收货地址，使用默认测试地址", userId)
		receiverName = fmt.Sprintf("秒杀用户%d", userId)
		receiverMobile = "13800008888"
		fullAddr = "秒杀专用通道虚拟地址"
	} else {
		addr := addrResp.Addresses[0]
		receiverName = addr.Name
		receiverMobile = addr.Mobile
		fullAddr = fmt.Sprintf("%s%s%s%s", addr.Province, addr.City, addr.District, addr.DetailAddress)
	}

	// 3. 获取商品信息 (为了存快照价格)
	prodResp, err := s.productClient.GetProduct(ctx, &product.GetProductRequest{Id: skuId})
	if err != nil {
		return fmt.Errorf("查询商品失败: %v", err)
	}

	// 4. 写入 MySQL
	newOrder := model.Order{
		OrderNo:         orderNo,
		UserID:          userId,
		TotalAmount:     float64(prodResp.Price),
		Status:          0, // 待支付
		ReceiverName:    receiverName,
		ReceiverMobile:  receiverMobile,
		ReceiverAddress: fullAddr,
		Items: []model.OrderItem{{
			ProductID:   prodResp.Id,
			SkuID:       prodResp.SkuId,
			ProductName: prodResp.Name,
			SkuName:     prodResp.SkuName,
			Price:       float64(prodResp.Price),
			Quantity:    1,
			Picture:     prodResp.Picture,
		}},
	}

	if err := s.db.Create(&newOrder).Error; err != nil {
		// 再次检查是否为唯一键冲突 (并发场景下)
		return fmt.Errorf("写入数据库失败: %v", err)
	}

	// 5. 发送超时取消消息 (秒杀订单也需要超时取消，否则库存永远被占用)
	_ = s.publishDelayMessage(orderNo)

	return nil
}

// CreateOrder 普通下单逻辑 (保持不变)
func (s *server) CreateOrder(ctx context.Context, req *order.CreateOrderRequest) (*order.CreateOrderResponse, error) {
	if req.AddressId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "必须选择收货地址")
	}
	if len(req.SkuIds) == 0 {
		return nil, status.Error(codes.InvalidArgument, "未选择任何商品")
	}

	addrResp, err := s.addressClient.GetAddress(ctx, &address.GetAddressRequest{AddressId: req.AddressId})
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "地址不存在")
	}
	fullAddress := fmt.Sprintf("%s%s%s%s", addrResp.Address.Province, addrResp.Address.City, addrResp.Address.District, addrResp.Address.DetailAddress)

	cartResp, err := s.cartClient.GetCart(ctx, &cart.GetCartRequest{UserId: req.UserId})
	if err != nil || len(cartResp.Items) == 0 {
		return nil, status.Error(codes.Unknown, "购物车为空")
	}

	selectedMap := make(map[int64]bool)
	for _, id := range req.SkuIds {
		selectedMap[id] = true
	}

	var selectedItems []*cart.CartItem
	for _, item := range cartResp.Items {
		if selectedMap[item.SkuId] {
			selectedItems = append(selectedItems, item)
		}
	}

	if len(selectedItems) == 0 {
		return nil, status.Error(codes.InvalidArgument, "选中的商品无效")
	}

	tx := s.db.Begin()
	var totalAmount float32
	var orderItems []model.OrderItem

	for _, item := range selectedItems {
		prodResp, err := s.productClient.GetProduct(ctx, &product.GetProductRequest{Id: item.SkuId})
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.NotFound, "商品 SKU %d 不存在", item.SkuId)
		}

		_, err = s.productClient.DecreaseStock(ctx, &product.DecreaseStockRequest{SkuId: item.SkuId, Count: item.Quantity})
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.ResourceExhausted, "商品 %s 库存不足", prodResp.Name)
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

	orderNo := fmt.Sprintf("%d%d", time.Now().UnixNano(), req.UserId)
	newOrder := model.Order{
		OrderNo:         orderNo,
		UserID:          req.UserId,
		TotalAmount:     float64(totalAmount),
		Status:          0,
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

	for _, item := range selectedItems {
		_, _ = s.cartClient.DeleteItem(ctx, &cart.DeleteItemRequest{
			UserId: req.UserId,
			SkuId:  item.SkuId,
		})
	}

	_ = s.publishDelayMessage(orderNo)

	return &order.CreateOrderResponse{OrderNo: orderNo, TotalAmount: totalAmount}, nil
}

// ListOrders 查询订单列表 (RPC)
func (s *server) ListOrders(ctx context.Context, req *order.ListOrdersRequest) (*order.ListOrdersResponse, error) {
	var orders []model.Order
	if err := s.db.Preload("Items").Where("user_id = ?", req.UserId).Order("created_at desc").Find(&orders).Error; err != nil {
		return nil, status.Error(codes.Internal, "查询失败")
	}
	var respOrders []*order.OrderInfo
	for _, o := range orders {
		var items []*order.OrderItem
		for _, item := range o.Items {
			items = append(items, &order.OrderItem{
				// 🔥 这里必须赋值！否则前端收到的就是 0
				SkuId:       int64(item.SkuID),
				ProductId:   int64(item.ProductID),
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

// MarkOrderPaid 标记支付成功 (RPC)
func (s *server) MarkOrderPaid(ctx context.Context, req *order.MarkOrderPaidRequest) (*order.MarkOrderPaidResponse, error) {
	var o model.Order
	if err := s.db.Where("order_no = ?", req.OrderNo).First(&o).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "订单不存在")
	}
	if o.Status == 1 {
		return &order.MarkOrderPaidResponse{Success: true}, nil
	}
	if err := s.db.Model(&o).UpdateColumn("status", 1).Error; err != nil {
		return nil, status.Error(codes.Internal, "更新状态失败")
	}
	log.Printf("订单 %s 支付成功", req.OrderNo)
	return &order.MarkOrderPaidResponse{Success: true}, nil
}

// CancelOrder 取消订单 (RPC)
func (s *server) CancelOrder(ctx context.Context, req *order.CancelOrderRequest) (*order.CancelOrderResponse, error) {
	return s.cancelOrderLogic(ctx, req.OrderNo)
}

// cancelOrderLogic 取消逻辑核心 (RPC/MQ 共用)
func (s *server) cancelOrderLogic(ctx context.Context, orderNo string) (*order.CancelOrderResponse, error) {
	var o model.Order
	if err := s.db.Preload("Items").Where("order_no = ?", orderNo).First(&o).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "订单不存在")
	}

	if o.Status != 0 {
		log.Printf("订单 %s 状态为 %d，跳过取消", orderNo, o.Status)
		return &order.CancelOrderResponse{Success: true}, nil
	}

	if err := s.db.Model(&o).UpdateColumn("status", 2).Error; err != nil {
		return nil, status.Error(codes.Internal, "更新状态失败")
	}

	for _, item := range o.Items {
		_, err := s.productClient.RollbackStock(ctx, &product.RollbackStockRequest{SkuId: int64(item.SkuID), Count: int32(item.Quantity)})
		if err != nil {
			log.Printf("[严重错误] 订单 %s 回滚库存失败: %v", orderNo, err)
		}
	}

	log.Printf("订单 %s 已成功取消", orderNo)
	return &order.CancelOrderResponse{Success: true}, nil
}

func main() {
	jaegerAddr := "jaeger:4318"
	if os.Getenv("JAEGER_HOST") != "" {
		jaegerAddr = os.Getenv("JAEGER_HOST")
	}
	tp, err := tracer.InitTracer("order-service", jaegerAddr)
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
	if v := os.Getenv("MYSQL_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Mysql.Port = p
		}
	}
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	}

	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("初始化 MySQL 失败: %v", err)
	}
	db.AutoMigrate(&model.Order{}, &model.OrderItem{})

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	}
	prodConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "product-service"), opts...)
	cartConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "cart-service"), opts...)
	addrConn, _ := grpc.Dial(fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "address-service"), opts...)

	s := grpc.NewServer(
		grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
	)
	srv := &server{
		db:            db,
		productClient: product.NewProductServiceClient(prodConn),
		cartClient:    cart.NewCartServiceClient(cartConn),
		addressClient: address.NewAddressServiceClient(addrConn),
	}

	// 初始化 RabbitMQ (重试机制)
	for i := 0; i < 10; i++ {
		if err := srv.initRabbitMQ(); err != nil {
			log.Printf("等待 RabbitMQ... %v", err)
			time.Sleep(2 * time.Second)
		} else {
			break
		}
	}
	if srv.mqConn != nil {
		defer srv.mqConn.Close()
		defer srv.mqCh.Close()
		srv.startConsumer() // 启动消费者
	} else {
		log.Println("[警告] RabbitMQ 未连接，自动取消和秒杀下单功能将失效！")
	}

	order.RegisterOrderServiceServer(s, srv)
	reflection.Register(s)

	lis, _ := net.Listen("tcp", fmt.Sprintf(":%d", c.Service.Port))
	discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)
	log.Printf("Order Service listening on :%d", c.Service.Port)
	s.Serve(lis)
}
