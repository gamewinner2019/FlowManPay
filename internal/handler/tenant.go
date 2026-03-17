package handler

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
	"github.com/gamewinner2019/FlowManPay/internal/service"
)

// TenantHandler handles tenant CRUD operations
type TenantHandler struct {
	DB          *gorm.DB
	AuthService *service.AuthService
}

// NewTenantHandler creates a new TenantHandler
func NewTenantHandler(db *gorm.DB, authService *service.AuthService) *TenantHandler {
	return &TenantHandler{DB: db, AuthService: authService}
}

// List returns paginated tenant list
// GET /api/agent/tenant/
func (h *TenantHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)

	query := h.DB.Model(&model.Tenant{}).
		Preload("SystemUser").
		Preload("SystemUser.Role")

	// 搜索过滤
	if name := c.Query("name"); name != "" {
		query = query.Joins("JOIN "+model.Users{}.TableName()+" su ON su.id = "+model.Tenant{}.TableName()+".system_user_id").
			Where("su.name LIKE ? OR su.username LIKE ?", "%"+name+"%", "%"+name+"%")
	}

	var total int64
	query.Count(&total)

	var tenants []model.Tenant
	query.Order("create_datetime DESC").Offset(offset).Limit(limit).Find(&tenants)

	var result []gin.H
	for _, t := range tenants {
		item := gin.H{
			"id":              t.ID,
			"system_user_id":  t.SystemUserID,
			"balance":         t.Balance,
			"trust":           t.Trust,
			"pre_tax":         t.PreTax,
			"polling":         t.Polling,
			"telegram":        t.Telegram,
			"create_datetime": t.CreateDatetime,
			"update_datetime": t.UpdateDatetime,
		}
		if t.SystemUser.ID > 0 {
			item["username"] = t.SystemUser.Username
			item["name"] = t.SystemUser.Name
			item["status"] = t.SystemUser.Status
			item["key"] = t.SystemUser.Key
		}
		result = append(result, item)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// Retrieve returns a single tenant detail
// GET /api/agent/tenant/:id/
func (h *TenantHandler) Retrieve(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var tenant model.Tenant
	if err := h.DB.Preload("SystemUser").Preload("SystemUser.Role").First(&tenant, id).Error; err != nil {
		response.ErrorResponse(c, "租户不存在")
		return
	}

	data := gin.H{
		"id":              tenant.ID,
		"system_user_id":  tenant.SystemUserID,
		"balance":         tenant.Balance,
		"trust":           tenant.Trust,
		"pre_tax":         tenant.PreTax,
		"polling":         tenant.Polling,
		"telegram":        tenant.Telegram,
		"bot_token":       tenant.BotToken,
		"bot_chat_id":     tenant.BotChatID,
		"create_datetime": tenant.CreateDatetime,
		"update_datetime": tenant.UpdateDatetime,
	}
	if tenant.SystemUser.ID > 0 {
		data["username"] = tenant.SystemUser.Username
		data["name"] = tenant.SystemUser.Name
		data["email"] = tenant.SystemUser.Email
		data["mobile"] = tenant.SystemUser.Mobile
		data["status"] = tenant.SystemUser.Status
		data["key"] = tenant.SystemUser.Key
	}

	response.DetailResponse(c, data, "")
}

// Update updates tenant information
// PUT /api/agent/tenant/:id/
func (h *TenantHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var tenant model.Tenant
	if err := h.DB.First(&tenant, id).Error; err != nil {
		response.ErrorResponse(c, "租户不存在")
		return
	}

	var req struct {
		Trust   *bool   `json:"trust"`
		Polling *bool   `json:"polling"`
		Telegram *string `json:"telegram"`
		BotToken *string `json:"bot_token"`
		BotChatID *string `json:"bot_chat_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)
	updates := make(map[string]interface{})
	if req.Trust != nil {
		updates["trust"] = *req.Trust
	}
	if req.Polling != nil {
		updates["polling"] = *req.Polling
	}
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

	if err := h.DB.Model(&tenant).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}

	response.DetailResponse(c, gin.H{"id": tenant.ID}, "更新成功")
}

// ChangeMoney adjusts tenant balance (requires op_pwd or 2FA)
// POST /api/agent/tenant/:id/change_money/
func (h *TenantHandler) ChangeMoney(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var req struct {
		Money      int64  `json:"money" binding:"required"` // 变动金额(分)
		Remark     string `json:"remark"`
		OpPwd      string `json:"op_pwd"`
		GoogleCode string `json:"google_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	currentUser, exists := middleware.GetCurrentUser(c)
	if !exists || currentUser == nil {
		response.ErrorResponse(c, "用户未登录")
		return
	}

	// 验证操作密码或Google 2FA
	if req.OpPwd == "" && req.GoogleCode == "" {
		response.ErrorResponse(c, "请输入操作密码或谷歌验证码")
		return
	}

	if req.GoogleCode != "" {
		if err := h.AuthService.CheckGoogle2FAExported(currentUser.ID, req.GoogleCode); err != nil {
			response.ErrorResponse(c, err.Error())
			return
		}
	} else if req.OpPwd != "" {
		if currentUser.OpPwd == nil || *currentUser.OpPwd == "" {
			response.ErrorResponse(c, "未设置操作密码")
			return
		}
		if !service.CheckPasswordExported(req.OpPwd, *currentUser.OpPwd) {
			response.ErrorResponse(c, "操作密码不正确")
			return
		}
	}

	// 事务处理余额变动
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var tenant model.Tenant
		if err := tx.First(&tenant, id).Error; err != nil {
			return err
		}

		beforeMoney := tenant.Balance
		afterMoney := beforeMoney + req.Money

		result := tx.Model(&tenant).Where("version = ?", tenant.Version).Updates(map[string]interface{}{
			"balance": afterMoney,
			"version": tenant.Version + 1,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("数据已被修改，请重试")
		}

		// 记录流水
		cashFlow := model.TenantCashFlow{
			TenantID:    tenant.ID,
			FlowType:    model.TenantCashFlowAdjust,
			ChangeMoney: req.Money,
			OldMoney:    beforeMoney,
			NewMoney:    afterMoney,
			Creator:     &currentUser.ID,
		}
		return tx.Create(&cashFlow).Error
	})

	if err != nil {
		response.ErrorResponse(c, "调额失败: "+err.Error())
		return
	}

	response.DetailResponse(c, nil, "调额成功")
}

// ResetGoogle resets the Google 2FA for a tenant user
// POST /api/agent/tenant/:id/reset_google/
func (h *TenantHandler) ResetGoogle(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var tenant model.Tenant
	if err := h.DB.First(&tenant, id).Error; err != nil {
		response.ErrorResponse(c, "租户不存在")
		return
	}

	if err := h.AuthService.ResetGoogle(tenant.SystemUserID); err != nil {
		response.ErrorResponse(c, "重置失败")
		return
	}

	response.DetailResponse(c, nil, "重置成功")
}

// CashFlowList returns tenant cash flow records
// GET /api/agent/tenant/:id/cash_flow/
func (h *TenantHandler) CashFlowList(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	page, limit, offset := response.GetPagination(c)

	query := h.DB.Model(&model.TenantCashFlow{}).Where("tenant_id = ?", id)

	var total int64
	query.Count(&total)

	var flows []model.TenantCashFlow
	query.Order("create_datetime DESC").Offset(offset).Limit(limit).Find(&flows)

	response.PageResponse(c, flows, total, page, limit, "")
}

// SimpleList returns simplified tenant list
// GET /api/agent/tenant/simple_list/
func (h *TenantHandler) SimpleList(c *gin.Context) {
	var tenants []model.Tenant
	h.DB.Preload("SystemUser").Find(&tenants)

	var result []gin.H
	for _, t := range tenants {
		if t.SystemUser.IsActive {
			result = append(result, gin.H{
				"id":   t.ID,
				"name": t.SystemUser.Name,
			})
		}
	}

	response.DetailResponse(c, result, "")
}
