package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Standard response structure matching Django's DetailResponse/ErrorResponse
type Response struct {
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
	Msg     string      `json:"msg"`
	Success bool        `json:"success"`
}

// PageData represents paginated data
type PageData struct {
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
	Data  interface{} `json:"data"`
}

// DetailResponse returns a success response with data
func DetailResponse(c *gin.Context, data interface{}, msg string) {
	if msg == "" {
		msg = "获取成功"
	}
	c.JSON(http.StatusOK, Response{
		Code:    2000,
		Data:    data,
		Msg:     msg,
		Success: true,
	})
}

// ErrorResponse returns an error response
func ErrorResponse(c *gin.Context, msg string, code ...int) {
	respCode := 4000
	if len(code) > 0 {
		respCode = code[0]
	}
	if msg == "" {
		msg = "请求失败"
	}
	c.JSON(http.StatusOK, Response{
		Code:    respCode,
		Data:    nil,
		Msg:     msg,
		Success: false,
	})
}

// PageResponse returns a paginated success response
func PageResponse(c *gin.Context, data interface{}, total int64, page, limit int, msg string) {
	if msg == "" {
		msg = "获取成功"
	}
	c.JSON(http.StatusOK, Response{
		Code: 2000,
		Data: PageData{
			Total: total,
			Page:  page,
			Limit: limit,
			Data:  data,
		},
		Msg:     msg,
		Success: true,
	})
}

// GetPagination extracts page and limit from query params with defaults
func GetPagination(c *gin.Context) (page, limit, offset int) {
	page = 1
	limit = 10

	if p := c.Query("page"); p != "" {
		if v := atoi(p); v > 0 {
			page = v
		}
	}
	if l := c.Query("limit"); l != "" {
		if v := atoi(l); v > 0 && v <= 1000 {
			limit = v
		}
	}
	offset = (page - 1) * limit
	return
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
