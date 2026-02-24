
# 🌿 Go-Shouguang Fresh: 企业级微服务生鲜系统

本项目是一个基于 **Go (Gin + gRPC)** 构建的高性能、高并发生鲜电商微服务系统。以**“寿光蔬菜”**为业务背景，完整打通了从 C 端用户浏览下单，到 B 端管理员大屏监控、动态调价、全链路追踪的全套业务闭环。

## 🚀 核心亮点 (Key Features)

* **🏢 领域驱动微服务 (DDD)** ：系统垂直拆分为网关(Gateway)、用户(User)、商品(Product)、订单(Order)、支付(Payment)、地址(Address) 以及 **管理后台(Admin)** 7 大核心服务。
* **🥬 智能化生鲜库存管理** ：
* **市场波动调价** ：支持管理员基于分类（如：茄果类、瓜果类）一键批量上调/下调商品价格。
* **数字化大屏** ：实时聚合 GMV（总流水）、实际成交额、各品类占比及 7 日销量趋势。
* **⚡ 高并发秒杀 (Seckill)** ：
* **前置拦截** ：基于 Redis + Lua 脚本实现绝对原子性的库存扣减，彻底杜绝超卖。
* **削峰填谷** ：RabbitMQ 异步解耦下单请求，保护底层 MySQL 数据库。
* **🛡️ 高可用与服务治理** ：
* **限流熔断** ：接入 **Sentinel** 实现接口级 QPS 限流（例如拦截恶意秒杀流量）。
* **超时关单** ：利用 RabbitMQ **死信队列 (DLX)** 实现订单超时（测试设为 60s）自动取消并回滚库存。
* **🔍 全文检索与可观测性** ：
* **Elasticsearch** ：支持百万级商品数据的毫秒级检索及高亮显示，与 MySQL 数据保持同步。
* **OpenTelemetry + Jaeger** ：实现 HTTP 与 gRPC 跨服务调用的全链路追踪，性能瓶颈一目了然。

## 🛠️ 技术栈图谱 (Tech Stack)

| **类别**     | **技术方案**         | **核心作用**                      |
| ------------------ | -------------------------- | --------------------------------------- |
| **基础框架** | Go / Gin / gRPC / Protobuf | 高性能 HTTP 网关与内部二进制 RPC 通信   |
| **服务治理** | Hashicorp Consul           | 服务自动注册与发现、健康检查            |
| **数据存储** | MySQL 8.0 / GORM           | 核心业务强一致性存储 (订单、用户、商品) |
| **缓存与锁** | Redis 7.0                  | 秒杀库存预热、分布式锁、热点数据缓存    |
| **消息队列** | RabbitMQ                   | 异步下单、微服务解耦、延迟死信队列      |
| **搜索引擎** | Elasticsearch 7.17         | 商品高频关键字搜索与分类聚合            |
| **熔断观测** | Sentinel / Jaeger          | QPS 流量控制、分布式调用链路可视化      |
| **容器部署** | Docker & Docker Compose    | 数据库、中间件及微服务一键容器化编排    |

## 📂 微服务架构目录

**Plaintext**

```
go-ecommerce/
├── apps/                   # 核心微服务源码
│   ├── gateway/            # 🌐 [网关] 统一入口、JWT 鉴权、Sentinel 限流
│   ├── admin/              # 📊 [后台] 数据大屏、批量调价、库存与用户管理
│   ├── user/               # 👤 [用户] 身份认证、密码管理
│   ├── product/            # 🥬 [商品] 商品详情、ES 检索、秒杀扣库存
│   ├── order/              # 🛒 [订单] 订单状态机、RabbitMQ 异步收发
│   ├── payment/            # 💳 [支付] 模拟支付网关接入
│   ├── cart/               # 🛍️ [购物] 购物车数据维护
│   └── address/            # 📍 [地址] 用户收货地址管理
├── pkg/                    # 📦 公共组件包 (Config, GORM, Jaeger, 响应封装)
├── proto/                  # 📜 Protobuf IDL 定义文件 (所有服务契约)
└── docker-compose-full.yml # 🐳 一键部署编排文件
```

---

## ⚡ 快速启动指南 (Quick Start)

### 1. 启动基础设施与微服务

确保本地已安装 Docker Desktop，在项目根目录执行：

**Bash**

```
docker-compose -f docker-compose-full.yml up -d --build
```

> ⏳  **Tip** : 初次启动需拉取镜像及初始化 MySQL 脚本（含 32 种寿光蔬菜测试数据），请耐心等待 1-2 分钟。

### 2. 控制台访问矩阵

启动完成后，您可以通过以下地址访问系统的各个模块：

* 📱  **前端商城/后台** ：`http://localhost:5173` (运行前端 Vue 项目 `npm run dev` 即可)
* ⚙️  **Consul 注册中心** ：`http://localhost:8500` (查看服务健康状态)
* 🐰  **RabbitMQ 控制台** ：`http://localhost:15672` (账号: `guest` / 密码: `guest`)
* 👁️  **Jaeger 链路追踪** ：`http://localhost:16686` (查看请求瀑布图)

---

## 📚 常用运维与开发命令速查 (Cheat Sheet)

为了方便日常开发调试与环境管理，以下命令请在**项目根目录**下执行。

### 🛠️ 1. 代码契约与依赖管理

当您修改了接口定义或升级了依赖包时使用：

**Bash**

```
# 重新生成 Protobuf 代码 (以 admin 为例，修改路径以生成其他服务)
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/admin/admin.proto

# 清理并同步 Go 依赖 (解决各种红色波浪线和依赖冲突)
go clean -modcache
go mod tidy
```

### 🚀 2. 容器生命周期控制

精准控制各个微服务的启停：

**Bash**

```
# 后台启动所有服务 (最常用)
docker-compose -f docker-compose-full.yml up -d

# 停止所有服务 (不删除数据)
docker-compose -f docker-compose-full.yml stop

# 🔥 热更新单个服务 (修改了 gateway 代码后，只重构重启它)
docker-compose -f docker-compose-full.yml up -d --build gateway
```

### 🔍 3. 日志排查与状态监控

系统报错时，最快定位问题的手段：

**Bash**

```
# 纵览全局：查看所有容器是否都在 Running
docker ps

# 实时追踪日志：查看 API 网关的最新请求 (按 Ctrl+C 退出)
docker-compose -f docker-compose-full.yml logs -f --tail 50 gateway

# 错误排查：查看 Admin 服务是否报 SQL 错误或连不上 Consul
docker-compose -f docker-compose-full.yml logs -f admin-service
```

### 🧹 4. 环境重置与脏数据清理

测试秒杀或搞乱了数据后，用来“恢复出厂设置”：

**Bash**

```
# ⚠️ 核弹级销毁：停止容器并删除所有相关网络与数据卷 (MySQL数据将清空！)
docker-compose -f docker-compose-full.yml down -v

# 🥬 Elasticsearch 重建：删除 ES 中的商品索引，下次 Product 服务启动会自动重搜 MySQL 同步
docker exec deploy-elasticsearch curl -X DELETE http://localhost:9200/products

# ⚡ 秒杀缓存清理：清空 Redis 中所有的秒杀预热库存和防刷记录
docker exec deploy-redis redis-cli -a root FLUSHALL

# 🧪 秒杀库存预热 (测试前必做：给 ID 为 1 的西红柿设置 100 个库存)
docker exec -it deploy-redis redis-cli
> SET seckill:stock:1 100
```
