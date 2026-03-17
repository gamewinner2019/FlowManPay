package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
)

// filterAllowedFields 从 updates map 中只保留允许更新的字段（防止批量赋值攻击）
func filterAllowedFields(updates map[string]interface{}, allowed []string) map[string]interface{} {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, f := range allowed {
		allowedSet[f] = struct{}{}
	}
	filtered := make(map[string]interface{}, len(allowed))
	for k, v := range updates {
		if _, ok := allowedSet[k]; ok {
			filtered[k] = v
		}
	}
	return filtered
}

// AlipayNativeHandler 支付宝原生管理 Handler
type AlipayNativeHandler struct {
	DB *gorm.DB
}

// NewAlipayNativeHandler 创建支付宝原生管理 Handler
func NewAlipayNativeHandler(db *gorm.DB) *AlipayNativeHandler {
	return &AlipayNativeHandler{DB: db}
}

// ===== AlipayProduct CRUD =====

// alipayProductBaseQuery 返回基础查询（排除软删除的产品，按角色过滤）
func (h *AlipayNativeHandler) alipayProductBaseQuery(user *model.Users) *gorm.DB {
	query := h.DB.Model(&model.AlipayProduct{}).Where("is_delete = ?", false)
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
		// 管理员/运维：看全部
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			return query.Where("1=0")
		}
		query = query.Where("writeoff_id = ? OR writeoff_id IN (?)",
			writeoff.ID,
			h.DB.Model(&model.WriteOff{}).Select("id").Where("parent_writeoff_id = ?", writeoff.ID))
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			return query.Where("1=0")
		}
		query = query.Where("writeoff_id IN (?)",
			h.DB.Model(&model.WriteOff{}).Select("id").Where("parent_id = ?", tenant.ID))
	default:
		return query.Where("1=0")
	}
	return query
}

// ProductList 支付宝产品列表
// GET /api/alipay/product/
func (h *AlipayNativeHandler) ProductList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	query := h.alipayProductBaseQuery(user)

	// 列表过滤: 仅展示parent_id为空或account_type=4的顶级产品
	query = query.Where("parent_id IS NULL OR account_type = 4")

	// 过滤条件
	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}
	if uid := c.Query("uid"); uid != "" {
		query = query.Where("uid LIKE ? OR app_id LIKE ?", "%"+uid+"%", "%"+uid+"%")
	}
	if accountType := c.Query("account_type"); accountType != "" {
		query = query.Where("account_type = ?", accountType)
	}
	if collectionType := c.Query("collection_type"); collectionType != "" {
		query = query.Where("collection_type = ?", collectionType)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}
	if canPay := c.Query("can_pay"); canPay != "" {
		query = query.Where("can_pay = ?", canPay == "true" || canPay == "1")
	}

	page, limit, offset := response.GetPagination(c)
	var total int64
	query.Count(&total)

	var products []model.AlipayProduct
	query.Preload("Writeoff").Preload("Writeoff.SystemUser").
		Order("create_datetime DESC").
		Offset(offset).Limit(limit).
		Find(&products)

	// 构造列表响应，附加子产品信息和今日统计
	type productItem struct {
		model.AlipayProduct
		WriteoffName string `json:"writeoff_name"`
		ChildCount   int64  `json:"child_count"`
		TodayMoney   int64  `json:"today_money"`
		TodayCount   int    `json:"today_count"`
	}
	today := time.Now().Format("2006-01-02")
	items := make([]productItem, 0, len(products))
	for _, p := range products {
		item := productItem{AlipayProduct: p}
		if p.Writeoff != nil && p.Writeoff.SystemUser.Name != "" {
			item.WriteoffName = p.Writeoff.SystemUser.Name
		}
		h.DB.Model(&model.AlipayProduct{}).
			Where("parent_id = ? AND is_delete = ?", p.ID, false).
			Count(&item.ChildCount)
		var stats struct {
			SuccessMoney int64 `gorm:"column:success_money"`
			SuccessCount int   `gorm:"column:success_count"`
		}
		h.DB.Model(&model.AlipayProductDayStatistics{}).
			Where("product_id = ? AND date = ?", p.ID, today).
			Select("COALESCE(SUM(success_money),0) as success_money, COALESCE(SUM(success_count),0) as success_count").
			Scan(&stats)
		item.TodayMoney = stats.SuccessMoney
		item.TodayCount = stats.SuccessCount
		items = append(items, item)
	}

	response.PageResponse(c, items, total, page, limit, "")
}

// ProductRetrieve 支付宝产品详情
// GET /api/alipay/product/:id/
func (h *AlipayNativeHandler) ProductRetrieve(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id := c.Param("id")

	var product model.AlipayProduct
	query := h.alipayProductBaseQuery(user)
	if err := query.Where("id = ?", id).
		Preload("Writeoff").Preload("Writeoff.SystemUser").
		Preload("AllowPayChannels").
		First(&product).Error; err != nil {
		response.ErrorResponse(c, "产品不存在")
		return
	}

	// 查子产品列表
	var children []model.AlipayProduct
	h.DB.Where("parent_id = ? AND is_delete = ?", product.ID, false).
		Preload("Writeoff").Preload("Writeoff.SystemUser").
		Find(&children)

	response.DetailResponse(c, gin.H{
		"product":  product,
		"children": children,
	}, "")
}

// ProductCreate 创建支付宝产品
// POST /api/alipay/product/
func (h *AlipayNativeHandler) ProductCreate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	var req model.AlipayProduct
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	req.Creator = &user.ID
	req.Modifier = &user.ID

	if err := h.DB.Create(&req).Error; err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			response.ErrorResponse(c, "产品名称已存在")
			return
		}
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}

	response.DetailResponse(c, req, "创建成功")
}

// ProductUpdate 更新支付宝产品
// PUT /api/alipay/product/:id/
func (h *AlipayNativeHandler) ProductUpdate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id := c.Param("id")

	var product model.AlipayProduct
	if err := h.alipayProductBaseQuery(user).Where("id = ?", id).First(&product).Error; err != nil {
		response.ErrorResponse(c, "产品不存在")
		return
	}

	var raw map[string]interface{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}
	updates := filterAllowedFields(raw, []string{
		"name", "uid", "app_id", "status", "can_pay", "account_type", "sign_type",
		"collection_type", "max_fail_count", "limit_money", "max_money", "min_money",
		"float_min_money", "float_max_money", "day_count_limit", "settled_moneys",
		"subject", "private_key", "public_key", "app_public_crt", "alipay_public_crt",
		"alipay_root_crt", "split_async", "proxy", "description", "writeoff_id", "parent_id",
	})
	updates["modifier"] = user.ID

	if err := h.DB.Model(&product).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败: "+err.Error())
		return
	}

	h.DB.First(&product, product.ID)
	response.DetailResponse(c, product, "更新成功")
}

