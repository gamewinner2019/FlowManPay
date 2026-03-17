package handler

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
)

// DataAnalysisHandler 数据分析处理器
type DataAnalysisHandler struct {
	DB *gorm.DB
}

// NewDataAnalysisHandler 创建数据分析处理器
func NewDataAnalysisHandler(db *gorm.DB) *DataAnalysisHandler {
	return &DataAnalysisHandler{DB: db}
}

// Dashboard 仪表盘统计
// GET /api/statistics/dashboard/
func (h *DataAnalysisHandler) Dashboard(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	roleKey := user.Role.Key

	result := make(map[string]interface{})

	switch roleKey {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
		h.dashboardAdmin(c, result, todayStart, yesterdayStart)
	case model.RoleKeyTenant:
		h.dashboardTenant(c, result, todayStart, yesterdayStart, user)
	case model.RoleKeyMerchant:
		h.dashboardMerchant(c, result, todayStart, yesterdayStart, user)
	case model.RoleKeyWriteoff:
		h.dashboardWriteoff(c, result, todayStart, yesterdayStart, user)
	default:
		response.DetailResponse(c, result, "")
		return
	}

	response.DetailResponse(c, result, "")
}

// dashboardAdmin 管理员仪表盘
func (h *DataAnalysisHandler) dashboardAdmin(c *gin.Context, result map[string]interface{}, todayStart, yesterdayStart time.Time) {
	// 今日统计
	var todayStat model.DayStatistics
	h.DB.Where("date = ?", todayStart).First(&todayStat)

	// 昨日统计
	var yesterdayStat model.DayStatistics
	h.DB.Where("date = ?", yesterdayStart).First(&yesterdayStat)

	// 累计成功
	var totalSuccess struct {
		TotalMoney int64 `gorm:"column:total_money"`
		TotalCount int   `gorm:"column:total_count"`
	}
	h.DB.Model(&model.DayStatistics{}).
		Select("COALESCE(SUM(success_money), 0) as total_money, COALESCE(SUM(success_count), 0) as total_count").
		Scan(&totalSuccess)

	result["today_success_money"] = todayStat.SuccessMoney
	result["today_success_count"] = todayStat.SuccessCount
	result["today_submit_count"] = todayStat.SubmitCount
	result["today_tax"] = todayStat.TotalTax
	result["yesterday_success_money"] = yesterdayStat.SuccessMoney
	result["yesterday_success_count"] = yesterdayStat.SuccessCount
	result["total_success_money"] = totalSuccess.TotalMoney
	result["total_success_count"] = totalSuccess.TotalCount

	// 设备类型分布
	result["device_distribution"] = map[string]interface{}{
		"android": todayStat.AndroidCount,
		"ios":     todayStat.IOSCount,
		"pc":      todayStat.PCCount,
		"unknown": todayStat.UnknownCount,
	}

	// 租户余额
	var tenantBalance struct {
		Total int64 `gorm:"column:total"`
	}
	h.DB.Model(&model.Tenant{}).Select("COALESCE(SUM(balance), 0) as total").Scan(&tenantBalance)
	result["tenant_balance"] = tenantBalance.Total

	// 15天订单趋势
	result["order_trend"] = h.getOrderTrend(nil, nil, nil, 15)

	// 租户数/商户数/核销数
	var tenantCount, merchantCount, writeoffCount int64
	h.DB.Model(&model.Tenant{}).Count(&tenantCount)
	h.DB.Model(&model.Merchant{}).Count(&merchantCount)
	h.DB.Model(&model.WriteOff{}).Count(&writeoffCount)
	result["tenant_count"] = tenantCount
	result["merchant_count"] = merchantCount
	result["writeoff_count"] = writeoffCount
}

