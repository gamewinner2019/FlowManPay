package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Standard response structure matching Django's DetailResponse/ErrorResponse
type Response struct {
	Code int         `json:"code"`
	Data interface{} `json:"data"`
	Msg  string      `json:"msg"`
}

// PageData represents paginated data matching Django's CustomPagination.get_paginated_response
type PageData struct {
	Page       int         `json:"page"`
	Total      int64       `json:"total"`
	Limit      int         `json:"limit"`
	IsNext     bool        `json:"is_next"`
	IsPrevious bool        `json:"is_previous"`
	Data       interface{} `json:"data"`
}

// DetailResponse returns a success response with data
func DetailResponse(c *gin.Context, data interface{}, msg string) {
	if msg == "" {
		msg = "获取成功"
	}
	c.JSON(http.StatusOK, Response{
		Code: 2000,
		Data: data,
		Msg:  msg,
	})
}

// ErrorResponse returns an error response
func ErrorResponse(c *gin.Context, msg string, code ...int) {
	respCode := 400
	if len(code) > 0 {
		respCode = code[0]
	}
	if msg == "" {
		msg = "请求失败"
	}
	c.JSON(http.StatusOK, Response{
		Code: respCode,
		Data: nil,
		Msg:  msg,
	})
}

// ErrorResponseWithCode 带错误码的错误响应
func ErrorResponseWithCode(c *gin.Context, code int, msg string) {
	if msg == "" {
		msg = "请求失败"
	}
	c.JSON(http.StatusOK, Response{
		Code: code,
		Data: nil,
		Msg:  msg,
	})
}

// PageResponse returns a paginated success response
func PageResponse(c *gin.Context, data interface{}, total int64, page, limit int, msg string) {
	if msg == "" {
		msg = "获取成功"
	}
	if data == nil {
		data = []interface{}{}
		msg = "暂无数据"
	}
	isNext := int64(page*limit) < total
	isPrevious := page > 1
	c.JSON(http.StatusOK, Response{
		Code: 2000,
		Data: PageData{
			Page:       page,
			Total:      total,
			Limit:      limit,
			IsNext:     isNext,
			IsPrevious: isPrevious,
			Data:       data,
		},
		Msg: msg,
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