// ProductDelete 删除支付宝产品（软删除：标记 is_delete=true）
// DELETE /api/alipay/product/:id/
func (h *AlipayNativeHandler) ProductDelete(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id := c.Param("id")

	var product model.AlipayProduct
	if err := h.alipayProductBaseQuery(user).Where("id = ?", id).First(&product).Error; err != nil {
		response.ErrorResponse(c, "产品不存在")
		return
	}

	// 对标 Django delete_alipay: 重命名 + is_delete=true
	newName := product.Name + "[已删除0]"
	counter := 0
	for {
		var count int64
		h.DB.Model(&model.AlipayProduct{}).Where("name = ?", newName).Count(&count)
		if count == 0 {
			break
		}
		counter++
		newName = fmt.Sprintf("%s[已删除%d]", product.Name, counter)
	}

	h.DB.Model(&product).Updates(map[string]interface{}{
		"is_delete": true,
		"name":      newName,
	})
	// 删除关联的分账历史
	h.DB.Where("alipay_product = ?", product.ID).Delete(&model.SplitHistory{})

	response.DetailResponse(c, nil, "删除成功")
}

// ProductSimple 支付宝产品简单列表（下拉选择用）
// GET /api/alipay/product/simple/
func (h *AlipayNativeHandler) ProductSimple(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	query := h.alipayProductBaseQuery(user)

	var products []struct {
		ID   uint   `json:"id"`
		Name string `json:"name"`
		UID  string `json:"uid"`
	}
	query.Select("id, name, uid").Find(&products)

	response.DetailResponse(c, products, "")
}

// ===== 产品统计 =====

// ProductStatisticsDay 产品日统计
// GET /api/alipay/product/:id/statistics/day/
func (h *AlipayNativeHandler) ProductStatisticsDay(c *gin.Context) {
	id := c.Param("id")
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	type statsResult struct {
		TodaySubmit    int   `json:"today_submit"`
		TodaySuccess   int   `json:"today_success"`
		TodayMoney     int64 `json:"today_money"`
		YesterdayMoney int64 `json:"yesterday_money"`
		TotalMoney     int64 `json:"total_money"`
	}

	var result statsResult
	// 今日
	var todayResult struct {
		TodaySubmit  int   `gorm:"column:today_submit"`
		TodaySuccess int   `gorm:"column:today_success"`
		TodayMoney   int64 `gorm:"column:today_money"`
	}
	h.DB.Model(&model.AlipayProductDayStatistics{}).
		Where("product_id = ? AND date = ?", id, today).
		Select("COALESCE(SUM(submit_count),0) as today_submit, COALESCE(SUM(success_count),0) as today_success, COALESCE(SUM(success_money),0) as today_money").
		Scan(&todayResult)
	result.TodaySubmit = todayResult.TodaySubmit
	result.TodaySuccess = todayResult.TodaySuccess
	result.TodayMoney = todayResult.TodayMoney
	// 昨日
	var yesterdayResult struct {
		YesterdayMoney int64 `gorm:"column:yesterday_money"`
	}
	h.DB.Model(&model.AlipayProductDayStatistics{}).
		Where("product_id = ? AND date = ?", id, yesterday).
		Select("COALESCE(SUM(success_money),0) as yesterday_money").
		Scan(&yesterdayResult)
	result.YesterdayMoney = yesterdayResult.YesterdayMoney
	// 累计
	var totalResult struct {
		TotalMoney int64 `gorm:"column:total_money"`
	}
	h.DB.Model(&model.AlipayProductDayStatistics{}).
		Where("product_id = ?", id).
		Select("COALESCE(SUM(success_money),0) as total_money").
		Scan(&totalResult)
	result.TotalMoney = totalResult.TotalMoney

	response.DetailResponse(c, result, "")
}

// ProductStatisticsChannel 产品按通道统计
// GET /api/alipay/product/:id/statistics/channel/
func (h *AlipayNativeHandler) ProductStatisticsChannel(c *gin.Context) {
	id := c.Param("id")
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	type channelStats struct {
		PayChannelID uint   `json:"pay_channel_id"`
		ChannelName  string `json:"channel_name"`
		TodayMoney   int64  `json:"today_money"`
		YesterdayMoney int64 `json:"yesterday_money"`
		TotalMoney   int64  `json:"total_money"`
	}

	// 获取该产品关联的所有通道统计
	var stats []channelStats
	h.DB.Raw(`
		SELECT s.pay_channel_id, pc.name as channel_name,
			COALESCE(SUM(CASE WHEN s.date = ? THEN s.success_money ELSE 0 END), 0) as today_money,
			COALESCE(SUM(CASE WHEN s.date = ? THEN s.success_money ELSE 0 END), 0) as yesterday_money,
			COALESCE(SUM(s.success_money), 0) as total_money
		FROM `+model.AlipayProductDayStatistics{}.TableName()+` s
		LEFT JOIN `+model.PayChannel{}.TableName()+` pc ON pc.id = s.pay_channel_id
		WHERE s.product_id = ?
		GROUP BY s.pay_channel_id, pc.name
	`, today, yesterday, id).Scan(&stats)

	response.DetailResponse(c, stats, "")
}

// ===== 产品标签管理 =====

// ProductTags 获取产品标签列表
// GET /api/alipay/product/tags/
func (h *AlipayNativeHandler) ProductTags(c *gin.Context) {
	var tags []model.AlipayProductTag
	h.DB.Order("sort ASC").Find(&tags)
	response.DetailResponse(c, tags, "")
}