// dashboardTenant 租户仪表盘
func (h *DataAnalysisHandler) dashboardTenant(c *gin.Context, result map[string]interface{}, todayStart, yesterdayStart time.Time, user *model.Users) {
	var tenant model.Tenant
	if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
		return
	}

	var todayStat model.TenantDayStatistics
	h.DB.Where("date = ? AND tenant_id = ?", todayStart, tenant.ID).First(&todayStat)

	var yesterdayStat model.TenantDayStatistics
	h.DB.Where("date = ? AND tenant_id = ?", yesterdayStart, tenant.ID).First(&yesterdayStat)

	var totalSuccess struct {
		TotalMoney int64 `gorm:"column:total_money"`
		TotalCount int   `gorm:"column:total_count"`
	}
	h.DB.Model(&model.TenantDayStatistics{}).
		Where("tenant_id = ?", tenant.ID).
		Select("COALESCE(SUM(success_money), 0) as total_money, COALESCE(SUM(success_count), 0) as total_count").
		Scan(&totalSuccess)

	result["today_success_money"] = todayStat.SuccessMoney
	result["today_success_count"] = todayStat.SuccessCount
	result["today_submit_count"] = todayStat.SubmitCount
	result["today_tax"] = todayStat.TotalTax
	result["yesterday_success_money"] = yesterdayStat.SuccessMoney
	result["yesterday_success_count"] = yesterdayStat.SuccessCount
	result["total_success_money"] = totalSuccess.TotalMoney
	result["total_success_count"] = totalSuccess.TotalCount
	result["balance"] = tenant.Balance

	result["device_distribution"] = map[string]interface{}{
		"android": todayStat.AndroidCount,
		"ios":     todayStat.IOSCount,
		"pc":      todayStat.PCCount,
		"unknown": todayStat.UnknownCount,
	}

	tenantID := tenant.ID
	result["order_trend"] = h.getOrderTrend(&tenantID, nil, nil, 15)

	var merchantCount int64
	h.DB.Model(&model.Merchant{}).Where("parent_id = ?", tenant.ID).Count(&merchantCount)
	result["merchant_count"] = merchantCount
}

// dashboardMerchant 商户仪表盘
func (h *DataAnalysisHandler) dashboardMerchant(c *gin.Context, result map[string]interface{}, todayStart, yesterdayStart time.Time, user *model.Users) {
	var merchant model.Merchant
	if err := h.DB.Where("system_user_id = ?", user.ID).First(&merchant).Error; err != nil {
		return
	}

	var todayStat model.MerchantDayStatistics
	h.DB.Where("date = ? AND merchant_id = ?", todayStart, merchant.ID).First(&todayStat)

	var yesterdayStat model.MerchantDayStatistics
	h.DB.Where("date = ? AND merchant_id = ?", yesterdayStart, merchant.ID).First(&yesterdayStat)

	var totalSuccess struct {
		TotalMoney int64 `gorm:"column:total_money"`
		TotalCount int   `gorm:"column:total_count"`
	}
	h.DB.Model(&model.MerchantDayStatistics{}).
		Where("merchant_id = ?", merchant.ID).
		Select("COALESCE(SUM(success_money), 0) as total_money, COALESCE(SUM(success_count), 0) as total_count").
		Scan(&totalSuccess)

	result["today_success_money"] = todayStat.SuccessMoney
	result["today_success_count"] = todayStat.SuccessCount
	result["today_submit_count"] = todayStat.SubmitCount
	result["today_real_money"] = todayStat.RealMoney
	result["yesterday_success_money"] = yesterdayStat.SuccessMoney
	result["yesterday_success_count"] = yesterdayStat.SuccessCount
	result["total_success_money"] = totalSuccess.TotalMoney
	result["total_success_count"] = totalSuccess.TotalCount

	result["device_distribution"] = map[string]interface{}{
		"android": todayStat.AndroidCount,
		"ios":     todayStat.IOSCount,
		"pc":      todayStat.PCCount,
		"unknown": todayStat.UnknownCount,
	}

	merchantID := merchant.ID
	result["order_trend"] = h.getOrderTrend(nil, &merchantID, nil, 15)
}

