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

// MerchantHandler handles merchant CRUD operations
type MerchantHandler struct {
	DB          *gorm.DB
	AuthService *service.AuthService
}

// NewMerchantHandler creates a new MerchantHandler
func NewMerchantHandler(db *gorm.DB, authService *service.AuthService) *MerchantHandler {
	return &MerchantHandler{DB: db, AuthService: authService}
}

// List returns paginated merchant list
// GET /api/agent/merchant/
func (h *MerchantHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)

	currentUser, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.Merchant{}).
		Preload("SystemUser").
		Preload("SystemUser.Role").
		Preload("Parent.SystemUser")

	// 租户只能看自己的商户
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			query = query.Where("parent_id = ?", tenant.ID)
		}
	}

	// 搜索过滤
	if name := c.Query("name"); name != "" {
		query = query.Joins("JOIN "+model.Users{}.TableName()+" su ON su.id = "+model.Merchant{}.TableName()+".system_user_id").
			Where("su.name LIKE ? OR su.username LIKE ?", "%"+name+"%", "%"+name+"%")
	}
	if parentID := c.Query("parent_id"); parentID != "" {
		query = query.Where("parent_id = ?", parentID)
	}

	var total int64
	query.Count(&total)

	var merchants []model.Merchant
	query.Order("create_datetime DESC").Offset(offset).Limit(limit).Find(&merchants)

	var result []gin.H
	for _, m := range merchants {
		item := gin.H{
			"id":              m.ID,
			"system_user_id":  m.SystemUserID,
			"parent_id":       m.ParentID,
			"telegram":        m.Telegram,
			"create_datetime": m.CreateDatetime,
			"update_datetime": m.UpdateDatetime,
		}
		if m.SystemUser.ID > 0 {
			item["username"] = m.SystemUser.Username
			item["name"] = m.SystemUser.Name
			item["status"] = m.SystemUser.Status
			item["key"] = m.SystemUser.Key
		}
		if m.Parent != nil && m.Parent.SystemUser.ID > 0 {
			item["parent_name"] = m.Parent.SystemUser.Name
		}
		result = append(result, item)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// Retrieve returns a single merchant detail
// GET /api/agent/merchant/:id/
func (h *MerchantHandler) Retrieve(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var merchant model.Merchant
	if err := h.DB.Preload("SystemUser").Preload("Parent.SystemUser").First(&merchant, id).Error; err != nil {
		response.ErrorResponse(c, "商户不存在")
		return
	}

	data := gin.H{
		"id":              merchant.ID,
		"system_user_id":  merchant.SystemUserID,
		"parent_id":       merchant.ParentID,
		"telegram":        merchant.Telegram,
		"bot_token":       merchant.BotToken,
		"bot_chat_id":     merchant.BotChatID,
		"create_datetime": merchant.CreateDatetime,
		"update_datetime": merchant.UpdateDatetime,
	}
	if merchant.SystemUser.ID > 0 {
		data["username"] = merchant.SystemUser.Username
		data["name"] = merchant.SystemUser.Name
		data["email"] = merchant.SystemUser.Email
		data["mobile"] = merchant.SystemUser.Mobile
		data["status"] = merchant.SystemUser.Status
		data["key"] = merchant.SystemUser.Key
	}
	if merchant.Parent != nil && merchant.Parent.SystemUser.ID > 0 {
		data["parent_name"] = merchant.Parent.SystemUser.Name
	}

	response.DetailResponse(c, data, "")
}

// Update updates merchant information
// PUT /api/agent/merchant/:id/
func (h *MerchantHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var merchant model.Merchant
	if err := h.DB.First(&merchant, id).Error; err != nil {
		response.ErrorResponse(c, "商户不存在")
		return
	}

	var req struct {
		Telegram  *string `json:"telegram"`
		BotToken  *string `json:"bot_token"`
		BotChatID *string `json:"bot_chat_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)
	updates := make(map[string]interface{})
	if req.Telegram != nil {
		updates["telegram"] = *req.Telegram
	}
	if req.BotToken != nil {
		updates["bot_token"] = *req.BotToken
	}
	if req.BotChatID != nil {
		updates["bot_chat_id"] = *req.BotChatID
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}

	if err := h.DB.Model(&merchant).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}

	response.DetailResponse(c, gin.H{"id": merchant.ID}, "更新成功")
}

// ResetGoogle resets the Google 2FA for a merchant user
// POST /api/agent/merchant/:id/reset_google/
func (h *MerchantHandler) ResetGoogle(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var merchant model.Merchant
	if err := h.DB.First(&merchant, id).Error; err != nil {
		response.ErrorResponse(c, "商户不存在")
		return
	}

	if err := h.AuthService.ResetGoogle(merchant.SystemUserID); err != nil {
		response.ErrorResponse(c, "重置失败")
		return
	}

	response.DetailResponse(c, nil, "重置成功")
}

// SimpleList returns simplified merchant list
// GET /api/agent/merchant/simple_list/
func (h *MerchantHandler) SimpleList(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.Merchant{}).Preload("SystemUser")

	// 租户只能看自己的商户
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			query = query.Where("parent_id = ?", tenant.ID)
		}
	}

	var merchants []model.Merchant
	query.Find(&merchants)

	var result []gin.H
	for _, m := range merchants {
		if m.SystemUser.IsActive {
			result = append(result, gin.H{
				"id":   m.ID,
				"name": m.SystemUser.Name,
			})
		}
	}

	response.DetailResponse(c, result, "")
}
