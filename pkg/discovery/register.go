package discovery

import (
	"fmt"
	"log"
	"net"

	"github.com/hashicorp/consul/api"
)

// RegisterService 将服务注册到 Consul
func RegisterService(serviceName string, servicePort int, consulAddr string) error {
	// 1. 获取 Consul 客户端
	config := api.DefaultConfig()
	config.Address = consulAddr
	client, err := api.NewClient(config)
	if err != nil {
		return err
	}

	// 2. 获取本机 IP (非 Loopback)
	localIP, err := getOutboundIP()
	if err != nil {
		return err
	}

	// 3. 创建注册对象
	// ID 必须唯一，通常使用 "服务名-IP-端口"
	serviceID := fmt.Sprintf("%s-%s-%d", serviceName, localIP, servicePort)

	registration := &api.AgentServiceRegistration{
		ID:      serviceID,
		Name:    serviceName,
		Port:    servicePort,
		Address: localIP, // 自动获取的本机局域网 IP
		Tags:    []string{"gomall", "grpc"},
		Check: &api.AgentServiceCheck{
			// 这里使用 TCP 检查，Consul 会定期 ping 这个端口看服务活没活着
			TCP:                            fmt.Sprintf("%s:%d", localIP, servicePort),
			Interval:                       "10s",
			Timeout:                        "5s",
			DeregisterCriticalServiceAfter: "30s", // 挂了30秒后自动注销
		},
	}

	// 4. 发送注册请求
	if err := client.Agent().ServiceRegister(registration); err != nil {
		return err
	}

	log.Printf("Service Registered: %s (ID: %s) at %s:%d", serviceName, serviceID, localIP, servicePort)
	return nil
}

// getOutboundIP 获取本机对外 IP
// 因为如果是 Docker 或局域网，不能注册 127.0.0.1，否则网关找不到
func getOutboundIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}
