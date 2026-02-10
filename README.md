# 🛍️ Go Microservices Ecommerce System (Go微服务电商系统)![Microservices Architecture Diagram的图片](https://encrypted-tbn3.gstatic.com/licensed-image?q=tbn:ANd9GcQiDB_UeyPIoNWGPkQTcqQOoGURoHRVJ4A_kTfnp1ieahwq1enJenhgU-9cwwNVZnme2ZaQbzcYZwhhTEUemDEI9uJm9xnf14HUIrHRUMZfcGMOhoM)**Shutterstock**

基于 **Go (Gin + gRPC)** 构建的高性能、高并发、企业级微服务电商系统。

本项目涵盖了电商系统的核心业务场景，包括用户、商品搜索、购物车、订单、支付、地址管理，以及 **高并发秒杀（Seckill）** 。采用了主流的微服务治理技术栈，如 **Consul** 服务发现、**Sentinel** 限流、**Jaeger** 链路追踪、**RabbitMQ** 消息削峰与死信队列、**Elasticsearch** 全文检索、**Redis + Lua** 原子库存扣减。

## 🚀 项目亮点 (Key Features)

* **微服务架构** ：基于 DDD 领域驱动设计，拆分为网关、用户、商品、订单、购物车、支付、地址等 7 个微服务。
* **高并发秒杀** ：
* **流量削峰** ：使用 RabbitMQ 异步处理下单请求。
* **抗压黑科技** ：Redis + Lua 脚本实现原子性库存扣减，防止超卖。
* **服务保护** ：接入 **Sentinel** 实现网关层 QPS 限流，瞬间流量直接拒绝。
* **搜索引擎** ：
* 集成  **Elasticsearch 7.x** ，实现商品关键词毫秒级全文检索。
* 支持 MySQL 数据启动时自动全量同步至 ES。
* **订单高级特性** ：
* **超时自动取消** ：基于 RabbitMQ **死信队列 (DLX)** 实现订单 60秒未支付自动关闭与库存回滚。
* **可观测性** ：
* 集成 **OpenTelemetry** 与  **Jaeger** ，实现全链路分布式追踪，可视化请求调用链与耗时。
* **容器化部署** ：全套环境 (MySQL, Redis, RabbitMQ, ES, Consul, Jaeger) 一键 Docker Compose 部署。

## 🛠️ 技术栈 (Tech Stack)

| **类别**     | **技术组件**      | **说明**                 |
| ------------------ | ----------------------- | ------------------------------ |
| **编程语言** | Go (Golang)             | 1.20+                          |
| **Web 框架** | Gin                     | 高性能 HTTP Web 框架 (Gateway) |
| **RPC 框架** | gRPC + Protobuf         | 微服务间的高效通信             |
| **服务发现** | Hashicorp Consul        | 服务注册与发现、健康检查       |
| **数据库**   | MySQL 8.0               | 核心业务数据存储 (GORM ORM)    |
| **缓存**     | Redis 7.0               | 缓存、分布式锁、秒杀库存计数   |
| **消息队列** | RabbitMQ                | 异步解耦、流量削峰、延迟队列   |
| **搜索引擎** | Elasticsearch 7.17      | 商品全文检索                   |
| **限流熔断** | Sentinel                | 流量控制、熔断降级             |
| **链路追踪** | Jaeger (OpenTelemetry)  | 分布式调用链监控               |
| **部署**     | Docker & Docker Compose | 容器化编排                     |

## 📂 项目结构 (Directory Structure)

**Bash**

```
go-ecommerce/
├── apps/                   # 微服务源码
│   ├── gateway/            # [入口] API 网关 (Gin + Sentinel + OTEL)
│   ├── user/               # 用户服务 (gRPC + MySQL)
│   ├── product/            # 商品服务 (gRPC + MySQL + ES + Redis)
│   ├── order/              # 订单服务 (gRPC + MySQL + RabbitMQ)
│   ├── cart/               # 购物车服务 (gRPC + Redis)
│   ├── payment/            # 支付服务 (gRPC + Mock)
│   └── address/            # 地址服务 (gRPC + MySQL)
├── pkg/                    # 公共包 (配置、DB、Tracer、Response)
├── proto/                  # Protobuf 定义文件 (.proto)
├── scripts/                # 工具脚本 (如压测脚本)
├── docker-compose-full.yml # 完整环境部署文件
├── go.mod                  # 依赖管理
└── README.md               # 项目说明
```

## ⚡️ 快速开始 (Quick Start)

### 1. 环境准备

确保本地已安装：

* Docker & Docker Compose
* Go 1.20+ (仅开发需要，运行只需 Docker)

### 2. 一键启动

在项目根目录下执行：

**PowerShell**

```
# 启动所有基础设施(DB, MQ, ES等)和微服务
docker-compose -f docker-compose-full.yml up --build -d
```

*初次启动可能需要几分钟下载镜像和编译 Go 代码，请耐心等待。*

### 3. 服务验证

* **API 网关** : `http://localhost:8080`
* **Consul 后台** : `http://localhost:8500` (查看服务注册状态)
* **RabbitMQ 后台** : `http://localhost:15672` (账号/密码: guest/guest)
* **Jaeger 追踪** : `http://localhost:16686`

