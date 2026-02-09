package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 配置
const (
	GatewayURL = "http://localhost:8080/api/v1/product/seckill"
	SecretKey  = "my_secret_key" // 必须与 Gateway/User 服务一致
	SkuID      = 2               // 抢购商品 ID
	TotalUsers = 50              // 模拟抢购人数
)

// 统计器
var (
	successCount int
	failCount    int
	mu           sync.Mutex
)

// GenerateToken 生成测试用的 JWT
func GenerateToken(userId int64) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(userId), // 注意：JSON 数字解析通常是 float64
		"exp":     time.Now().Add(time.Hour).Unix(),
		"iss":     "go-ecommerce",
	})
	return token.SignedString([]byte(SecretKey))
}

// SeckillRequest 发起单个抢购请求
func SeckillRequest(userId int64, wg *sync.WaitGroup) {
	defer wg.Done()

	// 1. 生成 Token
	token, _ := GenerateToken(userId)

	// 2. 构造请求体
	reqBody := map[string]int64{"sku_id": SkuID}
	jsonBody, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", GatewayURL, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// 3. 发送请求
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[User %d] 请求失败: %v\n", userId, err)
		return
	}
	defer resp.Body.Close()

	// 4. 解析结果
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	mu.Lock()
	defer mu.Unlock()

	// 这里的 code 是我们在 pkg/response 定义的 200
	if code, ok := result["code"].(float64); ok && code == 200 {
		fmt.Printf("🟢 [User %d] 抢购成功!\n", userId)
		successCount++
	} else {
		msg := result["msg"]
		fmt.Printf("🔴 [User %d] 抢购失败: %v\n", userId, msg)
		failCount++
	}
}

func main() {
	fmt.Printf("🚀 开始秒杀测试！库存: 5, 参与人数: %d\n", TotalUsers)
	fmt.Println("------------------------------------------------")

	var wg sync.WaitGroup
	wg.Add(TotalUsers)

	startTime := time.Now()

	// 模拟 TotalUsers 个用户同时抢购
	for i := 0; i < TotalUsers; i++ {
		userId := int64(1000 + i) // 用户ID 从 1000 开始
		go SeckillRequest(userId, &wg)
	}

	wg.Wait()

	fmt.Println("------------------------------------------------")
	fmt.Printf("🏁 测试结束，耗时: %v\n", time.Since(startTime))
	fmt.Printf("✅ 成功抢到: %d 人\n", successCount)
	fmt.Printf("❌ 抢购失败: %d 人\n", failCount)
}