// dashboardWriteoff 核销仪表盘
func (h *DataAnalysisHandler) dashboardWriteoff(c *gin.Context, result map[string]interface{}, todayStart, yesterdayStart time.Time, user *model.Users) {
	var writeoff model.WriteOff
	if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
		return
	}

	var todayStat model.WriteOffDayStatistics
	h.DB.Where("date = ? AND writeoff_id = ?", todayStart, writeoff.ID).First(&todayStat)

	var yesterdayStat model.WriteOffDayStatistics
	h.DB.Where("date = ? AND writeoff_id = ?", yesterdayStart, writeoff.ID).First(&yesterdayStat)

	var totalSuccess struct {
		TotalMoney int64 `gorm:"column:total_money"`
		TotalCount int   `gorm:"column:total_count"`
	}
	h.DB.Model(&model.WriteOffDayStatistics{}).
		Where("writeoff_id = ?", writeoff.ID).
		Select("COALESCE(SUM(success_money), 0) as total_money, COALESCE(SUM(success_count), 0) as total_count").
		Scan(&totalSuccess)

	result["today_success_money"] = todayStat.SuccessMoney
	result["today_success_count"] = todayStat.SuccessCount
	result["today_submit_count"] = todayStat.SubmitCount
	result["yesterday_success_money"] = yesterdayStat.SuccessMoney
	result["yesterday_success_count"] = yesterdayStat.SuccessCount
	result["total_success_money"] = totalSuccess.TotalMoney
	result["total_success_count"] = totalSuccess.TotalCount

	result["device_distribution"] = map[string]interface{}{
		"android": todayStat.AndroidCount,
		"ios":     todayStat.IOSCount,
		"pc":      todayStat.PCCount,
		"unknown": todayStat.UnknownCount,
	}

	writeoffID := writeoff.ID
	result["order_trend"] = h.getOrderTrend(nil, nil, &writeoffID, 15)
}

// getOrderTrend 获取近N天订单趋势
func (h *DataAnalysisHandler) getOrderTrend(tenantID *uint, merchantID *uint, writeoffID *uint, days int) []map[string]interface{} {
	now := time.Now()
	startDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))

	var results []map[string]interface{}

	if tenantID != nil {
		var stats []model.TenantDayStatistics
		h.DB.Where("date >= ? AND tenant_id = ?", startDate, *tenantID).Order("date ASC").Find(&stats)
		for _, s := range stats {
			results = append(results, map[string]interface{}{
				"date":          s.Date.Format("2006-01-02"),
				"submit_count":  s.SubmitCount,
				"success_count": s.SuccessCount,
				"success_money": s.SuccessMoney,
			})
		}
	} else if merchantID != nil {
		var stats []model.MerchantDayStatistics
		h.DB.Where("date >= ? AND merchant_id = ?", startDate, *merchantID).Order("date ASC").Find(&stats)
		for _, s := range stats {
			results = append(results, map[string]interface{}{
				"date":          s.Date.Format("2006-01-02"),
				"submit_count":  s.SubmitCount,
				"success_count": s.SuccessCount,
				"success_money": s.SuccessMoney,
			})
		}
	} else if writeoffID != nil {
		var stats []model.WriteOffDayStatistics
		h.DB.Where("date >= ? AND writeoff_id = ?", startDate, *writeoffID).Order("date ASC").Find(&stats)
		for _, s := range stats {
			results = append(results, map[string]interface{}{
				"date":          s.Date.Format("2006-01-02"),
				"submit_count":  s.SubmitCount,
				"success_count": s.SuccessCount,
				"success_money": s.SuccessMoney,
			})
		}
	} else {
		var stats []model.DayStatistics
		h.DB.Where("date >= ?", startDate).Order("date ASC").Find(&stats)
		for _, s := range stats {
			results = append(results, map[string]interface{}{
				"date":          s.Date.Format("2006-01-02"),
				"submit_count":  s.SubmitCount,
				"success_count": s.SuccessCount,
				"success_money": s.SuccessMoney,
			})
		}
	}

	if results == nil {
		results = []map[string]interface{}{}
	}
	return results
}

