FROM golang:1.25-alpine AS builder

WORKDIR /app

# 1. 下载依赖
COPY go.mod go.sum ./
# 设置代理，防止在容器里下载依赖卡住
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download

# 2. 复制源码
COPY . .

# 3. 接收构建参数
ARG APP_PATH
# 编译
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ${APP_PATH}/main.go

# Run Stage
FROM alpine:latest

WORKDIR /app

# 1. 从构建层复制二进制文件
COPY --from=builder /app/server .

# 2. 接收配置路径参数，复制对应的 config.yaml
ARG APP_PATH
COPY ${APP_PATH}/config.yaml .

# 3. 暴露端口
EXPOSE 8080 50051 50052 50053 50054 50055

# 4. 启动服务
CMD ["./server"]