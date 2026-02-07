package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go-ecommerce/apps/order/model"
	"go-ecommerce/pkg/config"
	"go-ecommerce/pkg/database"
	"go-ecommerce/pkg/discovery"
	"go-ecommerce/proto/cart"
	"go-ecommerce/proto/order"
	"go-ecommerce/proto/product"

	"github.com/google/uuid"
	_ "github.com/mbobakov/grpc-consul-resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"gorm.io/gorm"
)

type server struct {
	order.UnimplementedOrderServiceServer
	db            *gorm.DB
	cartClient    cart.CartServiceClient
	productClient product.ProductServiceClient
}

// CreateOrder 创建订单的核心逻辑
func (s *server) CreateOrder(ctx context.Context, req *order.CreateOrderRequest) (*order.CreateOrderResponse, error) {
	// 1. 获取购物车商品
	cartResp, err := s.cartClient.GetCart(ctx, &cart.GetCartRequest{UserId: req.UserId})
	if err != nil || len(cartResp.Items) == 0 {
		return nil, fmt.Errorf("cart is empty or failed to retrieve")
	}

	var totalAmount float64
	var orderItems []model.OrderItem
	var stockRequests []*product.StockInfo // 用于扣库存的请求列表

	// 2. 遍历购物车，去商品服务查询最新价格和详情
	for _, cartItem := range cartResp.Items {
		// 这里为了简单，我们假设通过 ProductID 或 SkuID 能查到详情
		// 因为购物车里只有 SKU ID，我们需要去 Product Service 查详细信息
		// 注意：实际生产中应批量查询以减少 RPC 调用，这里简化为循环调用

		// 暂时没有直接根据 SkuID 查详情的接口，我们先假设我们能拿到 ProductID (或者扩展 Product 接口)
		// 为了演示流程，我们这里做一个简化：
		// 我们假设 SkuID 对应的数据是真实存在的，并且我们通过 GetProduct(ID=1) 模拟查询
		// TODO: 在 Product Service 增加 GetSkuDetail 接口是最佳实践

		// 模拟：我们假设所有 SKU 都属于 ID=1 的商品(仅供演示)，实际应根据 cartItem.SkuId 反查
		// 在真实项目中，CartItem 应该包含 ProductID
		pResp, err := s.productClient.GetProduct(ctx, &product.GetProductRequest{Id: 1})
		if err != nil {
			continue
		}

		// 找到对应的 SKU 价格
		var skuPrice float64 = 0
		var skuName string = "未知规格"
		for _, sku := range pResp.Skus {
			if sku.Id == cartItem.SkuId {
				skuPrice = float64(sku.Price)
				skuName = sku.Name
				break
			}
		}

		// 累加金额
		itemTotal := skuPrice * float64(cartItem.Quantity)
		totalAmount += itemTotal

		// 组装订单项
		orderItems = append(orderItems, model.OrderItem{
			ProductID:   pResp.Product.Id,
			SkuID:       cartItem.SkuId,
			ProductName: pResp.Product.Name,
			SkuName:     skuName,
			Price:       skuPrice,
			Quantity:    cartItem.Quantity,
			Picture:     pResp.Product.Picture,
		})

		// 准备扣库存的数据
		stockRequests = append(stockRequests, &product.StockInfo{
			SkuId: cartItem.SkuId,
			Count: cartItem.Quantity,
		})
	}

	// 3. 远程扣减库存 (分布式事务的关键点，这里采用 Best Effort)
	stockResp, err := s.productClient.DecreaseStock(ctx, &product.DecreaseStockRequest{
		Requests: stockRequests,
	})
	if err != nil || !stockResp.Success {
		return nil, fmt.Errorf("failed to decrease stock: %v", err)
	}

	// 4. 创建本地订单 (DB Transaction)
	orderNo := uuid.New().String() // 生成唯一订单号
	newOrder := model.Order{
		OrderNo:     orderNo,
		UserID:      req.UserId,
		TotalAmount: totalAmount,
		Status:      0, // 待支付
		Items:       orderItems,
	}

	// GORM 级联创建：创建 Order 时会自动创建 Items
	if err := s.db.Create(&newOrder).Error; err != nil {
		// 严重错误：库存扣了，订单没创建成功。
		// 实际场景需要发消息队列进行“库存回滚” (Compensating Transaction)
		log.Printf("CRITICAL: Order creation failed but stock decreased. User: %d", req.UserId)
		return nil, fmt.Errorf("failed to create order")
	}

	// 5. 清空购物车
	_, _ = s.cartClient.EmptyCart(ctx, &cart.EmptyCartRequest{UserId: req.UserId})

	return &order.CreateOrderResponse{
		OrderNo:     orderNo,
		TotalAmount: float32(totalAmount),
	}, nil
}

func (s *server) ListOrders(ctx context.Context, req *order.ListOrdersRequest) (*order.ListOrdersResponse, error) {
	var orders []model.Order
	// 预加载 Items
	if err := s.db.Where("user_id = ?", req.UserId).Preload("Items").Find(&orders).Error; err != nil {
		return nil, err
	}

	var protoOrders []*order.OrderInfo
	for _, o := range orders {
		var items []*order.OrderItem
		for _, i := range o.Items {
			items = append(items, &order.OrderItem{
				ProductName: i.ProductName,
				SkuName:     i.SkuName,
				Price:       float32(i.Price),
				Quantity:    i.Quantity,
			})
		}
		protoOrders = append(protoOrders, &order.OrderInfo{
			OrderNo:     o.OrderNo,
			TotalAmount: float32(o.TotalAmount),
			Status:      o.Status,
			Items:       items,
		})
	}
	return &order.ListOrdersResponse{Orders: protoOrders}, nil
}

// MarkOrderPaid 支付成功回调
func (s *server) MarkOrderPaid(ctx context.Context, req *order.MarkOrderPaidRequest) (*order.MarkOrderPaidResponse, error) {
	// 更新数据库状态 status = 1 (已支付)
	result := s.db.Model(&model.Order{}).Where("order_no = ?", req.OrderNo).Update("status", 1)
	if result.Error != nil {
		return nil, result.Error
	}
	return &order.MarkOrderPaidResponse{Success: true}, nil
}

func main() {
	// 1. 加载配置
	c, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. 初始化数据库 (db_order)
	db, err := database.InitMySQL(c.Mysql)
	if err != nil {
		log.Fatalf("Failed to init mysql: %v", err)
	}
	db.AutoMigrate(&model.Order{}, &model.OrderItem{})

	// 3. 初始化 GRPC 客户端 (Cart & Product)
	// 连接 Cart Service
	cartConn, err := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "cart-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatal(err)
	}
	cartClient := cart.NewCartServiceClient(cartConn)

	// 连接 Product Service
	productConn, err := grpc.Dial(
		fmt.Sprintf("consul://%s/%s?wait=14s", c.Consul.Address, "product-service"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatal(err)
	}
	productClient := product.NewProductServiceClient(productConn)

	// 4. 启动服务
	addr := fmt.Sprintf(":%d", c.Service.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// 注册到 Consul
	discovery.RegisterService(c.Service.Name, c.Service.Port, c.Consul.Address)

	s := grpc.NewServer()
	order.RegisterOrderServiceServer(s, &server{
		db:            db,
		cartClient:    cartClient,
		productClient: productClient,
	})
	reflection.Register(s)

	log.Printf("Order Service listening on %s", addr)

	go func() {
		s.Serve(lis)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	s.GracefulStop()
}