// DayStatisticsList 日统计列表
// GET /api/statistics/day/
func (h *DataAnalysisHandler) DayStatisticsList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	roleKey := user.Role.Key

	switch roleKey {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
		h.dayStatisticsGlobal(c, offset, limit, startDate, endDate)
	case model.RoleKeyTenant:
		h.dayStatisticsTenant(c, user, offset, limit, startDate, endDate)
	case model.RoleKeyMerchant:
		h.dayStatisticsMerchant(c, user, offset, limit, startDate, endDate)
	case model.RoleKeyWriteoff:
		h.dayStatisticsWriteoff(c, user, offset, limit, startDate, endDate)
	default:
		response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
	}
}

func (h *DataAnalysisHandler) dayStatisticsGlobal(c *gin.Context, offset, limit int, startDate, endDate string) {
	query := h.DB.Model(&model.DayStatistics{})
	if startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var stats []model.DayStatistics
	query.Order("date DESC").Offset(offset).Limit(limit).Find(&stats)

	// 汇总
	var summary struct {
		TotalSubmit  int   `gorm:"column:total_submit"`
		TotalSuccess int   `gorm:"column:total_success"`
		TotalMoney   int64 `gorm:"column:total_money"`
		TotalTax     int64 `gorm:"column:total_tax"`
	}
	summaryQuery := h.DB.Model(&model.DayStatistics{})
	if startDate != "" {
		summaryQuery = summaryQuery.Where("date >= ?", startDate)
	}
	if endDate != "" {
		summaryQuery = summaryQuery.Where("date <= ?", endDate)
	}
	summaryQuery.Select("COALESCE(SUM(submit_count),0) as total_submit, COALESCE(SUM(success_count),0) as total_success, COALESCE(SUM(success_money),0) as total_money, COALESCE(SUM(total_tax),0) as total_tax").Scan(&summary)

	response.DetailResponse(c, gin.H{
		"data":  stats,
		"total": total,
		"summary": gin.H{
			"submit_count":  summary.TotalSubmit,
			"success_count": summary.TotalSuccess,
			"success_money": summary.TotalMoney,
			"total_tax":     summary.TotalTax,
		},
	}, "")
}

func (h *DataAnalysisHandler) dayStatisticsTenant(c *gin.Context, user *model.Users, offset, limit int, startDate, endDate string) {
	var tenant model.Tenant
	if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
		response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
		return
	}

	query := h.DB.Model(&model.TenantDayStatistics{}).Where("tenant_id = ?", tenant.ID)
	if startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var stats []model.TenantDayStatistics
	query.Order("date DESC").Offset(offset).Limit(limit).Find(&stats)

	response.DetailResponse(c, gin.H{"data": stats, "total": total}, "")
}

func (h *DataAnalysisHandler) dayStatisticsMerchant(c *gin.Context, user *model.Users, offset, limit int, startDate, endDate string) {
	var merchant model.Merchant
	if err := h.DB.Where("system_user_id = ?", user.ID).First(&merchant).Error; err != nil {
		response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
		return
	}

	query := h.DB.Model(&model.MerchantDayStatistics{}).Where("merchant_id = ?", merchant.ID)
	if startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var stats []model.MerchantDayStatistics
	query.Order("date DESC").Offset(offset).Limit(limit).Find(&stats)

	response.DetailResponse(c, gin.H{"data": stats, "total": total}, "")
}

func (h *DataAnalysisHandler) dayStatisticsWriteoff(c *gin.Context, user *model.Users, offset, limit int, startDate, endDate string) {
	var writeoff model.WriteOff
	if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
		response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
		return
	}

	query := h.DB.Model(&model.WriteOffDayStatistics{}).Where("writeoff_id = ?", writeoff.ID)
	if startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var stats []model.WriteOffDayStatistics
	query.Order("date DESC").Offset(offset).Limit(limit).Find(&stats)

	response.DetailResponse(c, gin.H{"data": stats, "total": total}, "")
}

