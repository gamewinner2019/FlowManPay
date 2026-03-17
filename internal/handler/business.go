package handler

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
)

// BusinessHandler 业务模块处理器
type BusinessHandler struct {
	DB  *gorm.DB
	RDB *redis.Client
}

// NewBusinessHandler 创建业务模块处理器
func NewBusinessHandler(db *gorm.DB, rdb *redis.Client) *BusinessHandler {
	return &BusinessHandler{DB: db, RDB: rdb}
}

// ==================== 1. 商户通知管理 (merchant_notification) ====================

// MerchantNotificationList 商户通知列表（只读）
// GET /api/business/merchant_notification/
func (h *BusinessHandler) MerchantNotificationList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.MerchantNotification{}).
		Preload("Order").
		Preload("Order.Merchant").
		Preload("Order.Merchant.SystemUser").
		Joins("LEFT JOIN " + model.MerchantNotificationHistory{}.TableName() + " mnh ON mnh.notification_id = " + model.MerchantNotification{}.TableName() + ".id")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.Order{}.TableName()+" o ON o.id = "+model.MerchantNotification{}.TableName()+".order_id").
					Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = o.merchant_id").
					Where("m.parent_id = ?", tenant.ID)
			}
		case model.RoleKeyMerchant:
			var merchant model.Merchant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&merchant).Error; err == nil {
				query = query.Joins("JOIN "+model.Order{}.TableName()+" o ON o.id = "+model.MerchantNotification{}.TableName()+".order_id").
					Where("o.merchant_id = ?", merchant.ID)
			}
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Joins("JOIN "+model.Order{}.TableName()+" o ON o.id = "+model.MerchantNotification{}.TableName()+".order_id").
					Joins("JOIN "+model.OrderDetail{}.TableName()+" od ON od.order_id = o.id").
					Where("od.writeoff_id = ?", writeoff.ID)
			}
		}
	}

	// 筛选
	if status := c.Query("status"); status != "" {
		query = query.Where(model.MerchantNotification{}.TableName()+".status = ?", status)
	}
	if orderNo := c.Query("order_no"); orderNo != "" {
		query = query.Joins("JOIN "+model.Order{}.TableName()+" o2 ON o2.id = "+model.MerchantNotification{}.TableName()+".order_id").
			Where("o2.order_no LIKE ?", "%"+orderNo+"%")
	}
	if orderID := c.Query("order"); orderID != "" {
		query = query.Where(model.MerchantNotification{}.TableName()+".order_id = ?", orderID)
	}

	var total int64
	query.Count(&total)

	var items []model.MerchantNotification
	query.Order(model.MerchantNotification{}.TableName() + ".id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		// Get latest notification history URL
		var lastHistory model.MerchantNotificationHistory
		h.DB.Where("notification_id = ?", item.ID).Order("id DESC").First(&lastHistory)

		row := gin.H{
			"id":              item.ID,
			"order_id":        item.OrderID,
			"status":          item.Status,
			"url":             lastHistory.URL,
			"create_datetime": item.CreateDatetime,
			"update_datetime": item.UpdateDatetime,
		}
		if item.Order != nil {
			row["order_no"] = item.Order.OrderNo
			if item.Order.Merchant != nil {
				row["merchant_name"] = item.Order.Merchant.SystemUser.Name
			}
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// ==================== 2. 商户预付历史 (merchant_pre) ====================

// MerchantPreHistoryList 商户预付历史列表（只读）
// GET /api/business/merchant_pre/history/
func (h *BusinessHandler) MerchantPreHistoryList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.MerchantPreHistory{}).
		Preload("Merchant").
		Preload("Merchant.SystemUser")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyMerchant:
			var merchant model.Merchant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&merchant).Error; err == nil {
				query = query.Where("merchant_id = ?", merchant.ID)
			}
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = "+model.MerchantPreHistory{}.TableName()+".merchant_id").
					Where("m.parent_id = ?", tenant.ID)
			}
		}
	}

	if merchantID := c.Query("merchant"); merchantID != "" {
		query = query.Where(model.MerchantPreHistory{}.TableName()+".merchant_id = ?", merchantID)
	}

	var total int64
	query.Count(&total)

	var items []model.MerchantPreHistory
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":              item.ID,
			"merchant_id":     item.MerchantID,
			"pre_pay":         item.PrePay,
			"before":          item.Before,
			"after":           item.After,
			"user":            item.User,
			"rate":            item.Rate,
			"usdt":            item.USDT,
			"cert":            item.Cert,
			"create_datetime": item.CreateDatetime,
		}
		if item.Merchant != nil {
			row["merchant_name"] = item.Merchant.SystemUser.Name
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// MerchantPreHistoryStatistics 商户预付统计
// GET /api/business/merchant_pre/history/statistics/
func (h *BusinessHandler) MerchantPreHistoryStatistics(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	// baseQuery builds a fresh RBAC-filtered query each time to avoid GORM session reuse issues
	baseQuery := func() *gorm.DB {
		q := h.DB.Model(&model.MerchantPreHistory{})
		if currentUser != nil {
			switch currentUser.Role.Key {
			case model.RoleKeyMerchant:
				var merchant model.Merchant
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&merchant).Error; err == nil {
					q = q.Where("merchant_id = ?", merchant.ID)
				}
			case model.RoleKeyTenant:
				var tenant model.Tenant
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
					q = q.Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = "+model.MerchantPreHistory{}.TableName()+".merchant_id").
						Where("m.parent_id = ?", tenant.ID)
				}
			}
		}
		if merchantID := c.Query("merchant"); merchantID != "" {
			q = q.Where(model.MerchantPreHistory{}.TableName()+".merchant_id = ?", merchantID)
		}
		return q
	}

	var todayMoney, yesterdayMoney int64
	baseQuery().Where("DATE(create_datetime) = ?", today).Select("COALESCE(SUM(pre_pay), 0)").Scan(&todayMoney)
	baseQuery().Where("DATE(create_datetime) = ?", yesterday).Select("COALESCE(SUM(pre_pay), 0)").Scan(&yesterdayMoney)

	// 获取总预付
	var totalPre int64
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyMerchant {
		var merchant model.Merchant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&merchant).Error; err == nil {
			var pre model.MerchantPre
			if err := h.DB.Where("merchant_id = ?", merchant.ID).First(&pre).Error; err == nil {
				totalPre = pre.PrePay
			}
		}
	} else if merchantID := c.Query("merchant"); merchantID != "" {
		h.DB.Model(&model.MerchantPreHistory{}).Where("merchant_id = ?", merchantID).
			Select("COALESCE(SUM(pre_pay), 0)").Scan(&totalPre)
	} else {
		h.DB.Model(&model.MerchantPreHistory{}).Select("COALESCE(SUM(pre_pay), 0)").Scan(&totalPre)
	}

	response.DetailResponse(c, gin.H{
		"today_money":     todayMoney,
		"yesterday_money": yesterdayMoney,
		"total_pre":       totalPre,
	}, "")
}

// ==================== 3. 核销流水 (writeoff_flow) ====================