---

## 🧪 核心功能测试指南

### 1. 基础流程 (注册 -> 下单 -> 支付)

1. **注册用户** :
   **Bash**

```
   curl -X POST http://localhost:8080/api/v1/user/register -d '{"username":"test_user","password":"123","mobile":"13800000000"}'
```

1. **登录获取 Token** :
   **Bash**

```
   curl -X POST http://localhost:8080/api/v1/user/login -d '{"username":"test_user","password":"123"}'
   # 复制返回的 token
```

1. **添加地址** : (需 Header: `Authorization: Bearer <TOKEN>`)
   **Bash**

```
   curl -X POST http://localhost:8080/api/v1/address/add -H "Authorization: Bearer <TOKEN>" -d '{"name":"张三","mobile":"13800138000","province":"北京","city":"北京","district":"朝阳","detail_address":"国贸大厦"}'
```

1. **商品搜索 (ES)** :
   **Bash**

```
   curl -X GET "http://localhost:8080/api/v1/product/list?query=西红柿"
```

### 2. 高并发秒杀测试 (Seckill)

本项目包含一个 Go 编写的并发压测脚本，模拟多用户瞬间抢购。

**第一步：预热库存 (Redis)**

**Bash**

```
# 设置 SKU ID=2 的库存为 5 个
docker exec -it deploy-redis redis-cli SET seckill:stock:2 5
# 清除之前的去重记录
docker exec -it deploy-redis redis-cli DEL seckill:user:2
```

**第二步：运行压测脚本**

**Bash**

```
# 模拟 50 个用户并发抢购
go run scripts/seckill_load.go
```

 **预期结果** :

* 控制台显示 **5 个** `🟢 抢购成功`。
* 部分显示 `🔴 手慢了` (Redis 拦截)。
* 大部分显示 `🔴 系统繁忙` (Sentinel 限流拦截)。
* 查看数据库 `db_order.orders` 表，会新增 5 条 `SK` 开头的秒杀订单。

### 3. 订单超时取消测试

1. 创建普通订单但不支付。
2. 观察 RabbitMQ `order.delay.queue` 有消息积压。
3. 等待 60 秒。
4. 消息进入死信队列，被 Order Service 消费，订单状态变更为 `2 (已取消)`，且库存自动回滚。

### 4. 全链路追踪 (Jaeger)

1. 发起任意 API 请求。
2. 访问 `http://localhost:16686`。
3. 选择 Service: `gateway`，点击 Find Traces。
4. 查看完整的调用瀑布图：`Gateway -> Auth Middleware -> Order Service -> Product Service -> DB`。

---

## 🔧 常见问题 (FAQ)

* **Q: 启动时报错 `connection refused`?**
  * A: 基础设施（MySQL/Consul）启动需要时间，微服务可能会重试几次，稍等片刻即可，或者使用 `docker-compose restart <service_name>` 重启服务。
* **Q: ES 搜索不到数据?**
  * A: Product Service 仅在启动时同步数据。如果数据库是空的，请先手动插入数据，然后重启 Product Service 触发同步。
* **Q: 秒杀一直提示"系统繁忙"?**
  * A: 这是 Sentinel 限流生效了（默认 QPS=5）。这是预期行为，证明系统被保护住了。

---

## 👨‍💻 作者

Developed with ❤️ by  **Huarou** .

如果你觉得这个项目对你有帮助，请给个 Star ⭐️！

```



# protoc命令生成
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/cart/cart.proto

容器命令
🚀 启动与构建
后台启动所有服务 (最常用)：

PowerShell
docker-compose -f docker-compose-full.yml up -d
(加上 -d 是为了让它在后台跑，不会占住你的终端)

重新构建并启动 (当你修改了 Go 代码后)：

PowerShell
docker-compose -f docker-compose-full.yml up -d --build
只启动某个特定服务 (例如只重启网关)：

PowerShell
docker-compose -f docker-compose-full.yml up -d gateway
2. 🛑 停止与重启
停止所有服务 (保留数据卷)：

PowerShell
docker-compose -f docker-compose-full.yml stop
停止并删除容器 (清理环境，下次启动会重新创建容器，但保留数据)：

PowerShell
docker-compose -f docker-compose-full.yml down
重启某个服务 (修改配置或代码后)：

PowerShell
docker-compose -f docker-compose-full.yml restart user-service
3. 🔍 查看状态与日志
查看运行中的容器：

PowerShell
docker-compose -f docker-compose-full.yml ps
实时查看所有日志：

PowerShell
docker-compose -f docker-compose-full.yml logs -f
查看某个服务的日志 (比如排查为什么 User Service 挂了)：

PowerShell
docker-compose -f docker-compose-full.yml logs -f user-service
(加上 --tail 100 可以只看最后 100 行，防止日志太长)

4. 🧹 彻底清理 (核弹操作)
停止服务 + 删除容器 + 删除所有数据卷 (相当于重置回出厂设置，数据会丢失！)：

PowerShell
docker-compose -f docker-compose-full.yml down -v


5. 精准清除 Elasticsearch 索引,redis
docker exec deploy-elasticsearch curl -X DELETE http://localhost:9200/products
docker exec deploy-redis redis-cli FLUSHALL
```
