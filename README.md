# Gomall - Go Microservices E-commerce System

**Gomall** 是一个基于 Go 语言开发的 B2C 微服务电商系统。项目采用前后端分离架构，后端使用 gRPC 进行服务间通信，Gin 作为 HTTP 网关，结合 Consul 实现服务注册与发现。

## 🛠 技术栈 (Tech Stack)

* **开发语言**: Go 1.21+
* **Web 框架**: Gin
* **RPC 框架**: gRPC + Protobuf
* **ORM 框架**: GORM
* **数据库**: MySQL 8.0
* **配置管理**: Viper
* **服务发现**: Consul
* **网关路由**: gRPC-Consul-Resolver

## 📂 目录结构 (Directory Structure)

```text
go-ecommerce/
├── apps/                   # 微服务应用源码
│   ├── gateway/            # API 网关 (HTTP -> gRPC)
│   ├── user/               # 用户服务 (User Service)
│   ├── product/            # 商品服务 (Product Service) [TODO]
│   └── order/              # 订单服务 (Order Service) [TODO]
├── pkg/                    # 公共依赖库
│   ├── config/             # 配置读取
│   ├── database/           # 数据库连接
│   ├── discovery/          # Consul 服务注册工具
│   └── utils/              # 通用工具 (加密等)
├── proto/                  # Protobuf 协议定义
├── deploy/                 # 基础设施编排 (Docker Compose)
├── go.mod                  # 依赖管理
└── README.md               # 项目文档
```

## 🚀 快速开始 (Quick Start)

### 1. 环境准备

确保本地已安装：

* Go 1.21+
* Docker & Docker Compose

### 2. 启动基础设施

启动 MySQL, Redis, Consul：

**Bash**

```
cd deploy
docker-compose up -d
```

### 3. 初始化数据库

连接 MySQL (User: root, Pass: root)，创建用户服务数据库：

**SQL**

```
CREATE DATABASE db_user CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
```

### 4. 启动微服务

 **启动用户服务 (User Service)** :

**Bash**

```
cd apps/user
# 此时会自动连接 DB 并创建 users 表，同时注册到 Consul
go run main.go
```

 **启动网关 (Gateway)** :

**Bash**

```
cd apps/gateway
# 网关会通过 Consul 发现 User Service
go run main.go
```

## 🧪 接口测试 (API Testing)

### 用户注册

**Bash**

```
curl -X POST http://localhost:8080/api/v1/user/register \
     -H "Content-Type: application/json" \
     -d '{"username":"admin", "password":"password123", "mobile":"13800138000"}'
```

### 用户登录

**Bash**

```
curl -X POST http://localhost:8080/api/v1/user/login \
     -H "Content-Type: application/json" \
     -d '{"username":"admin", "password":"password123"}'
```

## 📅 开发计划 (Roadmap)

* [X] **Phase 1: 基础设施与用户体系**
  * [X] 项目骨架搭建 (Monorepo)
  * [X] Docker 环境 (MySQL, Consul)
  * [X] User Service (gRPC, GORM, BCrypt)
  * [X] Gateway (Gin, Consul Resolver)
* [ ] **Phase 2: 商品服务 (Product Service)**
  * [ ] 商品/类目表设计
  * [ ] 商品列表与详情接口
* [ ] **Phase 3: 交易闭环**
  * [ ] 购物车 (Redis)
  * [ ] 订单系统
  * [ ] 分布式事务处理
* [ ] **Phase 4: 运维与监控**
  * [ ] 链路追踪 (Jaeger)
  * [ ] 容器化部署

## 📝 License

MIT

```

---

### 3. Git 提交建议

文件准备好后，你可以按照以下步骤将代码提交到仓库：

```bash
# 1. 初始化 git 仓库
git init

# 2. 添加所有文件 (会根据 .gitignore 自动过滤)
git add .

# 3. 提交
git commit -m "feat: init project skeleton with user service and gateway"

# 4. (可选) 关联远程仓库并推送
# git remote add origin <你的远程仓库地址>
# git push -u origin master
```