// WriteoffCashFlowList 核销资金流水列表（只读）
// GET /api/business/writeoff_flow/
func (h *BusinessHandler) WriteoffCashFlowList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.WriteoffCashFlow{}).
		Preload("Writeoff").
		Preload("Writeoff.SystemUser")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Where("writeoff_id = ?", writeoff.ID)
			}
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+model.WriteoffCashFlow{}.TableName()+".writeoff_id").
					Where("w.parent_id = ?", tenant.ID)
			}
		}
	}

	// 筛选
	if writeoffID := c.Query("writeoff"); writeoffID != "" {
		query = query.Where(model.WriteoffCashFlow{}.TableName()+".writeoff_id = ?", writeoffID)
	}
	if flowType := c.Query("flow_type"); flowType != "" {
		query = query.Where(model.WriteoffCashFlow{}.TableName()+".flow_type = ?", flowType)
	}
	if channelID := c.Query("pay_channel"); channelID != "" {
		query = query.Where(model.WriteoffCashFlow{}.TableName()+".pay_channel_id = ?", channelID)
	}
	if remarks := c.Query("remarks"); remarks != "" {
		query = query.Where(model.WriteoffCashFlow{}.TableName()+".description LIKE ?", "%"+remarks+"%")
	}
	if orderNo := c.Query("order_no"); orderNo != "" {
		query = query.Joins("JOIN "+model.Order{}.TableName()+" ord ON ord.id = "+model.WriteoffCashFlow{}.TableName()+".order_id").
			Where("ord.order_no LIKE ?", "%"+orderNo+"%")
	}
	if startDate := c.Query("create_datetime_after"); startDate != "" {
		query = query.Where(model.WriteoffCashFlow{}.TableName()+".create_datetime >= ?", startDate)
	}
	if endDate := c.Query("create_datetime_before"); endDate != "" {
		query = query.Where(model.WriteoffCashFlow{}.TableName()+".create_datetime <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var items []model.WriteoffCashFlow
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":              item.ID,
			"writeoff_id":     item.WriteoffID,
			"flow_type":       item.FlowType,
			"old_money":       item.OldMoney,
			"new_money":       item.NewMoney,
			"change_money":    item.ChangeMoney,
			"old_money_msg":   formatMoney(item.OldMoney),
			"new_money_msg":   formatMoney(item.NewMoney),
			"change_money_msg": formatChangeMoney(item.ChangeMoney),
			"tax":             item.Tax,
			"pay_channel_id":  item.PayChannelID,
			"order_id":        item.OrderID,
			"description":     item.Description,
			"create_datetime": item.CreateDatetime,
		}
		if item.Writeoff != nil {
			row["name"] = item.Writeoff.SystemUser.Name
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// WriteoffBrokerageFlowList 核销佣金流水列表（只读）
// GET /api/business/writeoff_flow/brokerage/
func (h *BusinessHandler) WriteoffBrokerageFlowList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.WriteoffBrokerageFlow{}).
		Preload("Writeoff").
		Preload("Writeoff.SystemUser")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Where("writeoff_id = ?", writeoff.ID)
			}
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+model.WriteoffBrokerageFlow{}.TableName()+".writeoff_id").
					Where("w.parent_id = ?", tenant.ID)
			}
		}
	}

	if writeoffID := c.Query("writeoff"); writeoffID != "" {
		query = query.Where(model.WriteoffBrokerageFlow{}.TableName()+".writeoff_id = ?", writeoffID)
	}

	var total int64
	query.Count(&total)

	var items []model.WriteoffBrokerageFlow
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":              item.ID,
			"writeoff_id":     item.WriteoffID,
			"from_writeoff_id": item.FromWriteoffID,
			"old_money":       item.OldMoney,
			"new_money":       item.NewMoney,
			"change_money":    item.ChangeMoney,
			"old_money_msg":   formatMoney(item.OldMoney),
			"new_money_msg":   formatMoney(item.NewMoney),
			"change_money_msg": formatChangeMoney(item.ChangeMoney),
			"tax":             item.Tax,
			"order_id":        item.OrderID,
			"create_datetime": item.CreateDatetime,
		}
		if item.Writeoff != nil {
			row["name"] = item.Writeoff.SystemUser.Name
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// ==================== 4. 核销预付历史 (writeoff_pre) ====================

// WriteoffPreHistoryList 核销预付历史列表（只读）
// GET /api/business/writeoff_pre/history/
func (h *BusinessHandler) WriteoffPreHistoryList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.WriteoffPreHistory{}).
		Preload("Writeoff").
		Preload("Writeoff.SystemUser")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Where("writeoff_id = ?", writeoff.ID)
			}
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+model.WriteoffPreHistory{}.TableName()+".writeoff_id").
					Where("w.parent_id = ?", tenant.ID)
			}
		}
	}

	if writeoffID := c.Query("writeoff"); writeoffID != "" {
		query = query.Where(model.WriteoffPreHistory{}.TableName()+".writeoff_id = ?", writeoffID)
	}

	var total int64
	query.Count(&total)

	var items []model.WriteoffPreHistory
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":              item.ID,
			"writeoff_id":     item.WriteoffID,
			"pre_pay":         item.PrePay,
			"before":          item.Before,
			"after":           item.After,
			"user":            item.User,
			"rate":            item.Rate,
			"usdt":            item.USDT,
			"cert":            item.Cert,
			"create_datetime": item.CreateDatetime,
		}
		if item.Writeoff != nil {
			row["writeoff_name"] = item.Writeoff.SystemUser.Name
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// WriteoffPreHistoryStatistics 核销预付统计
// GET /api/business/writeoff_pre/history/statistics/
func (h *BusinessHandler) WriteoffPreHistoryStatistics(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	baseQuery := func() *gorm.DB {
		q := h.DB.Model(&model.WriteoffPreHistory{})
		if currentUser != nil {
			switch currentUser.Role.Key {
			case model.RoleKeyWriteoff:
				var writeoff model.WriteOff
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
					q = q.Where("writeoff_id = ?", writeoff.ID)
				}
			case model.RoleKeyTenant:
				var tenant model.Tenant
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
					q = q.Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+model.WriteoffPreHistory{}.TableName()+".writeoff_id").
						Where("w.parent_id = ?", tenant.ID)
				}
			}
		}
		if writeoffID := c.Query("writeoff"); writeoffID != "" {
			q = q.Where(model.WriteoffPreHistory{}.TableName()+".writeoff_id = ?", writeoffID)
		}
		return q
	}

	var todayMoney, yesterdayMoney int64
	baseQuery().Where("DATE(create_datetime) = ?", today).Select("COALESCE(SUM(pre_pay), 0)").Scan(&todayMoney)
	baseQuery().Where("DATE(create_datetime) = ?", yesterday).Select("COALESCE(SUM(pre_pay), 0)").Scan(&yesterdayMoney)

	var totalPre int64
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyWriteoff {
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
			var pre model.WriteoffPre
			if err := h.DB.Where("writeoff_id = ?", writeoff.ID).First(&pre).Error; err == nil {
				totalPre = pre.PrePay
			}
		}
	} else if writeoffID := c.Query("writeoff"); writeoffID != "" {
		h.DB.Model(&model.WriteoffPreHistory{}).Where("writeoff_id = ?", writeoffID).
			Select("COALESCE(SUM(pre_pay), 0)").Scan(&totalPre)
	} else {
		h.DB.Model(&model.WriteoffPreHistory{}).Select("COALESCE(SUM(pre_pay), 0)").Scan(&totalPre)
	}

	response.DetailResponse(c, gin.H{
		"today_money":     todayMoney,
		"yesterday_money": yesterdayMoney,
		"total_pre":       totalPre,
	}, "")
}

// ==================== 5. 租户流水 (tenant_flow) ====================

