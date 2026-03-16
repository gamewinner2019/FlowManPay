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

// WriteOffHandler handles writeoff CRUD operations
type WriteOffHandler struct {
	DB          *gorm.DB
	AuthService *service.AuthService
}

// NewWriteOffHandler creates a new WriteOffHandler
func NewWriteOffHandler(db *gorm.DB, authService *service.AuthService) *WriteOffHandler {
	return &WriteOffHandler{DB: db, AuthService: authService}
}

// List returns paginated writeoff list
// GET /api/agent/writeoff/
func (h *WriteOffHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)

	currentUser, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.WriteOff{}).
		Preload("SystemUser").
		Preload("SystemUser.Role").
		Preload("Parent.SystemUser").
		Preload("ParentWriteoff.SystemUser")

	// 租户只能看自己的核销
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			query = query.Where("parent_id = ?", tenant.ID)
		}
	}

	// 搜索过滤
	if name := c.Query("name"); name != "" {
		query = query.Joins("JOIN "+model.Users{}.TableName()+" su ON su.id = "+model.WriteOff{}.TableName()+".system_user_id").
			Where("su.name LIKE ? OR su.username LIKE ?", "%"+name+"%", "%"+name+"%")
	}
	if parentID := c.Query("parent_id"); parentID != "" {
		query = query.Where("parent_id = ?", parentID)
	}

	var total int64
	query.Count(&total)

	var writeoffs []model.WriteOff
	query.Order("create_datetime DESC").Offset(offset).Limit(limit).Find(&writeoffs)

	var result []gin.H
	for _, w := range writeoffs {
		item := gin.H{
			"id":                 w.ID,
			"system_user_id":     w.SystemUserID,
			"parent_id":          w.ParentID,
			"parent_writeoff_id": w.ParentWriteoffID,
			"balance":            w.Balance,
			"white":              w.White,
			"telegram":           w.Telegram,
			"create_datetime":    w.CreateDatetime,
			"update_datetime":    w.UpdateDatetime,
		}
		if w.SystemUser.ID > 0 {
			item["username"] = w.SystemUser.Username
			item["name"] = w.SystemUser.Name
			item["status"] = w.SystemUser.Status
		}
		if w.Parent != nil && w.Parent.SystemUser.ID > 0 {
			item["parent_name"] = w.Parent.SystemUser.Name
		}
		if w.ParentWriteoff != nil && w.ParentWriteoff.SystemUser.ID > 0 {
			item["parent_writeoff_name"] = w.ParentWriteoff.SystemUser.Name
		}
		result = append(result, item)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// Retrieve returns a single writeoff detail
// GET /api/agent/writeoff/:id/
func (h *WriteOffHandler) Retrieve(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var writeoff model.WriteOff
	if err := h.DB.Preload("SystemUser").Preload("Parent.SystemUser").Preload("ParentWriteoff.SystemUser").First(&writeoff, id).Error; err != nil {
		response.ErrorResponse(c, "核销不存在")
		return
	}

	data := gin.H{
		"id":                 writeoff.ID,
		"system_user_id":     writeoff.SystemUserID,
		"parent_id":          writeoff.ParentID,
		"parent_writeoff_id": writeoff.ParentWriteoffID,
		"balance":            writeoff.Balance,
		"white":              writeoff.White,
		"telegram":           writeoff.Telegram,
		"bot_token":          writeoff.BotToken,
		"bot_chat_id":        writeoff.BotChatID,
		"create_datetime":    writeoff.CreateDatetime,
		"update_datetime":    writeoff.UpdateDatetime,
	}
	if writeoff.SystemUser.ID > 0 {
		data["username"] = writeoff.SystemUser.Username
		data["name"] = writeoff.SystemUser.Name
		data["email"] = writeoff.SystemUser.Email
		data["mobile"] = writeoff.SystemUser.Mobile
		data["status"] = writeoff.SystemUser.Status
	}
	if writeoff.Parent != nil && writeoff.Parent.SystemUser.ID > 0 {
		data["parent_name"] = writeoff.Parent.SystemUser.Name
	}

	// 获取佣金信息
	var brokerage model.WriteoffBrokerage
	if err := h.DB.Where("writeoff_id = ?", writeoff.ID).First(&brokerage).Error; err == nil {
		data["brokerage"] = brokerage.Brokerage
	}

	response.DetailResponse(c, data, "")
}

// Update updates writeoff information
// PUT /api/agent/writeoff/:id/
func (h *WriteOffHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var writeoff model.WriteOff
	if err := h.DB.First(&writeoff, id).Error; err != nil {
		response.ErrorResponse(c, "核销不存在")
		return
	}

	var req struct {
		White             *bool   `json:"white"`
		Telegram          *string `json:"telegram"`
		BotToken          *string `json:"bot_token"`
		BotChatID         *string `json:"bot_chat_id"`
		ParentWriteoffID  *uint   `json:"parent_writeoff_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)
	updates := make(map[string]interface{})
	if req.White != nil {
		updates["white"] = *req.White
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
	if req.ParentWriteoffID != nil {
		updates["parent_writeoff_id"] = *req.ParentWriteoffID
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}

	if err := h.DB.Model(&writeoff).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}

	response.DetailResponse(c, gin.H{"id": writeoff.ID}, "更新成功")
}

// ChangeMoney adjusts writeoff balance
// POST /api/agent/writeoff/:id/change_money/
func (h *WriteOffHandler) ChangeMoney(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var req struct {
		Money      int64  `json:"money" binding:"required"`
		Remark     string `json:"remark"`
		OpPwd      string `json:"op_pwd"`
		GoogleCode string `json:"google_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)

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

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var writeoff model.WriteOff
		if err := tx.First(&writeoff, id).Error; err != nil {
			return err
		}

		beforeMoney := int64(0)
		if writeoff.Balance != nil {
			beforeMoney = *writeoff.Balance
		}
		afterMoney := beforeMoney + req.Money

		result := tx.Model(&writeoff).Where("version = ?", writeoff.Version).Updates(map[string]interface{}{
			"balance": afterMoney,
			"version": writeoff.Version + 1,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("数据已被修改，请重试")
		}

		cashFlow := model.WriteoffCashFlow{
			WriteoffID:  writeoff.ID,
			FlowType:    model.WriteoffCashFlowAdjust,
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

// Transfer transfers balance between writeoffs
// POST /api/agent/writeoff/:id/transfer/
func (h *WriteOffHandler) Transfer(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var req struct {
		ToWriteoffID uint   `json:"to_writeoff_id" binding:"required"`
		Money        int64  `json:"money" binding:"required,gt=0"`
		Remark       string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		// 扣减源核销余额
		var from model.WriteOff
		if err := tx.First(&from, id).Error; err != nil {
			return err
		}

		fromBefore := int64(0)
		if from.Balance != nil {
			fromBefore = *from.Balance
		}
		if fromBefore < req.Money {
			return gorm.ErrInvalidData
		}
		fromAfter := fromBefore - req.Money
		if err := tx.Model(&from).Update("balance", fromAfter).Error; err != nil {
			return err
		}

		// 增加目标核销余额
		var to model.WriteOff
		if err := tx.First(&to, req.ToWriteoffID).Error; err != nil {
			return err
		}
		toBefore := int64(0)
		if to.Balance != nil {
			toBefore = *to.Balance
		}
		toAfter := toBefore + req.Money
		if err := tx.Model(&to).Update("balance", toAfter).Error; err != nil {
			return err
		}

		// 记录双方流水
		if err := tx.Create(&model.WriteoffCashFlow{
			WriteoffID:  from.ID,
			FlowType:    model.WriteoffCashFlowTransfer,
			ChangeMoney: -req.Money,
			OldMoney:    fromBefore,
			NewMoney:    fromAfter,
			Creator:     &currentUser.ID,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.WriteoffCashFlow{
			WriteoffID:  to.ID,
			FlowType:    model.WriteoffCashFlowTransfer,
			ChangeMoney: req.Money,
			OldMoney:    toBefore,
			NewMoney:    toAfter,
			Creator:     &currentUser.ID,
		}).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		if err == gorm.ErrInvalidData {
			response.ErrorResponse(c, "余额不足")
			return
		}
		response.ErrorResponse(c, "转赠失败: "+err.Error())
		return
	}

	response.DetailResponse(c, nil, "转赠成功")
}

// CashFlowList returns writeoff cash flow records
// GET /api/agent/writeoff/:id/cash_flow/
func (h *WriteOffHandler) CashFlowList(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	page, limit, offset := response.GetPagination(c)

	query := h.DB.Model(&model.WriteoffCashFlow{}).Where("writeoff_id = ?", id)

	var total int64
	query.Count(&total)

	var flows []model.WriteoffCashFlow
	query.Order("create_datetime DESC").Offset(offset).Limit(limit).Find(&flows)

	response.PageResponse(c, flows, total, page, limit, "")
}

// ResetGoogle resets the Google 2FA for a writeoff user
// POST /api/agent/writeoff/:id/reset_google/
func (h *WriteOffHandler) ResetGoogle(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var writeoff model.WriteOff
	if err := h.DB.First(&writeoff, id).Error; err != nil {
		response.ErrorResponse(c, "核销不存在")
		return
	}

	if err := h.AuthService.ResetGoogle(writeoff.SystemUserID); err != nil {
		response.ErrorResponse(c, "重置失败")
		return
	}

	response.DetailResponse(c, nil, "重置成功")
}

// SimpleList returns simplified writeoff list
// GET /api/agent/writeoff/simple_list/
func (h *WriteOffHandler) SimpleList(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.WriteOff{}).Preload("SystemUser")

	// 租户只能看自己的核销
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			query = query.Where("parent_id = ?", tenant.ID)
		}
	}

	var writeoffs []model.WriteOff
	query.Find(&writeoffs)

	var result []gin.H
	for _, w := range writeoffs {
		if w.SystemUser.IsActive {
			result = append(result, gin.H{
				"id":   w.ID,
				"name": w.SystemUser.Name,
			})
		}
	}

	response.DetailResponse(c, result, "")
}
