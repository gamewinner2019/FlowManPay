package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/service"
)

// JobsHandler 定时任务手动触发处理器
type JobsHandler struct {
	DB      *gorm.DB
	JobsSvc *service.JobsService
}

// NewJobsHandler 创建定时任务处理器
func NewJobsHandler(db *gorm.DB, jobsSvc *service.JobsService) *JobsHandler {
	return &JobsHandler{DB: db, JobsSvc: jobsSvc}
}

// CheckNoSplitHistory 未分账检查
// POST /api/jobs/check_no_split_history/
func (h *JobsHandler) CheckNoSplitHistory(c *gin.Context) {
	count, err := h.JobsSvc.CheckNoSplitHistory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 4000,
			"msg":  fmt.Sprintf("未分账检查失败: %v", err),
			"data": nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  fmt.Sprintf("重新分账任务批量添加成功: %d", count),
		"data": gin.H{"count": count},
	})
}

// DeleteOrder 订单删除
// POST /api/jobs/delete_order/
func (h *JobsHandler) DeleteOrder(c *gin.Context) {
	count, err := h.JobsSvc.DeleteOrder()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 4000,
			"msg":  fmt.Sprintf("订单删除失败: %v", err),
			"data": nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "ok",
		"data": gin.H{"deleted": count},
	})
}

// DeleteLog 日志清理
// POST /api/jobs/delete_log/
func (h *JobsHandler) DeleteLog(c *gin.Context) {
	count, err := h.JobsSvc.DeleteLog()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 4000,
			"msg":  fmt.Sprintf("日志清理失败: %v", err),
			"data": nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "ok",
		"data": gin.H{"deleted": count},
	})
}

// AutoTransfer 自动转账
// POST /api/jobs/auto_transfer/
func (h *JobsHandler) AutoTransfer(c *gin.Context) {
	count, err := h.JobsSvc.AutoTransfer()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 4000,
			"msg":  fmt.Sprintf("自动转账失败: %v", err),
			"data": nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "成功",
		"data": gin.H{"count": count},
	})
}