// TenantCashFlowList 租户资金流水列表（只读）
// GET /api/business/tenant_flow/
func (h *BusinessHandler) TenantCashFlowList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.TenantCashFlow{}).
		Preload("Tenant").
		Preload("Tenant.SystemUser")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Where("tenant_id = ?", tenant.ID)
			}
		}
	}

	if tenantID := c.Query("tenant"); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if flowType := c.Query("flow_type"); flowType != "" {
		query = query.Where("flow_type = ?", flowType)
	}
	if startDate := c.Query("create_datetime_after"); startDate != "" {
		query = query.Where("create_datetime >= ?", startDate)
	}
	if endDate := c.Query("create_datetime_before"); endDate != "" {
		query = query.Where("create_datetime <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var items []model.TenantCashFlow
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":              item.ID,
			"tenant_id":       item.TenantID,
			"flow_type":       item.FlowType,
			"old_money":       item.OldMoney,
			"new_money":       item.NewMoney,
			"change_money":    item.ChangeMoney,
			"old_money_msg":   formatMoney(item.OldMoney),
			"new_money_msg":   formatMoney(item.NewMoney),
			"change_money_msg": formatChangeMoney(item.ChangeMoney),
			"order_id":        item.OrderID,
			"description":     item.Description,
			"create_datetime": item.CreateDatetime,
		}
		if item.Tenant != nil {
			row["name"] = item.Tenant.SystemUser.Name
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// ==================== 6. 订单设备 (order_device) ====================

// OrderDeviceList 订单设备列表（只读）
// GET /api/business/order_device/
func (h *BusinessHandler) OrderDeviceList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.OrderDeviceDetails{}).
		Preload("Order").
		Preload("Order.Merchant").
		Preload("Order.Merchant.SystemUser").
		Joins("JOIN "+model.Order{}.TableName()+" o ON o.id = "+model.OrderDeviceDetails{}.TableName()+".order_id").
		Where("o.merchant_id IS NOT NULL")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = o.merchant_id").
					Where("m.parent_id = ?", tenant.ID)
			}
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Joins("JOIN "+model.OrderDetail{}.TableName()+" od ON od.order_id = o.id").
					Where("od.writeoff_id = ?", writeoff.ID)
			}
		}
	}

	// 筛选
	if ip := c.Query("ip_address"); ip != "" {
		query = query.Where(model.OrderDeviceDetails{}.TableName()+".ip_address = ?", ip)
	}
	if deviceType := c.Query("device_type"); deviceType != "" {
		query = query.Where(model.OrderDeviceDetails{}.TableName()+".device_type = ?", deviceType)
	}
	if fingerprint := c.Query("device_fingerprint"); fingerprint != "" {
		query = query.Where(model.OrderDeviceDetails{}.TableName()+".device_fingerprint = ?", fingerprint)
	}
	if userID := c.Query("user_id"); userID != "" {
		query = query.Where(model.OrderDeviceDetails{}.TableName()+".user_id = ?", userID)
	}
	if orderID := c.Query("order"); orderID != "" {
		query = query.Where(model.OrderDeviceDetails{}.TableName()+".order_id = ?", orderID)
	}
	if orderNo := c.Query("order_no"); orderNo != "" {
		query = query.Where("o.order_no LIKE ?", "%"+orderNo+"%")
	}
	if startDate := c.Query("create_datetime_after"); startDate != "" {
		query = query.Where(model.OrderDeviceDetails{}.TableName()+".create_datetime >= ?", startDate)
	}
	if endDate := c.Query("create_datetime_before"); endDate != "" {
		query = query.Where(model.OrderDeviceDetails{}.TableName()+".create_datetime <= ?", endDate)
	}

	// 封禁状态过滤
	if ipStatus := c.Query("ip_address_status"); ipStatus != "" {
		if ipStatus == "true" || ipStatus == "1" {
			query = query.Where(model.OrderDeviceDetails{}.TableName()+".ip_address IN (SELECT ip_address FROM "+model.BanIp{}.TableName()+")")
		} else {
			query = query.Where(model.OrderDeviceDetails{}.TableName()+".ip_address NOT IN (SELECT ip_address FROM "+model.BanIp{}.TableName()+")")
		}
	}
	if uidStatus := c.Query("user_id_status"); uidStatus != "" {
		if uidStatus == "true" || uidStatus == "1" {
			query = query.Where(model.OrderDeviceDetails{}.TableName()+".user_id IN (SELECT user_id FROM "+model.BanUserId{}.TableName()+")")
		} else {
			query = query.Where(model.OrderDeviceDetails{}.TableName()+".user_id NOT IN (SELECT user_id FROM "+model.BanUserId{}.TableName()+")")
		}
	}

	var total int64
	query.Count(&total)

	var items []model.OrderDeviceDetails
	query.Order(model.OrderDeviceDetails{}.TableName() + ".id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":                item.ID,
			"ip_address":        item.IPAddress,
			"address":           item.Address,
			"device_type":       item.DeviceType,
			"device_fingerprint": item.DeviceFingerprint,
			"order_id":          item.OrderID,
			"user_id":           item.UserID,
			"create_datetime":   item.CreateDatetime,
		}
		if item.Order != nil {
			row["order_no"] = item.Order.OrderNo
			row["order_status"] = item.Order.OrderStatus
			if item.Order.Merchant != nil {
				row["merchant_name"] = item.Order.Merchant.SystemUser.Name
			}
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// OrderDeviceBanIP 封禁/解封IP
// POST /api/business/order_device/:id/ban/ip/
func (h *BusinessHandler) OrderDeviceBanIP(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var device model.OrderDeviceDetails
	if err := h.DB.Preload("Order").Where("id = ?", id).First(&device).Error; err != nil {
		response.ErrorResponse(c, "设备记录不存在")
		return
	}

	if device.IPAddress == "" {
		response.ErrorResponse(c, "订单号没有对应的IP")
		return
	}

	// 获取租户ID（通过订单的核销的上级租户）
	var tenantID uint
	if device.Order != nil {
		var detail model.OrderDetail
		if err := h.DB.Preload("Writeoff").Where("order_id = ?", device.OrderID).First(&detail).Error; err == nil {
			if detail.Writeoff != nil {
				tenantID = detail.Writeoff.ParentID
			}
		}
	}

	// 切换封禁状态
	var existing model.BanIp
	if err := h.DB.Where("ip_address = ? AND tenant_id = ?", device.IPAddress, tenantID).First(&existing).Error; err == nil {
		h.DB.Delete(&existing)
		response.DetailResponse(c, nil, "IP解封成功")
		return
	}
	h.DB.Create(&model.BanIp{IPAddress: device.IPAddress, TenantID: uintPtr(tenantID)})
	response.DetailResponse(c, nil, "IP封禁成功")
}

// OrderDeviceBanUserID 封禁/解封UserID
// POST /api/business/order_device/:id/ban/id/
func (h *BusinessHandler) OrderDeviceBanUserID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var device model.OrderDeviceDetails
	if err := h.DB.Preload("Order").Where("id = ?", id).First(&device).Error; err != nil {
		response.ErrorResponse(c, "设备记录不存在")
		return
	}

	if device.UserID == "" {
		response.ErrorResponse(c, "订单号没有对应的user_id")
		return
	}

	var tenantID uint
	if device.Order != nil {
		var detail model.OrderDetail
		if err := h.DB.Preload("Writeoff").Where("order_id = ?", device.OrderID).First(&detail).Error; err == nil {
			if detail.Writeoff != nil {
				tenantID = detail.Writeoff.ParentID
			}
		}
	}

	var existing model.BanUserId
	if err := h.DB.Where("user_id = ? AND tenant_id = ?", device.UserID, tenantID).First(&existing).Error; err == nil {
		h.DB.Delete(&existing)
		response.DetailResponse(c, nil, "UserID解封成功")
		return
	}
	h.DB.Create(&model.BanUserId{UserID: device.UserID, TenantID: tenantID})
	response.DetailResponse(c, nil, "UserID封禁成功")
}

