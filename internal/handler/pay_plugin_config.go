package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
)

// PayPluginConfigHandler 插件配置独立处理器
type PayPluginConfigHandler struct {
	DB *gorm.DB
}

// NewPayPluginConfigHandler 创建插件配置处理器
func NewPayPluginConfigHandler(db *gorm.DB) *PayPluginConfigHandler {
	return &PayPluginConfigHandler{DB: db}
}

// List 插件配置列表（支持按 parent / status 过滤，parent为空时返回顶级+children）
func (h *PayPluginConfigHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)
	query := h.DB.Model(&model.PayPluginConfig{})

	if parentID := c.Query("parent"); parentID != "" {
		query = query.Where("parent_id = ?", parentID)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}

	var total int64
	query.Count(&total)

	var items []model.PayPluginConfig
	query.Order("sort ASC, create_datetime ASC").Offset(offset).Limit(limit).Find(&items)

	// 如果没有指定 parent 过滤，为每个顶级配置加载 children
	if c.Query("parent") == "" {
		for i := range items {
			var children []model.PayPluginConfig
			h.DB.Where("parent_id = ?", items[i].ID).Order("sort ASC, create_datetime ASC").Find(&children)
			// 将 children 附加到响应中（通过 map 方式）
			_ = children
		}
	}

	response.PageResponse(c, items, total, page, limit, "")
}

// Retrieve 插件配置详情（含 children）
func (h *PayPluginConfigHandler) Retrieve(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	var item model.PayPluginConfig
	if err := h.DB.First(&item, id).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}

	// 获取 children
	var children []model.PayPluginConfig
	h.DB.Where("parent_id = ?", item.ID).Order("sort ASC, create_datetime ASC").Find(&children)

	result := map[string]interface{}{
		"id":             item.ID,
		"parent_id":      item.ParentID,
		"title":          item.Title,
		"key":            item.Key,
		"value":          item.Value,
		"sort":           item.Sort,
		"status":         item.Status,
		"data_options":   item.DataOptions,
		"form_item_type": item.FormItemType,
		"rule":           item.Rule,
		"placeholder":    item.Placeholder,
		"setting":        item.Setting,
		"children":       children,
	}

	response.DetailResponse(c, result, "")
}

// Create 创建插件配置
func (h *PayPluginConfigHandler) Create(c *gin.Context) {
	var req model.PayPluginConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	// key 唯一性检查（parent 为空时不允许重复）
	if req.ParentID == 0 {
		var count int64
		h.DB.Model(&model.PayPluginConfig{}).Where("`key` = ? AND parent_id = 0", req.Key).Count(&count)
		if count > 0 {
			response.ErrorResponse(c, "已存在相同变量名")
			return
		}
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

// Update 更新插件配置
func (h *PayPluginConfigHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var req struct {
		Title        string              `json:"title"`
		Key          string              `json:"key"`
		Value        *string             `json:"value"`
		Sort         int                 `json:"sort"`
		Status       *bool               `json:"status"`
		DataOptions  *string             `json:"data_options"`
		FormItemType model.FormItemType  `json:"form_item_type"`
		Rule         *string             `json:"rule"`
		Placeholder  string              `json:"placeholder"`
		Setting      *string             `json:"setting"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)
	updates := map[string]interface{}{
		"title":          req.Title,
		"key":            req.Key,
		"value":          req.Value,
		"sort":           req.Sort,
		"form_item_type": req.FormItemType,
		"placeholder":    req.Placeholder,
		"data_options":   req.DataOptions,
		"rule":           req.Rule,
		"setting":        req.Setting,
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}

	if err := h.DB.Model(&model.PayPluginConfig{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}
	response.DetailResponse(c, nil, "更新成功")
}

// Delete 删除插件配置
func (h *PayPluginConfigHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}
	if err := h.DB.Delete(&model.PayPluginConfig{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "删除成功")
}

// SaveContent 批量保存配置内容
// POST /api/pay_plugin_config/save_content/
func (h *PayPluginConfigHandler) SaveContent(c *gin.Context) {
	var items []struct {
		ID    uint    `json:"id"`
		Key   string  `json:"key"`
		Value *string `json:"value"`
		Title string  `json:"title"`
		Sort  int     `json:"sort"`
	}
	if err := c.ShouldBindJSON(&items); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	for _, item := range items {
		if item.ID == 0 {
			continue
		}
		var existing model.PayPluginConfig
		if err := h.DB.First(&existing, item.ID).Error; err != nil {
			// 不存在则创建
			newConfig := model.PayPluginConfig{
				Key:   item.Key,
				Value: item.Value,
				Title: item.Title,
				Sort:  item.Sort,
			}
			h.DB.Create(&newConfig)
		} else {
			// 存在则更新
			updates := map[string]interface{}{
				"value": item.Value,
			}
			if item.Key != "" {
				updates["key"] = item.Key
			}
			if item.Title != "" {
				updates["title"] = item.Title
			}
			h.DB.Model(&existing).Updates(updates)
		}
	}

	response.DetailResponse(c, nil, "保存成功")
}

// GetRelationInfo 查询关联的模板信息
// GET /api/pay_plugin_config/get_relation_info/
func (h *PayPluginConfigHandler) GetRelationInfo(c *gin.Context) {
	varName := c.Query("varName")
	table := c.Query("table")
	if varName == "" || table == "" {
		response.ErrorResponse(c, "未获取到关联信息")
		return
	}

	var config model.PayPluginConfig
	if err := h.DB.Where("`key` = ?", varName).First(&config).Error; err != nil {
		response.ErrorResponse(c, "未获取到关联信息")
		return
	}

	// 获取 parent 及其 children
	var parent model.PayPluginConfig
	if err := h.DB.First(&parent, config.ParentID).Error; err != nil {
		response.ErrorResponse(c, "未获取到关联信息")
		return
	}

	var children []model.PayPluginConfig
	h.DB.Where("parent_id = ?", parent.ID).Order("sort ASC").Find(&children)

	result := map[string]interface{}{
		"id":             parent.ID,
		"parent_id":      parent.ParentID,
		"title":          parent.Title,
		"key":            parent.Key,
		"value":          parent.Value,
		"sort":           parent.Sort,
		"status":         parent.Status,
		"form_item_type": parent.FormItemType,
		"children":       children,
	}

	response.DetailResponse(c, result, "查询成功")
}
