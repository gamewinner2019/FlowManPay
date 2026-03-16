package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
	"github.com/gamewinner2019/FlowManPay/internal/service"
)

// ===== PayTypeHandler 支付方式处理器 =====

// PayTypeHandler 支付方式处理器
type PayTypeHandler struct {
	DB *gorm.DB
}

// NewPayTypeHandler 创建支付方式处理器
func NewPayTypeHandler(db *gorm.DB) *PayTypeHandler {
	return &PayTypeHandler{DB: db}
}

// List 支付方式列表
func (h *PayTypeHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.PayType{})

	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}

	var total int64
	query.Count(&total)

	var items []model.PayType
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	response.PageResponse(c, items, total, page, limit, "")
}

// Create 创建支付方式
func (h *PayTypeHandler) Create(c *gin.Context) {
	var req model.PayType
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser != nil {
		req.Creator = &currentUser.ID
	}
	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// Retrieve 支付方式详情
func (h *PayTypeHandler) Retrieve(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var item model.PayType
	if err := h.DB.First(&item, id).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}
	response.DetailResponse(c, item, "")
}

// Update 更新支付方式
func (h *PayTypeHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Key         string `json:"key"`
		Status      *bool  `json:"status"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	updates := map[string]interface{}{
		"name":        req.Name,
		"key":         req.Key,
		"description": req.Description,
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}
	if err := h.DB.Model(&model.PayType{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}
	response.DetailResponse(c, nil, "更新成功")
}

// Delete 删除支付方式
func (h *PayTypeHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	if err := h.DB.Delete(&model.PayType{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ===== PayPluginHandler 支付插件处理器 =====

// PayPluginHandler 支付插件处理器
type PayPluginHandler struct {
	DB *gorm.DB
}

// NewPayPluginHandler 创建支付插件处理器
func NewPayPluginHandler(db *gorm.DB) *PayPluginHandler {
	return &PayPluginHandler{DB: db}
}

// List 支付插件列表
func (h *PayPluginHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.PayPlugin{}).Preload("PayTypes")

	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}

	var total int64
	query.Count(&total)

	var items []model.PayPlugin
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	response.PageResponse(c, items, total, page, limit, "")
}

// Create 创建支付插件
func (h *PayPluginHandler) Create(c *gin.Context) {
	var req model.PayPlugin
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser != nil {
		req.Creator = &currentUser.ID
	}
	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// Retrieve 支付插件详情
func (h *PayPluginHandler) Retrieve(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var item model.PayPlugin
	if err := h.DB.Preload("PayTypes").First(&item, id).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}
	response.DetailResponse(c, item, "")
}

// Update 更新支付插件
func (h *PayPluginHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var req struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		Status        *bool  `json:"status"`
		CanDivide     *bool  `json:"can_divide"`
		CanTransfer   *bool  `json:"can_transfer"`
		SupportDevice int    `json:"support_device"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	updates := map[string]interface{}{
		"name":           req.Name,
		"description":    req.Description,
		"support_device": req.SupportDevice,
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.CanDivide != nil {
		updates["can_divide"] = *req.CanDivide
	}
	if req.CanTransfer != nil {
		updates["can_transfer"] = *req.CanTransfer
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}
	if err := h.DB.Model(&model.PayPlugin{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}
	response.DetailResponse(c, nil, "更新成功")
}

// Delete 删除支付插件
func (h *PayPluginHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	if err := h.DB.Delete(&model.PayPlugin{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ConfigList 插件配置列表
func (h *PayPluginHandler) ConfigList(c *gin.Context) {
	pluginID, _ := strconv.Atoi(c.Param("id"))
	if pluginID == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var configs []model.PayPluginConfig
	h.DB.Where("parent_id = ?", pluginID).Find(&configs)
	response.DetailResponse(c, configs, "")
}

// ConfigUpdate 更新插件配置
func (h *PayPluginHandler) ConfigUpdate(c *gin.Context) {
	pluginID, _ := strconv.Atoi(c.Param("id"))
	if pluginID == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var req struct {
		Key   string  `json:"key" binding:"required"`
		Value *string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	// Upsert配置
	config := model.PayPluginConfig{
		ParentID: uint(pluginID),
		Key:      req.Key,
		Value:    req.Value,
	}
	if err := h.DB.Where("parent_id = ? AND `key` = ?", pluginID, req.Key).
		Assign(model.PayPluginConfig{Value: req.Value}).
		FirstOrCreate(&config).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}
	response.DetailResponse(c, config, "更新成功")
}

// ===== PayChannelHandler 支付通道处理器 =====

// PayChannelHandler 支付通道处理器
type PayChannelHandler struct {
	DB *gorm.DB
}

// NewPayChannelHandler 创建支付通道处理器
func NewPayChannelHandler(db *gorm.DB) *PayChannelHandler {
	return &PayChannelHandler{DB: db}
}

// List 支付通道列表
func (h *PayChannelHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.PayChannel{}).Preload("Plugin")

	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}
	if pluginID := c.Query("plugin_id"); pluginID != "" {
		query = query.Where("plugin_id = ?", pluginID)
	}

	var total int64
	query.Count(&total)

	var items []model.PayChannel
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	response.PageResponse(c, items, total, page, limit, "")
}

// Create 创建支付通道
func (h *PayChannelHandler) Create(c *gin.Context) {
	var req model.PayChannel
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser != nil {
		req.Creator = &currentUser.ID
	}
	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// Retrieve 支付通道详情
func (h *PayChannelHandler) Retrieve(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var item model.PayChannel
	if err := h.DB.Preload("Plugin").First(&item, id).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}
	response.DetailResponse(c, item, "")
}

// Update 更新支付通道
func (h *PayChannelHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var req struct {
		Name          string           `json:"name"`
		Status        *bool            `json:"status"`
		PluginID      uint             `json:"plugin_id"`
		MaxMoney      int              `json:"max_money"`
		MinMoney      int              `json:"min_money"`
		FloatMaxMoney int              `json:"float_max_money"`
		FloatMinMoney int              `json:"float_min_money"`
		Settled       *bool            `json:"settled"`
		Moneys        model.JSONIntSlice `json:"moneys"`
		StartTime     string           `json:"start_time"`
		EndTime       string           `json:"end_time"`
		Logo          string           `json:"logo"`
		Description   string           `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	updates := map[string]interface{}{
		"name":            req.Name,
		"plugin_id":       req.PluginID,
		"max_money":       req.MaxMoney,
		"min_money":       req.MinMoney,
		"float_max_money": req.FloatMaxMoney,
		"float_min_money": req.FloatMinMoney,
		"moneys":          req.Moneys,
		"start_time":      req.StartTime,
		"end_time":        req.EndTime,
		"logo":            req.Logo,
		"description":     req.Description,
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Settled != nil {
		updates["settled"] = *req.Settled
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}
	if err := h.DB.Model(&model.PayChannel{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}
	response.DetailResponse(c, nil, "更新成功")
}

// Delete 删除支付通道
func (h *PayChannelHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	if err := h.DB.Delete(&model.PayChannel{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ===== PayChannelTaxHandler 通道费率处理器 =====

// PayChannelTaxHandler 通道费率处理器
type PayChannelTaxHandler struct {
	DB *gorm.DB
}

// NewPayChannelTaxHandler 创建通道费率处理器
func NewPayChannelTaxHandler(db *gorm.DB) *PayChannelTaxHandler {
	return &PayChannelTaxHandler{DB: db}
}

// List 通道费率列表
func (h *PayChannelTaxHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.PayChannelTax{}).Preload("PayChannel").Preload("Tenant")

	if tenantID := c.Query("tenant_id"); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if channelID := c.Query("pay_channel_id"); channelID != "" {
		query = query.Where("pay_channel_id = ?", channelID)
	}

	var total int64
	query.Count(&total)

	var items []model.PayChannelTax
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	response.PageResponse(c, items, total, page, limit, "")
}

// Create 创建通道费率
func (h *PayChannelTaxHandler) Create(c *gin.Context) {
	var req model.PayChannelTax
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser != nil {
		req.Creator = &currentUser.ID
	}
	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// Update 更新通道费率
func (h *PayChannelTaxHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var req struct {
		Tax    float64 `json:"tax"`
		Status *bool   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	updates := map[string]interface{}{
		"tax": req.Tax,
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}
	if err := h.DB.Model(&model.PayChannelTax{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}
	response.DetailResponse(c, nil, "更新成功")
}

// Delete 删除通道费率
func (h *PayChannelTaxHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	if err := h.DB.Delete(&model.PayChannelTax{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ===== PayDomainHandler 支付域名处理器 =====

// PayDomainHandler 支付域名处理器
type PayDomainHandler struct {
	DB *gorm.DB
}

// NewPayDomainHandler 创建支付域名处理器
func NewPayDomainHandler(db *gorm.DB) *PayDomainHandler {
	return &PayDomainHandler{DB: db}
}

// List 支付域名列表
func (h *PayDomainHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.PayDomain{})

	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}

	var total int64
	query.Count(&total)

	var items []model.PayDomain
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	response.PageResponse(c, items, total, page, limit, "")
}

// Create 创建支付域名
func (h *PayDomainHandler) Create(c *gin.Context) {
	var req model.PayDomain
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser != nil {
		req.Creator = &currentUser.ID
	}
	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// Retrieve 支付域名详情
func (h *PayDomainHandler) Retrieve(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var item model.PayDomain
	if err := h.DB.First(&item, id).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}
	response.DetailResponse(c, item, "")
}

// Update 更新支付域名
func (h *PayDomainHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var req struct {
		URL             string `json:"url"`
		AppID           string `json:"app_id"`
		Status          *bool  `json:"status"`
		PayStatus       *bool  `json:"pay_status"`
		WechatStatus    *bool  `json:"wechat_status"`
		SignType        int    `json:"sign_type"`
		PublicKey       string `json:"public_key"`
		PrivateKey      string `json:"private_key"`
		AppPublicCrt    string `json:"app_public_crt"`
		AlipayPublicCrt string `json:"alipay_public_crt"`
		AlipayRootCrt   string `json:"alipay_root_crt"`
		AuthStatus      *bool  `json:"auth_status"`
		AuthTimeout     int    `json:"auth_timeout"`
		AuthKey         string `json:"auth_key"`
		Description     string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	updates := map[string]interface{}{
		"url":               req.URL,
		"app_id":            req.AppID,
		"sign_type":         req.SignType,
		"public_key":        req.PublicKey,
		"private_key":       req.PrivateKey,
		"app_public_crt":    req.AppPublicCrt,
		"alipay_public_crt": req.AlipayPublicCrt,
		"alipay_root_crt":   req.AlipayRootCrt,
		"auth_timeout":      req.AuthTimeout,
		"auth_key":          req.AuthKey,
		"description":       req.Description,
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.PayStatus != nil {
		updates["pay_status"] = *req.PayStatus
	}
	if req.WechatStatus != nil {
		updates["wechat_status"] = *req.WechatStatus
	}
	if req.AuthStatus != nil {
		updates["auth_status"] = *req.AuthStatus
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}
	if err := h.DB.Model(&model.PayDomain{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}
	response.DetailResponse(c, nil, "更新成功")
}

// Delete 删除支付域名
func (h *PayDomainHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	if err := h.DB.Delete(&model.PayDomain{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ===== MerchantPayChannelHandler 商户通道处理器 =====

// MerchantPayChannelHandler 商户通道处理器
type MerchantPayChannelHandler struct {
	DB *gorm.DB
}

// NewMerchantPayChannelHandler 创建商户通道处理器
func NewMerchantPayChannelHandler(db *gorm.DB) *MerchantPayChannelHandler {
	return &MerchantPayChannelHandler{DB: db}
}

// List 商户通道列表
func (h *MerchantPayChannelHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.MerchantPayChannel{}).Preload("PayChannel").Preload("Merchant").Preload("Merchant.SystemUser")

	if merchantID := c.Query("merchant_id"); merchantID != "" {
		query = query.Where("merchant_id = ?", merchantID)
	}
	if channelID := c.Query("pay_channel_id"); channelID != "" {
		query = query.Where("pay_channel_id = ?", channelID)
	}

	var total int64
	query.Count(&total)

	var items []model.MerchantPayChannel
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	response.PageResponse(c, items, total, page, limit, "")
}

// Create 创建商户通道
func (h *MerchantPayChannelHandler) Create(c *gin.Context) {
	var req model.MerchantPayChannel
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser != nil {
		req.Creator = &currentUser.ID
	}
	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// Update 更新商户通道
func (h *MerchantPayChannelHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var req struct {
		Status *bool   `json:"status"`
		Tax    float64 `json:"tax"`
		Limit  int     `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	updates := map[string]interface{}{
		"tax":       req.Tax,
		"`limit`":   req.Limit,
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}
	if err := h.DB.Model(&model.MerchantPayChannel{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}
	response.DetailResponse(c, nil, "更新成功")
}

// Delete 删除商户通道
func (h *MerchantPayChannelHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	if err := h.DB.Delete(&model.MerchantPayChannel{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ===== BanHandler 黑名单处理器 =====

// BanHandler 黑名单处理器
type BanHandler struct {
	DB *gorm.DB
}

// NewBanHandler 创建黑名单处理器
func NewBanHandler(db *gorm.DB) *BanHandler {
	return &BanHandler{DB: db}
}

// BanUserIDList 封禁用户列表
func (h *BanHandler) BanUserIDList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.BanUserId{}).Preload("Tenant")

	if tenantID := c.Query("tenant_id"); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if userID := c.Query("user_id"); userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	var total int64
	query.Count(&total)

	var items []model.BanUserId
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	response.PageResponse(c, items, total, page, limit, "")
}

// BanUserIDCreate 创建封禁用户
func (h *BanHandler) BanUserIDCreate(c *gin.Context) {
	var req model.BanUserId
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser != nil {
		req.Creator = &currentUser.ID
	}
	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// BanUserIDDelete 删除封禁用户
func (h *BanHandler) BanUserIDDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	if err := h.DB.Delete(&model.BanUserId{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// BanIPList 封禁IP列表
func (h *BanHandler) BanIPList(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.BanIp{}).Preload("Tenant")

	if tenantID := c.Query("tenant_id"); tenantID != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if ip := c.Query("ip_address"); ip != "" {
		query = query.Where("ip_address = ?", ip)
	}

	var total int64
	query.Count(&total)

	var items []model.BanIp
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&items)

	response.PageResponse(c, items, total, page, limit, "")
}

// BanIPCreate 创建封禁IP
func (h *BanHandler) BanIPCreate(c *gin.Context) {
	var req model.BanIp
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser != nil {
		req.Creator = &currentUser.ID
	}
	if err := h.DB.Create(&req).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}
	response.DetailResponse(c, req, "创建成功")
}

// BanIPDelete 删除封禁IP
func (h *BanHandler) BanIPDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	if err := h.DB.Delete(&model.BanIp{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// ===== RechargeHandler USDT充值处理器 =====

// RechargeHandler USDT充值处理器
type RechargeHandler struct {
	DB *gorm.DB
}

// NewRechargeHandler 创建充值处理器
func NewRechargeHandler(db *gorm.DB) *RechargeHandler {
	return &RechargeHandler{DB: db}
}

// List 充值记录列表
func (h *RechargeHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.RechargeHistory{}).Preload("User")

	if userID := c.Query("user_id"); userID != "" {
		query = query.Where("user_id = ?", userID)
	}

	var total int64
	query.Count(&total)

	var items []model.RechargeHistory
	query.Order("create_datetime DESC").Offset(offset).Limit(limit).Find(&items)

	response.PageResponse(c, items, total, page, limit, "")
}

// Create 创建充值记录
func (h *RechargeHandler) Create(c *gin.Context) {
	var req struct {
		UserID         uint   `json:"user_id" binding:"required"`
		ExchangeRates  int    `json:"exchange_rates" binding:"required"` // 汇率(分)
		CNYAmount      int64  `json:"cny_amount" binding:"required"`     // 人民币金额(分)
		USDTAmount     int64  `json:"usdt_amount" binding:"required"`    // USDT金额(分)
		PayHash        string `json:"pay_hash"`
		PaymentAddress string `json:"payment_address"`
		PayeeAddress   string `json:"payee_address"`
		Description    string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)

	rechargeID := model.CreateRechargeNo()
	recharge := &model.RechargeHistory{
		ID:             rechargeID,
		UserID:         &req.UserID,
		ExchangeRates:  req.ExchangeRates,
		CNYAmount:      req.CNYAmount,
		USDTAmount:     req.USDTAmount,
		PayHash:        req.PayHash,
		PaymentAddress: req.PaymentAddress,
		PayeeAddress:   req.PayeeAddress,
		Description:    req.Description,
	}
	if currentUser != nil {
		recharge.Creator = &currentUser.ID
	}

	if err := h.DB.Create(recharge).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}

	// 充值后更新租户余额
	var tenant model.Tenant
	if err := h.DB.Where("system_user_id = ?", req.UserID).First(&tenant).Error; err == nil {
		cashFlowSvc := service.NewCashFlowService(h.DB)
		var creatorID *uint
		if currentUser != nil {
			creatorID = &currentUser.ID
		}
		if err := cashFlowSvc.CreateTenantCashFlow(h.DB, tenant.ID, model.TenantCashFlowRecharge,
			req.CNYAmount, nil, nil, creatorID); err != nil {
			// 流水创建失败不影响充值记录
			_ = err
		}
	}

	response.DetailResponse(c, recharge, "充值成功")
}