// OrderDeviceStatistics 设备统计
// GET /api/business/order_device/statistics/
func (h *BusinessHandler) OrderDeviceStatistics(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.OrderDeviceDetails{}).
		Joins("JOIN "+model.Order{}.TableName()+" o ON o.id = "+model.OrderDeviceDetails{}.TableName()+".order_id").
		Where("o.merchant_id IS NOT NULL")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = o.merchant_id").
					Where("m.parent_id = ?", tenant.ID)
			}
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Joins("JOIN "+model.OrderDetail{}.TableName()+" od ON od.order_id = o.id").
					Where("od.writeoff_id = ?", writeoff.ID)
			}
		}
	}

	var totalIPCount, totalUserIDCount int64
	query.Where("o.order_status IN ?", []int{4, 6}).
		Select("COUNT(DISTINCT ip_address)").Scan(&totalIPCount)

	query2 := h.DB.Model(&model.OrderDeviceDetails{}).
		Joins("JOIN "+model.Order{}.TableName()+" o ON o.id = "+model.OrderDeviceDetails{}.TableName()+".order_id").
		Where("o.merchant_id IS NOT NULL")
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query2 = query2.Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = o.merchant_id").
					Where("m.parent_id = ?", tenant.ID)
			}
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query2 = query2.Joins("JOIN "+model.OrderDetail{}.TableName()+" od ON od.order_id = o.id").
					Where("od.writeoff_id = ?", writeoff.ID)
			}
		}
	}
	query2.Where("o.order_status IN ?", []int{4, 6}).
		Select("COUNT(DISTINCT user_id)").Scan(&totalUserIDCount)

	var banIPCount, banUserIDCount int64
	h.DB.Model(&model.BanIp{}).Count(&banIPCount)
	h.DB.Model(&model.BanUserId{}).Count(&banUserIDCount)

	response.DetailResponse(c, gin.H{
		"total_ip_count":      totalIPCount,
		"total_user_id_count": totalUserIDCount,
		"ban_ip_count":        banIPCount,
		"ban_user_id_count":   banUserIDCount,
	}, "查询成功")
}

// ==================== 7. 补单管理 (reorder) ====================

// ReOrderList 补单列表（只读）
// GET /api/business/reorder/
func (h *BusinessHandler) ReOrderList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.ReOrder{}).
		Preload("Order").
		Preload("Order.Merchant").
		Preload("Order.Merchant.SystemUser")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.Order{}.TableName()+" o ON o.id = "+model.ReOrder{}.TableName()+".order_id").
					Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = o.merchant_id").
					Where("m.parent_id = ?", tenant.ID)
			}
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Joins("JOIN "+model.Order{}.TableName()+" o ON o.id = "+model.ReOrder{}.TableName()+".order_id").
					Joins("JOIN "+model.OrderDetail{}.TableName()+" od ON od.order_id = o.id").
					Where("od.writeoff_id = ?", writeoff.ID)
			}
		}
	}

	// 筛选
	if orderNo := c.Query("order_no"); orderNo != "" {
		query = query.Joins("JOIN "+model.Order{}.TableName()+" o2 ON o2.id = "+model.ReOrder{}.TableName()+".order_id").
			Where("o2.order_no LIKE ?", orderNo+"%")
	}
	if remarks := c.Query("remarks"); remarks != "" {
		query = query.Where(model.ReOrder{}.TableName()+".description LIKE ?", "%"+remarks+"%")
	}
	if dateStr := c.Query("create_datetime"); dateStr != "" {
		query = query.Where(model.ReOrder{}.TableName()+".create_datetime >= ? AND "+model.ReOrder{}.TableName()+".create_datetime < ?",
			dateStr, dateStr+" 23:59:59")
	}

	var total int64
	query.Count(&total)

	var items []model.ReOrder
	query.Order(model.ReOrder{}.TableName() + ".id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":              item.ID,
			"order_id":        item.OrderID,
			"description":     item.Description,
			"creator":         item.Creator,
			"create_datetime": item.CreateDatetime,
		}
		if item.Order != nil {
			row["order_no"] = item.Order.OrderNo
			row["order_status"] = item.Order.OrderStatus
			row["money"] = item.Order.Money
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// ==================== 8. Telegram绑定 (tenant_yufu) ====================

// TenantYufuUserList Telegram绑定列表
// GET /api/business/tenant_yufu/
func (h *BusinessHandler) TenantYufuUserList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.TenantYufuUser{}).
		Preload("Tenant").
		Preload("Tenant.SystemUser")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Where("tenant_id = ?", tenant.ID)
			}
		}
	}

	var total int64
	query.Count(&total)

	var items []model.TenantYufuUser
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":        item.ID,
			"tenant_id": item.TenantID,
			"telegram":  item.Telegram,
		}
		if item.Tenant != nil {
			row["tenant_name"] = item.Tenant.SystemUser.Name
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// TenantYufuUserCreate 创建Telegram绑定链接
// POST /api/business/tenant_yufu/
func (h *BusinessHandler) TenantYufuUserCreate(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser == nil {
		response.ErrorResponse(c, "未获取到用户信息")
		return
	}

	var tenant model.Tenant
	if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err != nil {
		response.ErrorResponse(c, "租户信息不存在")
		return
	}

	// 生成绑定码
	code := fmt.Sprintf("TG%X", time.Now().UnixNano())
	ctx := c.Request.Context()
	h.RDB.Set(ctx, code, tenant.ID, 5*time.Minute)

	response.DetailResponse(c, gin.H{
		"url": fmt.Sprintf("https://t.me/googlepay2018_bot?start=%s", code),
	}, "获取绑定链接成功")
}

// TenantYufuUserDelete 删除Telegram绑定
// DELETE /api/business/tenant_yufu/:id/
func (h *BusinessHandler) TenantYufuUserDelete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	if err := h.DB.Where("id = ?", id).Delete(&model.TenantYufuUser{}).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}

	response.DetailResponse(c, nil, "删除成功")
}