// ProductTagsAdd 添加产品标签
// POST /api/alipay/product/tags/
func (h *AlipayNativeHandler) ProductTagsAdd(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	var req struct {
		Name string `json:"name" binding:"required"`
		Sort int    `json:"sort"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	tag := model.AlipayProductTag{
		Name:         req.Name,
		SystemUserID: &user.ID,
		Sort:         req.Sort,
	}
	if err := h.DB.Create(&tag).Error; err != nil {
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "PRIMARY") {
			response.ErrorResponse(c, "标签已存在")
			return
		}
		response.ErrorResponse(c, "创建失败")
		return
	}
	response.DetailResponse(c, tag, "创建成功")
}

// ProductTagsDelete 删除产品标签
// DELETE /api/alipay/product/tags/:name/
func (h *AlipayNativeHandler) ProductTagsDelete(c *gin.Context) {
	name := c.Param("name")
	if err := h.DB.Where("name = ?", name).Delete(&model.AlipayProductTag{}).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ===== 产品权重管理 =====

// ProductWeightList 产品权重列表
// GET /api/alipay/product/:id/weight/
func (h *AlipayNativeHandler) ProductWeightList(c *gin.Context) {
	id := c.Param("id")
	var weights []model.AlipayWeight
	h.DB.Where("alipay_id = ?", id).Find(&weights)

	// 附加通道名
	type weightItem struct {
		model.AlipayWeight
		ChannelName string `json:"channel_name"`
	}
	items := make([]weightItem, 0, len(weights))
	for _, w := range weights {
		item := weightItem{AlipayWeight: w}
		var ch model.PayChannel
		if h.DB.First(&ch, w.PayChannelID).Error == nil {
			item.ChannelName = ch.Name
		}
		items = append(items, item)
	}

	response.DetailResponse(c, items, "")
}

// ProductWeightSet 设置产品权重
// POST /api/alipay/product/:id/weight/
func (h *AlipayNativeHandler) ProductWeightSet(c *gin.Context) {
	id := c.Param("id")
	productID, _ := strconv.ParseUint(id, 10, 64)

	var req struct {
		PayChannelID uint `json:"pay_channel_id" binding:"required"`
		Weight       int  `json:"weight"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var existing model.AlipayWeight
	result := h.DB.Where("alipay_id = ? AND pay_channel_id = ?", productID, req.PayChannelID).First(&existing)
	if result.Error == nil {
		h.DB.Model(&existing).Update("weight", req.Weight)
	} else {
		h.DB.Create(&model.AlipayWeight{
			AlipayID:     uint(productID),
			PayChannelID: req.PayChannelID,
			Weight:       req.Weight,
		})
	}

	response.DetailResponse(c, nil, "设置成功")
}

// ===== 产品通道管理 =====

// ProductPayChannelList 产品可用通道列表
// GET /api/alipay/product/:id/pay_channel/
func (h *AlipayNativeHandler) ProductPayChannelList(c *gin.Context) {
	id := c.Param("id")
	var product model.AlipayProduct
	if err := h.DB.Preload("AllowPayChannels").First(&product, id).Error; err != nil {
		response.ErrorResponse(c, "产品不存在")
		return
	}
	response.DetailResponse(c, product.AllowPayChannels, "")
}

// ===== 产品批量操作 =====

// ProductBatch 批量更新产品状态
// POST /api/alipay/product/batch/
func (h *AlipayNativeHandler) ProductBatch(c *gin.Context) {
	var req struct {
		IDs    []uint `json:"ids" binding:"required"`
		Status *bool  `json:"status"`
		CanPay *bool  `json:"can_pay"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	updates := map[string]interface{}{}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.CanPay != nil {
		updates["can_pay"] = *req.CanPay
	}

	if len(updates) == 0 {
		response.ErrorResponse(c, "无更新字段")
		return
	}

	h.DB.Model(&model.AlipayProduct{}).
		Where("id IN ? AND is_delete = ?", req.IDs, false).
		Updates(updates)

	response.DetailResponse(c, nil, fmt.Sprintf("批量更新%d个产品成功", len(req.IDs)))
}

// ===== 转账用户管理 =====

// TransferUserList 转账用户列表
// GET /api/alipay/product/:id/transfer_user/
func (h *AlipayNativeHandler) TransferUserList(c *gin.Context) {
	id := c.Param("id")
	var users []model.AlipayTransferUser
	query := h.DB.Where("alipay_product_id = ?", id)

	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}
	if username := c.Query("username"); username != "" {
		query = query.Where("username LIKE ?", "%"+username+"%")
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}

	query.Order("create_datetime DESC").Find(&users)
	response.DetailResponse(c, users, "")
}

// TransferUserCreate 创建转账用户
// POST /api/alipay/product/:id/transfer_user/
func (h *AlipayNativeHandler) TransferUserCreate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id := c.Param("id")
	productID, _ := strconv.ParseUint(id, 10, 64)

	var req model.AlipayTransferUser
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	pid := uint(productID)
	req.AlipayProductID = &pid
	req.Creator = &user.ID
	req.Modifier = &user.ID

	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}

	response.DetailResponse(c, req, "创建成功")
}

// TransferUserUpdate 更新转账用户
// PUT /api/alipay/product/:pid/transfer_user/:id/
func (h *AlipayNativeHandler) TransferUserUpdate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	uid := c.Param("uid")

	var tu model.AlipayTransferUser
	if err := h.DB.First(&tu, uid).Error; err != nil {
		response.ErrorResponse(c, "用户不存在")
		return
	}

	var raw map[string]interface{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}
	updates := filterAllowedFields(raw, []string{
		"username_type", "username", "name", "status", "limit_money", "description",
	})
	updates["modifier"] = user.ID

	h.DB.Model(&tu).Updates(updates)
	response.DetailResponse(c, tu, "更新成功")
}

// TransferUserDelete 删除转账用户
// DELETE /api/alipay/product/:pid/transfer_user/:id/
func (h *AlipayNativeHandler) TransferUserDelete(c *gin.Context) {
	uid := c.Param("uid")
	if err := h.DB.Delete(&model.AlipayTransferUser{}, uid).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ===== 转账历史 =====

// TransferHistoryList 转账历史列表
// GET /api/alipay/transfer/history/
func (h *AlipayNativeHandler) TransferHistoryList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.TransferHistory{}).
		Joins("LEFT JOIN "+model.AlipayProduct{}.TableName()+" ap ON ap.id = "+model.TransferHistory{}.TableName()+".alipay_product_id").
		Where("ap.is_delete = ?", false)

	// 权限过滤
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
		// 全部
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where(model.TransferHistory{}.TableName()+".writeoff = ?", writeoff.ID)
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where(model.TransferHistory{}.TableName()+".tenant_id = ?", tenant.ID)
	default:
		response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
		return
	}

	// 过滤条件
	if transferStatus := c.Query("transfer_status"); transferStatus != "" {
		query = query.Where(model.TransferHistory{}.TableName()+".transfer_status = ?", transferStatus)
	}
	if name := c.Query("name"); name != "" {
		query = query.Where("ap.name LIKE ?", "%"+name+"%")
	}
	if writeoffName := c.Query("writeoff_name"); writeoffName != "" {
		query = query.Where(model.TransferHistory{}.TableName()+".writeoff_name LIKE ?", "%"+writeoffName+"%")
	}
	if userName := c.Query("user_name"); userName != "" {
		query = query.Where(model.TransferHistory{}.TableName()+".user_name LIKE ?", "%"+userName+"%")
	}
	if errFilter := c.Query("error"); errFilter != "" {
		query = query.Where(model.TransferHistory{}.TableName()+".error LIKE ?", "%"+errFilter+"%")
	}
	if idFilter := c.Query("id"); idFilter != "" {
		query = query.Where(model.TransferHistory{}.TableName()+".id LIKE ?", "%"+idFilter+"%")
	}

	page, limit, offset := response.GetPagination(c)
	var total int64
	query.Count(&total)

	var histories []model.TransferHistory
	query.Preload("AlipayProduct").Preload("AlipayUser").
		Order(model.TransferHistory{}.TableName() + ".create_datetime DESC").
		Offset(offset).Limit(limit).
		Find(&histories)

	response.PageResponse(c, histories, total, page, limit, "")
}

// TransferHistoryStatistics 转账历史统计
// GET /api/alipay/transfer/history/statistics/
func (h *AlipayNativeHandler) TransferHistoryStatistics(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	query := h.DB.Model(&model.TransferHistory{}).
		Joins("LEFT JOIN "+model.AlipayProduct{}.TableName()+" ap ON ap.id = "+model.TransferHistory{}.TableName()+".alipay_product_id").
		Where("ap.is_delete = ?", false)

	// 权限过滤
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
		// 全部
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.DetailResponse(c, gin.H{}, "")
			return
		}
		query = query.Where(model.TransferHistory{}.TableName()+".writeoff = ?", writeoff.ID)
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.DetailResponse(c, gin.H{}, "")
			return
		}
		query = query.Where(model.TransferHistory{}.TableName()+".tenant_id = ?", tenant.ID)
	}

	thTable := model.TransferHistory{}.TableName()
	var result struct {
		TodayMoney    int64 `gorm:"column:today_money"`
		YesterdayMoney int64 `gorm:"column:yesterday_money"`
		TotalMoney    int64 `gorm:"column:total_money"`
		SuccessCount  int64 `gorm:"column:success_count"`
		WaitCount     int64 `gorm:"column:wait_count"`
		FailCount     int64 `gorm:"column:fail_count"`
	}

	query.Select(fmt.Sprintf(`
		COALESCE(SUM(CASE WHEN %s.transfer_status=1 AND %s.create_datetime >= ? THEN %s.money ELSE 0 END),0) as today_money,
		COALESCE(SUM(CASE WHEN %s.transfer_status=1 AND %s.create_datetime >= ? AND %s.create_datetime < ? THEN %s.money ELSE 0 END),0) as yesterday_money,
		COALESCE(SUM(CASE WHEN %s.transfer_status=1 THEN %s.money ELSE 0 END),0) as total_money,
		COALESCE(SUM(CASE WHEN %s.transfer_status=1 THEN 1 ELSE 0 END),0) as success_count,
		COALESCE(SUM(CASE WHEN %s.transfer_status=0 THEN 1 ELSE 0 END),0) as wait_count,
		COALESCE(SUM(CASE WHEN %s.transfer_status=2 THEN 1 ELSE 0 END),0) as fail_count
	`, thTable, thTable, thTable, thTable, thTable, thTable, thTable, thTable, thTable, thTable, thTable, thTable),
		today, yesterday, today).
		Scan(&result)

	response.DetailResponse(c, result, "")
}

// ===== 公池管理 =====

// PublicPoolList 公池列表
// GET /api/alipay/public_pool/
func (h *AlipayNativeHandler) PublicPoolList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.AlipayPublicPool{}).
		Joins("LEFT JOIN "+model.AlipayProduct{}.TableName()+" ap ON ap.id = "+model.AlipayPublicPool{}.TableName()+".alipay_id").
		Where("ap.is_delete = ?", false)

	// 权限过滤
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
		// 全部
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("ap.writeoff_id = ?", writeoff.ID)
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("ap.writeoff_id IN (?)",
			h.DB.Model(&model.WriteOff{}).Select("id").Where("parent_id = ?", tenant.ID))
	default:
		response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
		return
	}

	// 过滤
	if name := c.Query("name"); name != "" {
		query = query.Where("ap.name LIKE ?", "%"+name+"%")
	}
	if uid := c.Query("uid"); uid != "" {
		query = query.Where("ap.uid LIKE ? OR ap.app_id LIKE ?", "%"+uid+"%", "%"+uid+"%")
	}
	if status := c.Query("status"); status != "" {
		query = query.Where(model.AlipayPublicPool{}.TableName()+".status = ?", status == "true" || status == "1")
	}

	page, limit, offset := response.GetPagination(c)
	var total int64
	query.Count(&total)

	var pools []model.AlipayPublicPool
	query.Preload("Alipay").
		Order(model.AlipayPublicPool{}.TableName() + ".id DESC").
		Offset(offset).Limit(limit).
		Find(&pools)

	response.PageResponse(c, pools, total, page, limit, "")
}

// PublicPoolUpdate 更新公池状态
// PUT /api/alipay/public_pool/:id/
func (h *AlipayNativeHandler) PublicPoolUpdate(c *gin.Context) {
	id := c.Param("id")
	var pool model.AlipayPublicPool
	if err := h.DB.First(&pool, id).Error; err != nil {
		response.ErrorResponse(c, "公池不存在")
		return
	}

	var raw map[string]interface{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}
	updates := filterAllowedFields(raw, []string{"status"})

	h.DB.Model(&pool).Updates(updates)
	response.DetailResponse(c, pool, "更新成功")
}

// PublicPoolDelete 删除公池
// DELETE /api/alipay/public_pool/:id/
func (h *AlipayNativeHandler) PublicPoolDelete(c *gin.Context) {
	id := c.Param("id")
	var pool model.AlipayPublicPool
	if err := h.DB.First(&pool, id).Error; err != nil {
		response.ErrorResponse(c, "公池不存在")
		return
	}
	h.DB.Delete(&pool)
	response.DetailResponse(c, nil, "删除成功")
}

// PublicPoolStatistics 公池统计
// GET /api/alipay/public_pool/statistics/
func (h *AlipayNativeHandler) PublicPoolStatistics(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	poolQuery := h.DB.Model(&model.AlipayPublicPool{}).
		Joins("LEFT JOIN "+model.AlipayProduct{}.TableName()+" ap ON ap.id = "+model.AlipayPublicPool{}.TableName()+".alipay_id").
		Where("ap.is_delete = ?", false)

	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.DetailResponse(c, gin.H{}, "")
			return
		}
		poolQuery = poolQuery.Where("ap.writeoff_id = ?", writeoff.ID)
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.DetailResponse(c, gin.H{}, "")
			return
		}
		poolQuery = poolQuery.Where("ap.writeoff_id IN (?)",
			h.DB.Model(&model.WriteOff{}).Select("id").Where("parent_id = ?", tenant.ID))
	}

	// 统计在线/总数
	var countResult struct {
		OnlineCount int64 `gorm:"column:online_count"`
		TotalCount  int64 `gorm:"column:total_count"`
	}
	poolQuery.Select(`
		COALESCE(SUM(CASE WHEN `+model.AlipayPublicPool{}.TableName()+`.status = 1 THEN 1 ELSE 0 END),0) as online_count,
		COUNT(*) as total_count
	`).Scan(&countResult)

	// 流水统计（复用带RBAC过滤的poolQuery，避免跨租户数据泄露）
	var poolIDs []uint
	poolQuery2 := h.DB.Model(&model.AlipayPublicPool{}).
		Joins("LEFT JOIN "+model.AlipayProduct{}.TableName()+" ap ON ap.id = "+model.AlipayPublicPool{}.TableName()+".alipay_id").
		Where("ap.is_delete = ?", false)
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
	case model.RoleKeyWriteoff:
		var wo model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&wo).Error; err == nil {
			poolQuery2 = poolQuery2.Where("ap.writeoff_id = ?", wo.ID)
		}
	case model.RoleKeyTenant:
		var t model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&t).Error; err == nil {
			poolQuery2 = poolQuery2.Where("ap.writeoff_id IN (?)",
				h.DB.Model(&model.WriteOff{}).Select("id").Where("parent_id = ?", t.ID))
		}
	}
	poolQuery2.Pluck(model.AlipayPublicPool{}.TableName()+".id", &poolIDs)

	var flowResult struct {
		TodayFlow    int64 `gorm:"column:today_flow"`
		YesterdayFlow int64 `gorm:"column:yesterday_flow"`
		TotalFlow    int64 `gorm:"column:total_flow"`
	}
	if len(poolIDs) > 0 {
		h.DB.Model(&model.AlipayPublicPoolDayStatistics{}).
			Where("pool_id IN ?", poolIDs).
			Select(`
				COALESCE(SUM(CASE WHEN date = ? THEN success_money ELSE 0 END),0) as today_flow,
				COALESCE(SUM(CASE WHEN date = ? THEN success_money ELSE 0 END),0) as yesterday_flow,
				COALESCE(SUM(success_money),0) as total_flow
			`, today, yesterday).
			Scan(&flowResult)
	}

	// 今日活跃公池数
	var successCount int64
	if len(poolIDs) > 0 {
		h.DB.Model(&model.AlipayPublicPoolDayStatistics{}).
			Where("pool_id IN ? AND date = ?", poolIDs, today).
			Distinct("pool_id").Count(&successCount)
	}

	response.DetailResponse(c, gin.H{
		"online_count":  countResult.OnlineCount,
		"total_count":   countResult.TotalCount,
		"today_flow":    flowResult.TodayFlow,
		"yesterday_flow": flowResult.YesterdayFlow,
		"total_flow":    flowResult.TotalFlow,
		"success_count": successCount,
	}, "")
}

// ===== 投诉管理 =====

// ComplainList 投诉列表
// GET /api/alipay/complain/
func (h *AlipayNativeHandler) ComplainList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.AlipayComplain{})

	// 权限过滤
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
		// 全部
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("alipay_product_id IN (?)",
			h.DB.Model(&model.AlipayProduct{}).Select("id").
				Where("writeoff_id = ? AND is_delete = ?", writeoff.ID, false))
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("alipay_product_id IN (?)",
			h.DB.Model(&model.AlipayProduct{}).Select("id").
				Where("writeoff_id IN (?) AND is_delete = ?",
					h.DB.Model(&model.WriteOff{}).Select("id").Where("parent_id = ?", tenant.ID), false))
	default:
		response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
		return
	}

	page, limit, offset := response.GetPagination(c)
	var total int64
	query.Count(&total)

	var complains []model.AlipayComplain
	query.Preload("AlipayProduct").
		Order("create_datetime DESC").
		Offset(offset).Limit(limit).
		Find(&complains)

	response.PageResponse(c, complains, total, page, limit, "")
}

// ComplainUpdate 更新投诉
// PUT /api/alipay/complain/:id/
func (h *AlipayNativeHandler) ComplainUpdate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id := c.Param("id")

	var complain model.AlipayComplain
	if err := h.DB.First(&complain, id).Error; err != nil {
		response.ErrorResponse(c, "投诉不存在")
		return
	}

	var raw map[string]interface{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}
	updates := filterAllowedFields(raw, []string{"status", "remark"})
	updates["modifier"] = user.ID

	h.DB.Model(&complain).Updates(updates)
	response.DetailResponse(c, complain, "更新成功")
}

// ===== 分账历史 =====

// SplitNativeHistoryList 分账历史列表（原生管理-支付宝）
// GET /api/alipay/split/history/
func (h *AlipayNativeHandler) SplitNativeHistoryList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.SplitHistory{}).Where("hide = ?", false)

	// 权限过滤
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
		// 全部
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("alipay_product IN (?)",
			h.DB.Model(&model.AlipayProduct{}).Select("id").
				Where("writeoff_id = ? AND is_delete = ?", writeoff.ID, false))
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("alipay_user IN (?)",
			h.DB.Model(&model.AlipaySplitUser{}).Select("id").
				Where("group_id IN (?)",
					h.DB.Model(&model.AlipaySplitUserGroup{}).Select("id").Where("tenant_id = ?", tenant.ID)))
	default:
		response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
		return
	}

	// 过滤条件
	if splitStatus := c.Query("split_status"); splitStatus != "" {
		query = query.Where("split_status = ?", splitStatus)
	}
	if splitType := c.Query("split_type"); splitType != "" {
		query = query.Where("split_type = ?", splitType)
	}
	if errFilter := c.Query("error"); errFilter != "" {
		query = query.Where("error LIKE ?", "%"+errFilter+"%")
	}
	if name := c.Query("name"); name != "" {
		query = query.Where("alipay_product IN (?)",
			h.DB.Model(&model.AlipayProduct{}).Select("id").Where("name LIKE ?", "%"+name+"%"))
	}

	page, limit, offset := response.GetPagination(c)
	var total int64
	query.Count(&total)

	var histories []model.SplitHistory
	query.Preload("AlipayUser").
		Order("create_datetime DESC").
		Offset(offset).Limit(limit).
		Find(&histories)

	response.PageResponse(c, histories, total, page, limit, "")
}

// SplitNativeHistoryStatistics 分账历史统计
// GET /api/alipay/split/history/statistics/
func (h *AlipayNativeHandler) SplitNativeHistoryStatistics(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	query := h.DB.Model(&model.SplitHistory{}).Where("hide = ?", false)

	// 权限过滤
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.DetailResponse(c, gin.H{}, "")
			return
		}
		query = query.Where("alipay_product IN (?)",
			h.DB.Model(&model.AlipayProduct{}).Select("id").
				Where("writeoff_id = ? AND is_delete = ?", writeoff.ID, false))
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.DetailResponse(c, gin.H{}, "")
			return
		}
		query = query.Where("alipay_user IN (?)",
			h.DB.Model(&model.AlipaySplitUser{}).Select("id").
				Where("group_id IN (?)",
					h.DB.Model(&model.AlipaySplitUserGroup{}).Select("id").Where("tenant_id = ?", tenant.ID)))
	}

	var result struct {
		TodayMoney    int64 `gorm:"column:today_money"`
		YesterdayMoney int64 `gorm:"column:yesterday_money"`
		TotalMoney    int64 `gorm:"column:total_money"`
		SuccessCount  int64 `gorm:"column:success_count"`
		WaitCount     int64 `gorm:"column:wait_count"`
		FailCount     int64 `gorm:"column:fail_count"`
	}

	query.Select(`
		COALESCE(SUM(CASE WHEN split_status=1 AND update_datetime >= ? THEN money ELSE 0 END),0) as today_money,
		COALESCE(SUM(CASE WHEN split_status=1 AND update_datetime >= ? AND update_datetime < ? THEN money ELSE 0 END),0) as yesterday_money,
		COALESCE(SUM(CASE WHEN split_status=1 THEN money ELSE 0 END),0) as total_money,
		COALESCE(SUM(CASE WHEN split_status=1 THEN 1 ELSE 0 END),0) as success_count,
		COALESCE(SUM(CASE WHEN split_status=0 THEN 1 ELSE 0 END),0) as wait_count,
		COALESCE(SUM(CASE WHEN split_status=3 THEN 1 ELSE 0 END),0) as fail_count
	`, today, yesterday, today).
		Scan(&result)

	response.DetailResponse(c, result, "")
}

// ===== 分账用户组（原生管理-支付宝） =====

// NativeSplitGroupList 分账用户组列表
// GET /api/alipay/split/group/
func (h *AlipayNativeHandler) NativeSplitGroupList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.AlipaySplitUserGroup{})

	// 权限过滤
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("tenant_id = ?", writeoff.ParentID)
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("tenant_id = ?", tenant.ID)
	default:
		response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
		return
	}

	// 过滤
	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}
	if preStatus := c.Query("pre_status"); preStatus != "" {
		query = query.Where("pre_status = ?", preStatus == "true" || preStatus == "1")
	}
	if tenantFilter := c.Query("tenant"); tenantFilter != "" {
		query = query.Where("tenant_id = ?", tenantFilter)
	}

	page, limit, offset := response.GetPagination(c)
	var total int64
	query.Count(&total)

	var groups []model.AlipaySplitUserGroup
	query.Preload("Tenant").Preload("Writeoff").
		Order("create_datetime DESC").
		Offset(offset).Limit(limit).
		Find(&groups)

	// 附加统计信息
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	type groupItem struct {
		model.AlipaySplitUserGroup
		PrePay        int64 `json:"pre_pay"`
		TodayFlow     int64 `json:"today_flow"`
		YesterdayFlow int64 `json:"yesterday_flow"`
		TotalFlow     int64 `json:"total_flow"`
	}

	items := make([]groupItem, 0, len(groups))
	for _, g := range groups {
		item := groupItem{AlipaySplitUserGroup: g}
		// 预付
		var pre model.AlipaySplitUserGroupPre
		if h.DB.Where("group_id = ?", g.ID).First(&pre).Error == nil {
			item.PrePay = pre.PrePay
		}
		// 流水
		var flow struct {
			TodayFlow     int64 `gorm:"column:today_flow"`
			YesterdayFlow int64 `gorm:"column:yesterday_flow"`
			TotalFlow     int64 `gorm:"column:total_flow"`
		}
		h.DB.Model(&model.AlipaySplitUserFlow{}).
			Where("alipay_user_id IN (?)",
				h.DB.Model(&model.AlipaySplitUser{}).Select("id").Where("group_id = ?", g.ID)).
			Select(`
				COALESCE(SUM(CASE WHEN date = ? THEN flow ELSE 0 END),0) as today_flow,
				COALESCE(SUM(CASE WHEN date = ? THEN flow ELSE 0 END),0) as yesterday_flow,
				COALESCE(SUM(flow),0) as total_flow
			`, today, yesterday).
			Scan(&flow)
		item.TodayFlow = flow.TodayFlow
		item.YesterdayFlow = flow.YesterdayFlow
		item.TotalFlow = flow.TotalFlow
		items = append(items, item)
	}

	response.PageResponse(c, items, total, page, limit, "")
}

// NativeSplitGroupCreate 创建分账用户组
// POST /api/alipay/split/group/
func (h *AlipayNativeHandler) NativeSplitGroupCreate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	var req struct {
		Name        string  `json:"name" binding:"required"`
		Telegram    string  `json:"telegram"`
		PreStatus   bool    `json:"pre_status"`
		Status      bool    `json:"status"`
		TenantID    uint    `json:"tenant_id"`
		Weight      int     `json:"weight"`
		Tax         float64 `json:"tax"`
		WriteoffID  *uint   `json:"writeoff_id"`
		Description string  `json:"description"`
		PrePay      int64   `json:"pre_pay"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	// 自动设置 tenant_id
	switch user.Role.Key {
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.ErrorResponse(c, "租户信息获取失败")
			return
		}
		req.TenantID = tenant.ID
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.ErrorResponse(c, "核销信息获取失败")
			return
		}
		req.TenantID = writeoff.ParentID
	}

	tx := h.DB.Begin()
	group := model.AlipaySplitUserGroup{
		Name:        req.Name,
		Telegram:    req.Telegram,
		PreStatus:   req.PreStatus,
		Status:      req.Status,
		TenantID:    req.TenantID,
		Weight:      req.Weight,
		Tax:         req.Tax,
		WriteoffID:  req.WriteoffID,
		Description: req.Description,
		Creator:     &user.ID,
		Modifier:    &user.ID,
	}
	if err := tx.Create(&group).Error; err != nil {
		tx.Rollback()
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}

	// 创建预付记录
	tx.Create(&model.AlipaySplitUserGroupPre{
		GroupID: group.ID,
		PrePay:  req.PrePay,
	})

	tx.Commit()
	response.DetailResponse(c, group, "创建成功")
}