// DayStatisticsExport 日统计导出CSV
// GET /api/statistics/day/export/
func (h *DataAnalysisHandler) DayStatisticsExport(c *gin.Context) {
	// 仅管理员/运维可导出全局统计，防止其他角色获取系统级财务数据
	user, _ := middleware.GetCurrentUser(c)
	if user == nil || (user.Role.Key != model.RoleKeyAdmin && user.Role.Key != model.RoleKeyOperation) {
		response.ErrorResponse(c, "无权导出")
		return
	}

	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	query := h.DB.Model(&model.DayStatistics{})
	if startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	var stats []model.DayStatistics
	query.Order("date DESC").Find(&stats)

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=day_statistics_%s.csv", time.Now().Format("20060102")))
	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM for Excel

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	writer.Write([]string{"日期", "提交数", "成功数", "成功金额(元)", "利润(元)", "Android", "iOS", "PC", "未知"})

	for _, s := range stats {
		writer.Write([]string{
			s.Date.Format("2006-01-02"),
			strconv.Itoa(s.SubmitCount),
			strconv.Itoa(s.SuccessCount),
			fmt.Sprintf("%.2f", float64(s.SuccessMoney)/100),
			fmt.Sprintf("%.2f", float64(s.TotalTax)/100),
			strconv.Itoa(s.AndroidCount),
			strconv.Itoa(s.IOSCount),
			strconv.Itoa(s.PCCount),
			strconv.Itoa(s.UnknownCount),
		})
	}
}

// PayChannelStatsList 支付通道统计列表
// GET /api/statistics/channel/
func (h *DataAnalysisHandler) PayChannelStatsList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	query := h.DB.Model(&model.PayChannelDayStatistics{}).Preload("PayChannel")

	roleKey := user.Role.Key

	switch roleKey {
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
			return
		}
		query = query.Where("tenant_id = ?", tenant.ID)
	case model.RoleKeyMerchant:
		var merchant model.Merchant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&merchant).Error; err != nil {
			response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
			return
		}
		query = query.Where("merchant_id = ?", merchant.ID)
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
			return
		}
		query = query.Where("writeoff_id = ?", writeoff.ID)
	}

	if startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var stats []model.PayChannelDayStatistics
	query.Order("date DESC").Offset(offset).Limit(limit).Find(&stats)

	response.DetailResponse(c, gin.H{"data": stats, "total": total}, "")
}

// SplitGroupStatsList 分账组统计列表
// GET /api/statistics/split_group/
func (h *DataAnalysisHandler) SplitGroupStatsList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	query := h.DB.Model(&model.AlipaySplitUserGroup{}).Where("deleted_at IS NULL")

	roleKey := user.Role.Key
	if roleKey == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
			return
		}
		query = query.Where("tenant_id = ?", tenant.ID)
	}

	var total int64
	query.Count(&total)

	var groups []model.AlipaySplitUserGroup
	query.Preload("Tenant").Preload("Writeoff").
		Offset(offset).Limit(limit).Find(&groups)

	// 附加每个组的今日流水和预付余额
	type groupResult struct {
		model.AlipaySplitUserGroup
		TodayFlow int64 `json:"today_flow"`
		PrePay    int64 `json:"pre_pay"`
	}

	today := time.Now().Format("2006-01-02")
	var results []groupResult
	for _, g := range groups {
		gr := groupResult{AlipaySplitUserGroup: g}

		// 今日流水
		var flow struct {
			Total int64 `gorm:"column:total"`
		}
		h.DB.Model(&model.AlipaySplitUserFlow{}).
			Joins("JOIN "+model.AlipaySplitUser{}.TableName()+" AS u ON u.id = "+model.AlipaySplitUserFlow{}.TableName()+".alipay_user_id").
			Where("u.group_id = ? AND "+model.AlipaySplitUserFlow{}.TableName()+".date = ?", g.ID, today).
			Select("COALESCE(SUM(flow), 0) as total").Scan(&flow)
		gr.TodayFlow = flow.Total

		// 预付余额
		var pre model.AlipaySplitUserGroupPre
		if err := h.DB.Where("group_id = ?", g.ID).First(&pre).Error; err == nil {
			gr.PrePay = pre.PrePay
		}

		results = append(results, gr)
	}

	if results == nil {
		response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": total}, "")
		return
	}
	response.DetailResponse(c, gin.H{"data": results, "total": total}, "")
}