// TenantYufuBotTelegram Bot回调接口（公开）
// POST /api/business/tenant_yufu/bot/telegram/
func (h *BusinessHandler) TenantYufuBotTelegram(c *gin.Context) {
	var req struct {
		Code   string `json:"code"`
		UserID string `json:"userId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	if req.Code == "" {
		response.ErrorResponse(c, "请求无效")
		return
	}

	ctx := c.Request.Context()
	tenantIDStr, err := h.RDB.Get(ctx, req.Code).Result()
	if err != nil {
		response.ErrorResponse(c, "绑定链接已过期或不存在")
		return
	}

	tenantID, _ := strconv.ParseUint(tenantIDStr, 10, 64)

	var tenant model.Tenant
	if err := h.DB.Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		response.ErrorResponse(c, "用户不存在")
		return
	}

	// 检查是否已绑定
	var count int64
	h.DB.Model(&model.TenantYufuUser{}).Where("tenant_id = ? AND telegram = ?", tenantID, req.UserID).Count(&count)
	if count > 0 {
		response.ErrorResponse(c, "用户已绑定")
		return
	}

	h.DB.Create(&model.TenantYufuUser{
		TenantID: uintPtr(uint(tenantID)),
		Telegram: req.UserID,
	})
	h.RDB.Del(ctx, req.Code)
	response.DetailResponse(c, nil, "绑定成功")
}

// ==================== 9. 归集统计 (collection_statistics) ====================

// CollectionUserDayStatisticsList 归集用户日统计列表
// GET /api/business/collection/statistics/
func (h *BusinessHandler) CollectionUserDayStatisticsList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.CollectionUser{})

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Where("tenant_id = ?", tenant.ID)
			}
		}
	}

	// 筛选
	if username := c.Query("username"); username != "" {
		query = query.Where("username = ?", username)
	}
	if name := c.Query("name"); name != "" {
		query = query.Where("name = ?", name)
	}

	var total int64
	query.Count(&total)

	var users []model.CollectionUser
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&users)

	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	dateFilter := map[string]interface{}{}
	if dateBefore := c.Query("date_before"); dateBefore != "" {
		dateFilter["date <="] = dateBefore
	}
	if dateAfter := c.Query("date_after"); dateAfter != "" {
		dateFilter["date >="] = dateAfter
	}

	var result []gin.H
	for _, user := range users {
		row := gin.H{
			"id":       user.ID,
			"username": user.Username,
			"name":     user.Name,
			"remarks":  user.Remarks,
		}

		// 计算今日/昨日/总流水
		var todayFlow, yesterdayFlow, totalFlow int64
		h.DB.Model(&model.CollectionDayFlow{}).Where("user_id = ? AND date = ?", user.ID, today).
			Select("COALESCE(SUM(flow), 0)").Scan(&todayFlow)
		h.DB.Model(&model.CollectionDayFlow{}).Where("user_id = ? AND date = ?", user.ID, yesterday).
			Select("COALESCE(SUM(flow), 0)").Scan(&yesterdayFlow)

		totalFlowQuery := h.DB.Model(&model.CollectionDayFlow{}).Where("user_id = ?", user.ID)
		for k, v := range dateFilter {
			totalFlowQuery = totalFlowQuery.Where(k+" ?", v)
		}
		totalFlowQuery.Select("COALESCE(SUM(flow), 0)").Scan(&totalFlow)

		row["today_flow"] = todayFlow
		row["yesterday_flow"] = yesterdayFlow
		row["total_flow"] = totalFlow

		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// CollectionUserDayStatisticsStatistics 归集统计汇总
// GET /api/business/collection/statistics/statistics/
func (h *BusinessHandler) CollectionUserDayStatisticsStatistics(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	baseQuery := func() *gorm.DB {
		q := h.DB.Model(&model.CollectionDayFlow{})
		if currentUser != nil {
			switch currentUser.Role.Key {
			case model.RoleKeyTenant:
				var tenant model.Tenant
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
					q = q.Joins("JOIN "+model.CollectionUser{}.TableName()+" cu ON cu.id = "+model.CollectionDayFlow{}.TableName()+".user_id").
						Where("cu.tenant_id = ?", tenant.ID)
				}
			case model.RoleKeyWriteoff:
				var writeoff model.WriteOff
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
					q = q.Joins("JOIN "+model.CollectionUser{}.TableName()+" cu ON cu.id = "+model.CollectionDayFlow{}.TableName()+".user_id").
						Where("cu.writeoff_id = ?", writeoff.ID)
				}
			}
		}
		return q
	}

	var todayMoney, yesterdayMoney, totalMoney int64
	baseQuery().Where("date = ?", today).Select("COALESCE(SUM(flow), 0)").Scan(&todayMoney)
	baseQuery().Where("date = ?", yesterday).Select("COALESCE(SUM(flow), 0)").Scan(&yesterdayMoney)
	baseQuery().Select("COALESCE(SUM(flow), 0)").Scan(&totalMoney)

	response.DetailResponse(c, gin.H{
		"today_money":     todayMoney,
		"yesterday_money": yesterdayMoney,
		"total_money":     totalMoney,
	}, "")
}

// ==================== 10. 租户小号管理 (tenant_account) ====================

// TenantCookieFileList 租户小号文件列表
// GET /api/business/tenant_account/file/
func (h *BusinessHandler) TenantCookieFileList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.TenantCookieFile{}).
		Preload("Tenant").
		Preload("Tenant.SystemUser").
		Preload("Plugin")

	// RBAC 过滤
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			query = query.Where("tenant_id = ?", tenant.ID)
		}
	}

	if pluginID := c.Query("plugin"); pluginID != "" {
		query = query.Where("plugin_id = ?", pluginID)
	}
	if tenantID := c.Query("tenant"); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if tenantName := c.Query("tenant_name"); tenantName != "" {
		query = query.Joins("JOIN "+model.Tenant{}.TableName()+" t ON t.id = "+model.TenantCookieFile{}.TableName()+".tenant_id").
			Joins("JOIN "+model.Users{}.TableName()+" u ON u.id = t.system_user_id").
			Where("u.name LIKE ?", "%"+tenantName+"%")
	}

	var total int64
	query.Count(&total)

	var items []model.TenantCookieFile
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":              item.ID,
			"tenant_id":       item.TenantID,
			"plugin_id":       item.PluginID,
			"filename":        item.Filename,
			"status":          item.Status,
			"create_datetime": item.CreateDatetime,
		}
		if item.Tenant != nil {
			row["tenant_name"] = item.Tenant.SystemUser.Name
		}
		if item.Plugin != nil {
			row["plugin_name"] = item.Plugin.Name
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// TenantCookieFileCreate 上传小号文件
// POST /api/business/tenant_account/file/
func (h *BusinessHandler) TenantCookieFileCreate(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser == nil {
		response.ErrorResponse(c, "未获取到用户信息")
		return
	}

	pluginIDStr := c.Query("plugin")
	tenantIDStr := c.Query("tenant")
	if pluginIDStr == "" {
		response.ErrorResponse(c, "缺少plugin参数")
		return
	}

	pluginID, _ := strconv.ParseUint(pluginIDStr, 10, 64)
	var tenantID uint

	if currentUser.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			tenantID = tenant.ID
		}
	} else {
		if tenantIDStr == "" {
			response.ErrorResponse(c, "缺少tenant参数")
			return
		}
		tid, _ := strconv.ParseUint(tenantIDStr, 10, 64)
		tenantID = uint(tid)
	}

	if tenantID == 0 {
		response.ErrorResponse(c, "租户不存在")
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		response.ErrorResponse(c, "请上传文件")
		return
	}

	f, err := file.Open()
	if err != nil {
		response.ErrorResponse(c, "文件打开失败")
		return
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		response.ErrorResponse(c, "文件读取失败")
		return
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// 创建文件记录
	cookieFile := model.TenantCookieFile{
		TenantID: &tenantID,
		PluginID: uintPtr(uint(pluginID)),
		Filename: file.Filename,
		Status:   true,
		Creator:  &currentUser.ID,
	}
	if err := h.DB.Create(&cookieFile).Error; err != nil {
		response.ErrorResponse(c, "文件记录创建失败")
		return
	}

	// 逐行解析并创建cookie记录
	successCount := 0
	totalCount := 0
	for _, line := range lines {
		totalCount++
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}

		// 尝试解析为JSON
		var contentMap map[string]interface{}
		if err := json.Unmarshal([]byte(line), &contentMap); err != nil {
			// 非JSON，作为单字段
			contentMap = map[string]interface{}{"cookies": line}
		}

		contentJSON, _ := json.Marshal(contentMap)

		cookie := model.TenantCookie{
			TenantID: &tenantID,
			PluginID: uintPtr(uint(pluginID)),
			FileID:   &cookieFile.ID,
			Content:  string(contentJSON),
			Status:   true,
			Creator:  &currentUser.ID,
		}
		if err := h.DB.Create(&cookie).Error; err == nil {
			successCount++
		}
	}

	response.DetailResponse(c, gin.H{
		"total":   totalCount,
		"success": successCount,
	}, "添加成功")
}

// TenantCookieFileUpdate 更新小号文件状态
// PUT /api/business/tenant_account/file/:id/
func (h *BusinessHandler) TenantCookieFileUpdate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var req struct {
		Status *bool `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	updates := map[string]interface{}{}
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	if len(updates) > 0 {
		h.DB.Model(&model.TenantCookieFile{}).Where("id = ?", id).Updates(updates)
	}
	response.DetailResponse(c, nil, "更新成功")
}

// TenantCookieFileDelete 删除小号文件
// DELETE /api/business/tenant_account/file/:id/
func (h *BusinessHandler) TenantCookieFileDelete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}
	h.DB.Where("id = ?", id).Delete(&model.TenantCookieFile{})
	response.DetailResponse(c, nil, "删除成功")
}