// NativeSplitGroupRetrieve 获取分账用户组详情
// GET /api/alipay/split/group/:id/
func (h *AlipayNativeHandler) NativeSplitGroupRetrieve(c *gin.Context) {
	id := c.Param("id")
	var group model.AlipaySplitUserGroup
	if err := h.DB.Preload("Tenant").Preload("Writeoff").First(&group, id).Error; err != nil {
		response.ErrorResponse(c, "分账组不存在")
		return
	}

	var pre model.AlipaySplitUserGroupPre
	h.DB.Where("group_id = ?", group.ID).First(&pre)

	response.DetailResponse(c, gin.H{
		"group":   group,
		"pre_pay": pre.PrePay,
	}, "")
}

// NativeSplitGroupUpdate 更新分账用户组
// PUT /api/alipay/split/group/:id/
func (h *AlipayNativeHandler) NativeSplitGroupUpdate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id := c.Param("id")

	var group model.AlipaySplitUserGroup
	if err := h.DB.First(&group, id).Error; err != nil {
		response.ErrorResponse(c, "分账组不存在")
		return
	}

	var raw map[string]interface{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}
	updates := filterAllowedFields(raw, []string{
		"name", "telegram", "pre_status", "status", "weight", "tax",
		"writeoff_id", "description",
	})
	updates["modifier"] = user.ID

	h.DB.Model(&group).Updates(updates)
	response.DetailResponse(c, group, "更新成功")
}

