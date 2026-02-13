FROM golang:1.25-alpine AS builder

WORKDIR /app

# 1. 下载依赖
COPY go.mod go.sum ./
# 设置代理，防止在容器里下载依赖卡住
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download

# 2. 复制源码
COPY . .

# 3. 接收构建参数 (例如: apps/address)
ARG APP_PATH
# 编译 (生成二进制文件到 /app/server)
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ./${APP_PATH}/main.go

# --------------------------------------------------------------------------------
# Run Stage
# --------------------------------------------------------------------------------
FROM alpine:latest

WORKDIR /app

# 替换 Alpine 源为阿里云源，加快 apk 安装速度 (可选，但在国内很有用)
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk add --no-cache tzdata

# 1. 从构建层复制二进制文件
COPY --from=builder /app/server .

# 2. 接收配置路径参数，复制对应的 config.yaml
# 注意：这要求每个 apps/xxx/ 目录下必须有 config.yaml
ARG APP_PATH
COPY ${APP_PATH}/config.yaml .

# 3. 暴露端口
# 8080: Gateway
# 50051: User, 50052: Product, 50053: Cart, 50054: Order, 50055: Payment
# [新增] 50056: Address
EXPOSE 8080 50051 50052 50053 50054 50055 50056 50057 50058

# 4. 启动服务
CMD ["./server"]