// TenantCookieFileExport 导出小号
// GET /api/business/tenant_account/file/:id/export/
func (h *BusinessHandler) TenantCookieFileExport(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var cookieFile model.TenantCookieFile
	if err := h.DB.Where("id = ?", id).First(&cookieFile).Error; err != nil {
		response.ErrorResponse(c, "文件不存在")
		return
	}

	exportType := c.DefaultQuery("export_type", "0")
	cookieStatus := c.DefaultQuery("cookie_status", "3")

	query := h.DB.Model(&model.TenantCookie{}).Where("file_id = ?", id)

	switch cookieStatus {
	case "1":
		query = query.Where("status = ?", true)
	case "2":
		query = query.Where("status = ?", false)
	case "3":
		// 提交数大于0但成功数为0的cookie
		query = query.Where("id IN (SELECT cookie_id FROM "+model.TenantCookieDayStatistics{}.TableName()+
			" WHERE submit_count > 0 AND success_count = 0)")
	}

	var contents []string
	query.Pluck("content", &contents)

	var lines []string
	for _, content := range contents {
		if exportType == "0" {
			// 提取第一个值
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(content), &m); err == nil {
				for _, v := range m {
					lines = append(lines, fmt.Sprintf("%v", v))
					break
				}
			}
		} else {
			lines = append(lines, content)
		}
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", cookieFile.Filename))
	c.Header("Content-Type", "application/octet-stream")
	c.String(200, strings.Join(lines, "\n"))
}