// NativeSplitGroupDelete 删除分账用户组
// DELETE /api/alipay/split/group/:id/
func (h *AlipayNativeHandler) NativeSplitGroupDelete(c *gin.Context) {
	id := c.Param("id")
	var group model.AlipaySplitUserGroup
	if err := h.DB.First(&group, id).Error; err != nil {
		response.ErrorResponse(c, "分账组不存在")
		return
	}
	h.DB.Delete(&group)
	response.DetailResponse(c, nil, "删除成功")
}

// NativeSplitGroupStatistics 分账组统计
// GET /api/alipay/split/group/statistics/
func (h *AlipayNativeHandler) NativeSplitGroupStatistics(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	query := h.DB.Model(&model.AlipaySplitUserGroup{})
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.DetailResponse(c, gin.H{}, "")
			return
		}
		query = query.Where("tenant_id = ?", writeoff.ParentID)
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.DetailResponse(c, gin.H{}, "")
			return
		}
		query = query.Where("tenant_id = ?", tenant.ID)
	}

	var countResult struct {
		OnlineCount int64 `gorm:"column:online_count"`
		TotalCount  int64 `gorm:"column:total_count"`
	}
	query.Select(`
		COALESCE(SUM(CASE WHEN status = 1 THEN 1 ELSE 0 END),0) as online_count,
		COUNT(*) as total_count
	`).Scan(&countResult)

	// 获取所有分账组的ID
	var groupIDs []uint
	query2 := h.DB.Model(&model.AlipaySplitUserGroup{})
	switch user.Role.Key {
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		h.DB.Where("system_user_id = ?", user.ID).First(&writeoff)
		query2 = query2.Where("tenant_id = ?", writeoff.ParentID)
	case model.RoleKeyTenant:
		var tenant model.Tenant
		h.DB.Where("system_user_id = ?", user.ID).First(&tenant)
		query2 = query2.Where("tenant_id = ?", tenant.ID)
	}
	query2.Pluck("id", &groupIDs)

	var flowResult struct {
		TodayFlow     int64 `gorm:"column:today_flow"`
		YesterdayFlow int64 `gorm:"column:yesterday_flow"`
		TotalFlow     int64 `gorm:"column:total_flow"`
	}
	var preResult struct {
		OnlinePrePay  int64 `gorm:"column:online_pre_pay"`
		OfflinePrePay int64 `gorm:"column:offline_pre_pay"`
	}

	if len(groupIDs) > 0 {
		userIDs := h.DB.Model(&model.AlipaySplitUser{}).Select("id").Where("group_id IN ?", groupIDs)
		h.DB.Model(&model.AlipaySplitUserFlow{}).
			Where("alipay_user_id IN (?)", userIDs).
			Select(`
				COALESCE(SUM(CASE WHEN date = ? THEN flow ELSE 0 END),0) as today_flow,
				COALESCE(SUM(CASE WHEN date = ? THEN flow ELSE 0 END),0) as yesterday_flow,
				COALESCE(SUM(flow),0) as total_flow
			`, today, yesterday).
			Scan(&flowResult)

		// 预付统计
		h.DB.Model(&model.AlipaySplitUserGroupPre{}).
			Joins("JOIN "+model.AlipaySplitUserGroup{}.TableName()+" g ON g.id = "+model.AlipaySplitUserGroupPre{}.TableName()+".group_id").
			Where(model.AlipaySplitUserGroupPre{}.TableName()+".group_id IN ?", groupIDs).
			Select(`
				COALESCE(SUM(CASE WHEN g.pre_status = 1 THEN `+model.AlipaySplitUserGroupPre{}.TableName()+`.pre_pay ELSE 0 END),0) as online_pre_pay,
				COALESCE(SUM(CASE WHEN g.pre_status = 0 THEN `+model.AlipaySplitUserGroupPre{}.TableName()+`.pre_pay ELSE 0 END),0) as offline_pre_pay
			`).Scan(&preResult)
	}

	response.DetailResponse(c, gin.H{
		"online_count":    countResult.OnlineCount,
		"total_count":     countResult.TotalCount,
		"online_pre_pay":  preResult.OnlinePrePay,
		"offline_pre_pay": preResult.OfflinePrePay,
		"today_flow":      flowResult.TodayFlow,
		"yesterday_flow":  flowResult.YesterdayFlow,
		"total_flow":      flowResult.TotalFlow,
	}, "")
}

