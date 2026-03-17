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

// ReportTenantPre 租户商户日终报告（批量）
// POST /api/jobs/report/tenant/pre/
func (h *JobsHandler) ReportTenantPre(c *gin.Context) {
	go h.JobsSvc.ReportTenantPre()
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "租户商户日终报告任务已提交",
		"data": nil,
	})
}

// ReportSplitPre 租户归集日终报告（批量）
// POST /api/jobs/report/split/pre/
func (h *JobsHandler) ReportSplitPre(c *gin.Context) {
	go h.JobsSvc.ReportSplitPre()
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "租户归集日终报告任务已提交",
		"data": nil,
	})
}

// ReportMerchantPre 商户日终报告（批量）
// POST /api/jobs/report/merchant/pre/
func (h *JobsHandler) ReportMerchantPre(c *gin.Context) {
	go h.JobsSvc.ReportMerchantPre()
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "商户日终报告任务已提交",
		"data": nil,
	})
}

// ReportWriteoffPre 核销日终报告（批量）
// POST /api/jobs/report/writeoff/pre/
func (h *JobsHandler) ReportWriteoffPre(c *gin.Context) {
	go h.JobsSvc.ReportWriteoffPre()
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "核销日终报告任务已提交",
		"data": nil,
	})
}

// ReportTenantPreOne 单个租户商户日终报告
// POST /api/jobs/report/tenant/pre/one/
func (h *JobsHandler) ReportTenantPreOne(c *gin.Context) {
	var req struct {
		TenantID uint `json:"tenant_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 4000,
			"msg":  "参数错误: tenant_id 必填",
			"data": nil,
		})
		return
	}
	if err := h.JobsSvc.ReportTenantPreOne(req.TenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 4000,
			"msg":  fmt.Sprintf("发送失败: %v", err),
			"data": nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "发送成功",
		"data": nil,
	})
}

// ReportSplitPreOne 单个租户归集日终报告
// POST /api/jobs/report/split/pre/one/
func (h *JobsHandler) ReportSplitPreOne(c *gin.Context) {
	var req struct {
		TenantID uint `json:"tenant_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 4000,
			"msg":  "参数错误: tenant_id 必填",
			"data": nil,
		})
		return
	}
	if err := h.JobsSvc.ReportSplitPreOne(req.TenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 4000,
			"msg":  fmt.Sprintf("发送失败: %v", err),
			"data": nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "发送成功",
		"data": nil,
	})
}

// ReportMerchantPreOne 单个商户日终报告
// POST /api/jobs/report/merchant/pre/one/
func (h *JobsHandler) ReportMerchantPreOne(c *gin.Context) {
	var req struct {
		MerchantID uint `json:"merchant_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 4000,
			"msg":  "参数错误: merchant_id 必填",
			"data": nil,
		})
		return
	}
	if err := h.JobsSvc.ReportMerchantPreOne(req.MerchantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 4000,
			"msg":  fmt.Sprintf("发送失败: %v", err),
			"data": nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  "发送成功",
		"data": nil,
	})
}

// CheckUserLogin 检查用户登录（自动关闭长期未登录用户）
// POST /api/jobs/check/user/login/
func (h *JobsHandler) CheckUserLogin(c *gin.Context) {
	count, err := h.JobsSvc.CheckUserLogin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 4000,
			"msg":  fmt.Sprintf("检查用户登录失败: %v", err),
			"data": nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 2000,
		"msg":  fmt.Sprintf("关闭了 %d 个用户", count),
		"data": gin.H{"count": count},
	})
}