// TenantCookieList 小号列表
// GET /api/business/tenant_account/cookie/
func (h *BusinessHandler) TenantCookieList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.TenantCookie{})

	// RBAC 过滤
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			query = query.Where("tenant_id = ?", tenant.ID)
		}
	}

	if pluginID := c.Query("plugin"); pluginID != "" {
		query = query.Where("plugin_id = ?", pluginID)
	}
	if tenantID := c.Query("tenant"); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if fileID := c.Query("file_id"); fileID != "" {
		query = query.Where("file_id = ?", fileID)
	}
	if tenantName := c.Query("tenant_name"); tenantName != "" {
		query = query.Joins("JOIN "+model.Tenant{}.TableName()+" t ON t.id = "+model.TenantCookie{}.TableName()+".tenant_id").
			Joins("JOIN "+model.Users{}.TableName()+" u ON u.id = t.system_user_id").
			Where("u.name LIKE ?", "%"+tenantName+"%")
	}

	var total int64
	query.Count(&total)

	var items []model.TenantCookie
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":              item.ID,
			"tenant_id":       item.TenantID,
			"plugin_id":       item.PluginID,
			"file_id":         item.FileID,
			"content":         item.Content,
			"status":          item.Status,
			"real_name":       item.RealName,
			"address":         item.Address,
			"remarks":         item.Remarks,
			"description":     item.Description,
			"create_datetime": item.CreateDatetime,
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// TenantCookieUpdate 更新小号
// PUT /api/business/tenant_account/cookie/:id/
func (h *BusinessHandler) TenantCookieUpdate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var req struct {
		Status  *bool  `json:"status"`
		Remarks string `json:"remarks"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	updates := map[string]interface{}{}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Remarks != "" {
		updates["remarks"] = req.Remarks
	}

	if len(updates) > 0 {
		h.DB.Model(&model.TenantCookie{}).Where("id = ?", id).Updates(updates)
	}
	response.DetailResponse(c, nil, "更新成功")
}

// TenantCookieDelete 删除小号
// DELETE /api/business/tenant_account/cookie/:id/
func (h *BusinessHandler) TenantCookieDelete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}
	h.DB.Where("id = ?", id).Delete(&model.TenantCookie{})
	response.DetailResponse(c, nil, "删除成功")
}

// TenantCookieArgs 获取插件Cookie参数
// GET /api/business/tenant_account/cookie/args/
func (h *BusinessHandler) TenantCookieArgs(c *gin.Context) {
	pluginIDStr := c.Query("plugin")
	if pluginIDStr == "" {
		response.ErrorResponse(c, "缺少plugin参数")
		return
	}

	pluginID, _ := strconv.ParseUint(pluginIDStr, 10, 64)

	// 获取插件配置中的cookie_select
	var configs []model.PayPluginConfig
	h.DB.Where("plugin_id = ? AND `key` = ?", pluginID, "cookie_select").Find(&configs)

	args := []string{}
	if len(configs) > 0 {
		var m map[string]interface{}
		if configs[0].Value != nil {
			if err := json.Unmarshal([]byte(*configs[0].Value), &m); err == nil {
				for k := range m {
					args = append(args, k)
				}
			}
		}
	}

	response.DetailResponse(c, gin.H{"args": args}, "查询成功")
}

// TenantCookieCount 小号统计
// GET /api/business/tenant_account/cookie/count/
func (h *BusinessHandler) TenantCookieCount(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.TenantCookie{})

	if currentUser != nil && currentUser.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			query = query.Where("tenant_id = ?", tenant.ID)
		}
	}

	if pluginID := c.Query("plugin"); pluginID != "" {
		query = query.Where("plugin_id = ?", pluginID)
	}
	if tenantID := c.Query("tenant"); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if fileID := c.Query("file_id"); fileID != "" {
		query = query.Where("file_id = ?", fileID)
	}

	var total int64
	query.Count(&total)

	// residue query must apply the same RBAC + parameter filters as the total query
	residueQuery := h.DB.Model(&model.TenantCookie{}).Where("status = ?", true)
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			residueQuery = residueQuery.Where("tenant_id = ?", tenant.ID)
		}
	}
	if pluginID := c.Query("plugin"); pluginID != "" {
		residueQuery = residueQuery.Where("plugin_id = ?", pluginID)
	}
	if tenantID := c.Query("tenant"); tenantID != "" {
		residueQuery = residueQuery.Where("tenant_id = ?", tenantID)
	}
	if fileID := c.Query("file_id"); fileID != "" {
		residueQuery = residueQuery.Where("file_id = ?", fileID)
	}
	var residue int64
	residueQuery.Count(&residue)

	response.DetailResponse(c, gin.H{
		"total":   total,
		"residue": residue,
	}, "查询成功")
}

// ==================== 11. 话单管理 (phone_order) ====================

// PhoneOrderList 话单列表（只读）
// GET /api/business/phone_order/
func (h *BusinessHandler) PhoneOrderList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.PhoneProduct{}).
		Preload("Writeoff").
		Preload("Writeoff.SystemUser").
		Preload("Order")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Where("writeoff_id = ?", writeoff.ID)
			}
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+model.PhoneProduct{}.TableName()+".writeoff_id").
					Where("w.parent_id = ?", tenant.ID)
			}
		}
	}

	// 筛选
	if orderStatus := c.Query("order_status"); orderStatus != "" {
		query = query.Where(model.PhoneProduct{}.TableName()+".order_status = ?", orderStatus)
	}
	if chargeType := c.Query("charge_type"); chargeType != "" {
		query = query.Where(model.PhoneProduct{}.TableName()+".charge_type = ?", chargeType)
	}
	if company := c.Query("company"); company != "" {
		query = query.Where(model.PhoneProduct{}.TableName()+".company = ?", company)
	}
	if phone := c.Query("phone"); phone != "" {
		query = query.Where(model.PhoneProduct{}.TableName()+".phone = ?", phone)
	}
	if province := c.Query("province"); province != "" {
		query = query.Where(model.PhoneProduct{}.TableName()+".province = ?", province)
	}
	if id := c.Query("id"); id != "" {
		query = query.Where(model.PhoneProduct{}.TableName()+".id = ?", id)
	}
	if money := c.Query("money"); money != "" {
		if m, err := strconv.Atoi(money); err == nil {
			query = query.Where(model.PhoneProduct{}.TableName()+".money = ?", m*100)
		}
	}
	if orderNo := c.Query("order_no"); orderNo != "" {
		query = query.Where(model.PhoneProduct{}.TableName()+".order_id IN (SELECT id FROM "+model.Order{}.TableName()+" WHERE order_no = ?)", orderNo)
	}
	if startDate := c.Query("create_datetime_after"); startDate != "" {
		query = query.Where(model.PhoneProduct{}.TableName()+".create_datetime >= ?", startDate)
	}
	if endDate := c.Query("create_datetime_before"); endDate != "" {
		query = query.Where(model.PhoneProduct{}.TableName()+".create_datetime <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var items []model.PhoneProduct
	query.Order(model.PhoneProduct{}.TableName() + ".create_datetime DESC").Offset(offset).Limit(limit).Find(&items)

	var result []gin.H
	for _, item := range items {
		row := gin.H{
			"id":               item.ID,
			"phone":            item.Phone,
			"province":         item.Province,
			"city":             item.City,
			"company":          item.Company,
			"phone_order_no":   item.PhoneOrderNo,
			"money":            item.Money,
			"charge_type":      item.ChargeType,
			"order_status":     item.OrderStatus,
			"order_id":         item.OrderID,
			"notify_url":       item.NotifyURL,
			"finish_datetime":  item.FinishDatetime,
			"create_datetime":  item.CreateDatetime,
			"writeoff_id":      item.WriteoffID,
		}
		if item.Writeoff != nil {
			row["writeoff_name"] = item.Writeoff.SystemUser.Name
		}
		if item.Order != nil {
			row["order_no"] = item.Order.OrderNo
		}
		result = append(result, row)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// PhoneOrderDelete 删除话单（标记为已取消）
// DELETE /api/business/phone_order/:id/
func (h *BusinessHandler) PhoneOrderDelete(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var product model.PhoneProduct
	if err := h.DB.Where("id = ?", id).First(&product).Error; err != nil {
		response.ErrorResponse(c, "话单不存在")
		return
	}

	// 更新状态为9（已取消）
	h.DB.Model(&model.PhoneProduct{}).Where("id = ?", id).Update("order_status", 9)
	response.DetailResponse(c, nil, "删除成功")
}

// PhoneOrderStatisticsMoney 话单流水统计
// GET /api/business/phone_order/statistics/money/
func (h *BusinessHandler) PhoneOrderStatisticsMoney(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	baseQuery := func() *gorm.DB {
		q := h.DB.Model(&model.PhoneOrderFlow{})
		if currentUser != nil {
			switch currentUser.Role.Key {
			case model.RoleKeyWriteoff:
				var writeoff model.WriteOff
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
					q = q.Where("writeoff_id = ?", writeoff.ID)
				}
			case model.RoleKeyTenant:
				var tenant model.Tenant
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
					q = q.Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+model.PhoneOrderFlow{}.TableName()+".writeoff_id").
						Where("w.parent_id = ?", tenant.ID)
				}
			}
		}
		return q
	}

	var todayMoney, yesterdayMoney, totalMoney, totalRefund, totalQuick, totalSlow int64
	baseQuery().Where("date = ?", today).Select("COALESCE(SUM(flow), 0)").Scan(&todayMoney)
	baseQuery().Where("date = ?", yesterday).Select("COALESCE(SUM(flow), 0)").Scan(&yesterdayMoney)
	baseQuery().Select("COALESCE(SUM(flow), 0)").Scan(&totalMoney)
	baseQuery().Select("COALESCE(SUM(refund), 0)").Scan(&totalRefund)
	baseQuery().Where("charge_type = 0").Select("COALESCE(SUM(flow), 0)").Scan(&totalQuick)
	baseQuery().Where("charge_type = 1").Select("COALESCE(SUM(flow), 0)").Scan(&totalSlow)

	response.DetailResponse(c, gin.H{
		"today_money":      todayMoney,
		"yesterday_money":  yesterdayMoney,
		"total_money":      totalMoney,
		"total_refund":     totalRefund,
		"total_quick_money": totalQuick,
		"total_slow_money":  totalSlow,
	}, "查询成功")
}

// PhoneOrderProduct 话单库存统计
// GET /api/business/phone_order/product/
func (h *BusinessHandler) PhoneOrderProduct(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.PhoneProduct{}).Where("order_status = 0")

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Where("writeoff_id = ?", writeoff.ID)
			}
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+model.PhoneProduct{}.TableName()+".writeoff_id").
					Where("w.parent_id = ?", tenant.ID)
			}
		}
	}

	moneys := []int{3000, 5000, 10000, 20000, 30000, 50000}
	labels := []string{"30", "50", "100", "200", "300", "500"}
	data := gin.H{}

	for i, money := range moneys {
		var quickCount, slowCount int64
		query.Session(&gorm.Session{}).Where("charge_type = 0 AND money = ?", money).Count(&quickCount)
		query.Session(&gorm.Session{}).Where("charge_type = 1 AND money = ?", money).Count(&slowCount)
		data[labels[i]] = gin.H{
			"quick": quickCount,
			"slow":  slowCount,
		}
	}

	response.DetailResponse(c, data, "查询成功")
}

// PhoneOrderStatistics 话单统计（按金额和状态分组）
// GET /api/business/phone_order/statistics/
func (h *BusinessHandler) PhoneOrderStatistics(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Model(&model.PhoneProduct{})

	// RBAC 过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Where("writeoff_id = ?", writeoff.ID)
			}
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				query = query.Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+model.PhoneProduct{}.TableName()+".writeoff_id").
					Where("w.parent_id = ?", tenant.ID)
			}
		}
	}

	if chargeType := c.Query("charge_type"); chargeType != "" {
		query = query.Where("charge_type = ?", chargeType)
	}
	if startDate := c.Query("create_datetime_after"); startDate != "" {
		query = query.Where("create_datetime >= ?", startDate)
	}
	if endDate := c.Query("create_datetime_before"); endDate != "" {
		query = query.Where("create_datetime <= ?", endDate)
	}

	moneys := []int{1000, 2000, 3000, 5000, 10000, 20000, 30000, 50000}
	labels := []string{"10", "20", "30", "50", "100", "200", "300", "500"}
	data := gin.H{}

	for i, money := range moneys {
		var waitingCount, refundCount, successCount, normalCount int64
		query.Session(&gorm.Session{}).Where("order_status = 2 AND money = ?", money).Count(&waitingCount)
		query.Session(&gorm.Session{}).Where("order_status = 3 AND money = ?", money).Count(&refundCount)
		query.Session(&gorm.Session{}).Where("order_status IN ? AND money = ?", []int{4, 5}, money).Count(&successCount)
		query.Session(&gorm.Session{}).Where("order_status NOT IN ? AND money = ?", []int{2, 3, 4, 5}, money).Count(&normalCount)
		data[labels[i]] = gin.H{
			"waiting": waitingCount,
			"refund":  refundCount,
			"success": successCount,
			"normal":  normalCount,
		}
	}

	data["filter"] = gin.H{
		"create_datetime_before": c.Query("create_datetime_before"),
		"create_datetime_after":  c.Query("create_datetime_after"),
		"charge_type":            c.Query("charge_type"),
	}

	response.DetailResponse(c, data, "查询成功")
}

// ==================== 12. 域名鉴权 (auth_responder) ====================

// AuthResponderCheckAuth 域名鉴权
// GET /api/auth_responder/:domain/
func (h *BusinessHandler) AuthResponderCheckAuth(c *gin.Context) {
	domain := c.Param("domain")
	if domain == "" {
		c.Status(403)
		return
	}

	path := c.GetHeader("URI")
	if path == "" {
		c.Status(403)
		return
	}

	// 忽略静态资源
	ignoreExts := []string{".jpg", ".png", ".js", ".svg", ".ico", ".css"}
	for _, ext := range ignoreExts {
		if strings.HasSuffix(path, ext) {
			c.Status(200)
			return
		}
	}

	scheme := c.GetHeader("Scheme")
	ctx := c.Request.Context()

	authStatusKey := fmt.Sprintf("domain.%s://%s.auth_status", scheme, domain)
	authTimeoutKey := fmt.Sprintf("domain.%s://%s.auth_timeout", scheme, domain)
	authKeyKey := fmt.Sprintf("domain.%s://%s.auth_key", scheme, domain)

	authStatus, err := h.RDB.Get(ctx, authStatusKey).Result()
	if err != nil || authStatus == "" {
		c.Status(200)
		return
	}

	authTimeoutStr, _ := h.RDB.Get(ctx, authTimeoutKey).Result()
	domainAuthKey, _ := h.RDB.Get(ctx, authKeyKey).Result()

	authKey := c.Query("auth_key")
	if authKey == "" {
		c.Status(403)
		return
	}

	parts := strings.SplitN(authKey, "-", 4)
	if len(parts) < 4 {
		c.Status(403)
		return
	}

	raw := fmt.Sprintf("%s-%s-%s-%s-%s", path, parts[0], parts[1], parts[2], domainAuthKey)
	hash := fmt.Sprintf("%x", md5.Sum([]byte(raw)))

	if hash != parts[3] {
		c.Status(403)
		return
	}

	authTimeout, err := strconv.ParseInt(authTimeoutStr, 10, 64)
	if err != nil {
		c.Status(403)
		return
	}
	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		c.Status(403)
		return
	}
	if ts+authTimeout < time.Now().Unix() {
		c.Status(403)
		return
	}

	c.Status(200)
}

// ==================== 日统计 CRUD ====================

// DayStatisticsGenericList 通用日统计列表
func (h *BusinessHandler) dayStatisticsGenericList(c *gin.Context, modelObj interface{}, tableName string, filterField string) {
	page, limit, offset := response.GetPagination(c)
	currentUser, _ := middleware.GetCurrentUser(c)

	query := h.DB.Table(tableName)

	// RBAC过滤
	if currentUser != nil && filterField != "" {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			if filterField == "tenant_id" {
				var tenant model.Tenant
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
					query = query.Where("tenant_id = ?", tenant.ID)
				}
			} else if filterField == "writeoff_id" {
				var tenant model.Tenant
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
					query = query.Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+tableName+".writeoff_id").
						Where("w.parent_id = ?", tenant.ID)
				}
			} else if filterField == "merchant_id" {
				var tenant model.Tenant
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
					query = query.Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = "+tableName+".merchant_id").
						Where("m.parent_id = ?", tenant.ID)
				}
			}
		case model.RoleKeyWriteoff:
			if filterField == "writeoff_id" {
				var writeoff model.WriteOff
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
					query = query.Where("writeoff_id = ?", writeoff.ID)
				}
			}
		case model.RoleKeyMerchant:
			if filterField == "merchant_id" {
				var merchant model.Merchant
				if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&merchant).Error; err == nil {
					query = query.Where("merchant_id = ?", merchant.ID)
				}
			}
		}
	}

	// 通用筛选
	if dateStr := c.Query("date"); dateStr != "" {
		query = query.Where("date = ?", dateStr)
	}
	if startDate := c.Query("date_after"); startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate := c.Query("date_before"); endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var results []map[string]interface{}
	query.Order("date DESC").Offset(offset).Limit(limit).Find(&results)

	response.PageResponse(c, results, total, page, limit, "")
}

// MerchantDayStatisticsList 商户日统计列表
// GET /api/business/day_statistics/merchant/
func (h *BusinessHandler) MerchantDayStatisticsList(c *gin.Context) {
	h.dayStatisticsGenericList(c, &model.MerchantDayStatistics{}, model.MerchantDayStatistics{}.TableName(), "merchant_id")
}

// WriteoffDayStatisticsList 核销日统计列表
// GET /api/business/day_statistics/writeoff/
func (h *BusinessHandler) WriteoffDayStatisticsList(c *gin.Context) {
	h.dayStatisticsGenericList(c, &model.WriteOffDayStatistics{}, model.WriteOffDayStatistics{}.TableName(), "writeoff_id")
}

// TenantDayStatisticsList 租户日统计列表
// GET /api/business/day_statistics/tenant/
func (h *BusinessHandler) TenantDayStatisticsList(c *gin.Context) {
	h.dayStatisticsGenericList(c, &model.TenantDayStatistics{}, model.TenantDayStatistics{}.TableName(), "tenant_id")
}

// PayChannelDayStatisticsList 支付通道日统计列表
// GET /api/business/day_statistics/pay_channel/
func (h *BusinessHandler) PayChannelDayStatisticsList(c *gin.Context) {
	h.dayStatisticsGenericList(c, &model.PayChannelDayStatistics{}, model.PayChannelDayStatistics{}.TableName(), "")
}

// AllDayStatisticsList 全站日统计列表
// GET /api/business/day_statistics/all/
func (h *BusinessHandler) AllDayStatisticsList(c *gin.Context) {
	h.dayStatisticsGenericList(c, &model.DayStatistics{}, model.DayStatistics{}.TableName(), "")
}

// Note: formatMoney is defined in data_analysis.go

func formatChangeMoney(amount int64) string {
	s := formatMoney(amount)
	if amount >= 0 && !strings.HasPrefix(s, "-") {
		s = "+" + s
	}
	return s
}

func uintPtr(v uint) *uint {
	return &v
}