// NativeSplitGroupBindAdd 分账组绑定核销
// POST /api/alipay/split/group/:id/bind/add/
func (h *AlipayNativeHandler) NativeSplitGroupBindAdd(c *gin.Context) {
	id := c.Param("id")
	var group model.AlipaySplitUserGroup
	if err := h.DB.First(&group, id).Error; err != nil {
		response.ErrorResponse(c, "分账组不存在")
		return
	}

	var req struct {
		WriteoffID uint `json:"writeoff"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var writeoff model.WriteOff
	if err := h.DB.Joins("JOIN "+model.Users{}.TableName()+" u ON u.id = "+model.WriteOff{}.TableName()+".system_user_id").
		Where(model.WriteOff{}.TableName()+".id = ? AND "+model.WriteOff{}.TableName()+".parent_id = ? AND u.is_active = ?",
			req.WriteoffID, group.TenantID, true).
		First(&writeoff).Error; err != nil {
		response.ErrorResponse(c, "找不到对应的核销")
		return
	}

	h.DB.Model(&group).Update("writeoff_id", writeoff.ID)
	response.DetailResponse(c, nil, "设置成功")
}

// NativeSplitGroupBindRemove 分账组解绑核销
// POST /api/alipay/split/group/:id/bind/remove/
func (h *AlipayNativeHandler) NativeSplitGroupBindRemove(c *gin.Context) {
	id := c.Param("id")
	var group model.AlipaySplitUserGroup
	if err := h.DB.First(&group, id).Error; err != nil {
		response.ErrorResponse(c, "分账组不存在")
		return
	}

	h.DB.Model(&group).Update("writeoff_id", nil)
	response.DetailResponse(c, nil, "删除成功")
}

// ===== 分账用户（原生管理-支付宝） =====

// NativeSplitUserList 分账用户列表
// GET /api/alipay/split/user/
func (h *AlipayNativeHandler) NativeSplitUserList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.AlipaySplitUser{})

	// 权限过滤
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("group_id IN (?)",
			h.DB.Model(&model.AlipaySplitUserGroup{}).Select("id").Where("tenant_id = ?", writeoff.ParentID))
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where("group_id IN (?)",
			h.DB.Model(&model.AlipaySplitUserGroup{}).Select("id").Where("tenant_id = ?", tenant.ID))
	default:
		response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
		return
	}

	// 过滤
	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}
	if username := c.Query("username"); username != "" {
		query = query.Where("username LIKE ?", "%"+username+"%")
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}
	if group := c.Query("group"); group != "" {
		query = query.Where("group_id = ?", group)
	}
	if groupName := c.Query("group_name"); groupName != "" {
		query = query.Where("group_id IN (?)",
			h.DB.Model(&model.AlipaySplitUserGroup{}).Select("id").Where("name LIKE ?", "%"+groupName+"%"))
	}
	if usernameType := c.Query("username_type"); usernameType != "" {
		query = query.Where("username_type = ?", usernameType)
	}

	page, limit, offset := response.GetPagination(c)
	var total int64
	query.Count(&total)

	var users []model.AlipaySplitUser
	query.Preload("Group").
		Order("create_datetime DESC").
		Offset(offset).Limit(limit).
		Find(&users)

	// 附加流水
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	type userItem struct {
		model.AlipaySplitUser
		TodayFlow     int64 `json:"today_flow"`
		YesterdayFlow int64 `json:"yesterday_flow"`
		TotalFlow     int64 `json:"total_flow"`
	}

	items := make([]userItem, 0, len(users))
	for _, u := range users {
		item := userItem{AlipaySplitUser: u}
		var flow struct {
			TodayFlow     int64 `gorm:"column:today_flow"`
			YesterdayFlow int64 `gorm:"column:yesterday_flow"`
			TotalFlow     int64 `gorm:"column:total_flow"`
		}
		h.DB.Model(&model.AlipaySplitUserFlow{}).
			Where("alipay_user_id = ?", u.ID).
			Select(`
				COALESCE(SUM(CASE WHEN date = ? THEN flow ELSE 0 END),0) as today_flow,
				COALESCE(SUM(CASE WHEN date = ? THEN flow ELSE 0 END),0) as yesterday_flow,
				COALESCE(SUM(flow),0) as total_flow
			`, today, yesterday).
			Scan(&flow)
		item.TodayFlow = flow.TodayFlow
		item.YesterdayFlow = flow.YesterdayFlow
		item.TotalFlow = flow.TotalFlow
		items = append(items, item)
	}

	response.PageResponse(c, items, total, page, limit, "")
}

// NativeSplitUserCreate 创建分账用户
// POST /api/alipay/split/user/
func (h *AlipayNativeHandler) NativeSplitUserCreate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	var req model.AlipaySplitUser
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	req.Creator = &user.ID
	req.Modifier = &user.ID

	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// NativeSplitUserUpdate 更新分账用户
// PUT /api/alipay/split/user/:id/
func (h *AlipayNativeHandler) NativeSplitUserUpdate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id := c.Param("id")

	var splitUser model.AlipaySplitUser
	if err := h.DB.First(&splitUser, id).Error; err != nil {
		response.ErrorResponse(c, "分账用户不存在")
		return
	}

	var raw map[string]interface{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}
	updates := filterAllowedFields(raw, []string{
		"username_type", "username", "name", "status", "limit_money",
		"group_id", "percentage", "risk", "description",
	})
	updates["modifier"] = user.ID

	h.DB.Model(&splitUser).Updates(updates)
	response.DetailResponse(c, splitUser, "更新成功")
}

// NativeSplitUserDelete 删除分账用户
// DELETE /api/alipay/split/user/:id/
func (h *AlipayNativeHandler) NativeSplitUserDelete(c *gin.Context) {
	id := c.Param("id")
	if err := h.DB.Delete(&model.AlipaySplitUser{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ===== 神码管理 =====

// ShenmaList 神码列表
// GET /api/alipay/shenma/
func (h *AlipayNativeHandler) ShenmaList(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.AlipayShenma{}).
		Joins("LEFT JOIN "+model.AlipayProduct{}.TableName()+" ap ON ap.id = "+model.AlipayShenma{}.TableName()+".alipay_id").
		Where("ap.is_delete = ?", false)

	// 权限过滤
	switch user.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
			return
		}
		query = query.Where(model.AlipayShenma{}.TableName()+".tenant_id = ?", tenant.ID)
	default:
		response.PageResponse(c, []interface{}{}, 0, 1, 10, "")
		return
	}

	// 过滤
	if name := c.Query("name"); name != "" {
		query = query.Where("ap.name LIKE ?", "%"+name+"%")
	}
	if uid := c.Query("uid"); uid != "" {
		query = query.Where("ap.uid LIKE ? OR ap.app_id LIKE ?", "%"+uid+"%", "%"+uid+"%")
	}
	if status := c.Query("status"); status != "" {
		query = query.Where(model.AlipayShenma{}.TableName()+".status = ?", status == "true" || status == "1")
	}

	page, limit, offset := response.GetPagination(c)
	var total int64
	query.Count(&total)

	var shenmas []model.AlipayShenma
	query.Preload("Alipay").Preload("Tenant").
		Order(model.AlipayShenma{}.TableName() + ".id DESC").
		Offset(offset).Limit(limit).
		Find(&shenmas)

	response.PageResponse(c, shenmas, total, page, limit, "")
}

// ShenmaCreate 创建神码
// POST /api/alipay/shenma/
func (h *AlipayNativeHandler) ShenmaCreate(c *gin.Context) {
	var req model.AlipayShenma
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// ShenmaUpdate 更新神码
// PUT /api/alipay/shenma/:id/
func (h *AlipayNativeHandler) ShenmaUpdate(c *gin.Context) {
	id := c.Param("id")
	var shenma model.AlipayShenma
	if err := h.DB.First(&shenma, id).Error; err != nil {
		response.ErrorResponse(c, "神码不存在")
		return
	}

	var raw map[string]interface{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}
	updates := filterAllowedFields(raw, []string{"status", "limit_money", "tenant_id"})

	h.DB.Model(&shenma).Updates(updates)
	response.DetailResponse(c, shenma, "更新成功")
}

// ShenmaDelete 删除神码
// DELETE /api/alipay/shenma/:id/
func (h *AlipayNativeHandler) ShenmaDelete(c *gin.Context) {
	id := c.Param("id")
	if err := h.DB.Delete(&model.AlipayShenma{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ShenmaPayChannel 神码可用通道
// GET /api/alipay/shenma/:id/pay_channel/
func (h *AlipayNativeHandler) ShenmaPayChannel(c *gin.Context) {
	id := c.Param("id")
	var shenma model.AlipayShenma
	if err := h.DB.First(&shenma, id).Error; err != nil {
		response.ErrorResponse(c, "神码不存在")
		return
	}

	var product model.AlipayProduct
	if err := h.DB.Preload("AllowPayChannels").First(&product, shenma.AlipayID).Error; err != nil {
		response.ErrorResponse(c, "产品不存在")
		return
	}

	response.DetailResponse(c, product.AllowPayChannels, "")
}
