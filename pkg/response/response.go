package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 统一响应结构体
type Response struct {
	Code int         `json:"code"`           // 业务码
	Msg  string      `json:"msg"`            // 提示信息
	Data interface{} `json:"data,omitempty"` // 数据
}

// Success 成功响应 (Code=200)
func Success(ctx *gin.Context, data interface{}) {
	ctx.JSON(http.StatusOK, Response{
		Code: 200,
		Msg:  "success",
		Data: data,
	})
}

// Error 失败响应
func Error(ctx *gin.Context, httpStatus int, msg string) {
	ctx.JSON(httpStatus, Response{
		Code: httpStatus, // 这里简单将 HTTP 状态码作为业务码，也可以自定义
		Msg:  msg,
		Data: nil,
	})
}