// CollectionStatsList 归集统计列表
// GET /api/statistics/collection/
func (h *DataAnalysisHandler) CollectionStatsList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	query := h.DB.Model(&model.CollectionUser{})

	roleKey := user.Role.Key
	if roleKey == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
			return
		}
		query = query.Where("tenant_id = ?", tenant.ID)
	}

	var total int64
	query.Count(&total)

	var users []model.CollectionUser
	query.Offset(offset).Limit(limit).Find(&users)

	type collectionResult struct {
		model.CollectionUser
		TodayFlow int64 `json:"today_flow"`
		TotalFlow int64 `json:"total_flow"`
	}

	today := time.Now().Format("2006-01-02")
	var results []collectionResult
	for _, u := range users {
		cr := collectionResult{CollectionUser: u}

		// 今日流水
		flowQuery := h.DB.Model(&model.CollectionDayFlow{}).Where("user_id = ?", u.ID)
		if startDate != "" {
			flowQuery = flowQuery.Where("date >= ?", startDate)
		}
		if endDate != "" {
			flowQuery = flowQuery.Where("date <= ?", endDate)
		}

		var todayFlowVal struct {
			Total int64 `gorm:"column:total"`
		}
		h.DB.Model(&model.CollectionDayFlow{}).
			Where("user_id = ? AND date = ?", u.ID, today).
			Select("COALESCE(SUM(flow), 0) as total").Scan(&todayFlowVal)
		cr.TodayFlow = todayFlowVal.Total

		// 总流水
		var totalFlowVal struct {
			Total int64 `gorm:"column:total"`
		}
		flowQuery.Select("COALESCE(SUM(flow), 0) as total").Scan(&totalFlowVal)
		cr.TotalFlow = totalFlowVal.Total

		results = append(results, cr)
	}

	if results == nil {
		response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": total}, "")
		return
	}
	response.DetailResponse(c, gin.H{"data": results, "total": total}, "")
}

// OrderSuccessRate 订单成功率统计（实时窗口）
// GET /api/statistics/success_rate/
func (h *DataAnalysisHandler) OrderSuccessRate(c *gin.Context) {
	windows := []int{1, 3, 5, 10, 30, 60}
	now := time.Now()

	var rates []map[string]interface{}
	for _, min := range windows {
		start := now.Add(-time.Duration(min) * time.Minute)

		var submitCount, successCount int64
		h.DB.Model(&model.Order{}).Where("create_datetime >= ?", start).Count(&submitCount)
		h.DB.Model(&model.Order{}).Where("create_datetime >= ? AND order_status IN ?", start,
			[]model.OrderStatus{model.OrderStatusSuccess, model.OrderStatusSuccessPre}).Count(&successCount)

		rate := float64(0)
		if submitCount > 0 {
			rate = float64(successCount) / float64(submitCount) * 100
		}

		rates = append(rates, map[string]interface{}{
			"window":        fmt.Sprintf("%d分钟", min),
			"submit_count":  submitCount,
			"success_count": successCount,
			"rate":          fmt.Sprintf("%.2f", rate),
		})
	}

	response.DetailResponse(c, rates, "")
}
