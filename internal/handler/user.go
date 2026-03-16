package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/sign"
	"github.com/gamewinner2019/FlowManPay/internal/service"
)

// UserHandler handles user CRUD operations
type UserHandler struct {
	DB *gorm.DB
}

// NewUserHandler creates a new UserHandler
func NewUserHandler(db *gorm.DB) *UserHandler {
	return &UserHandler{DB: db}
}

// List returns paginated user list
// GET /api/system/user/
func (h *UserHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)

	currentUser, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.Users{}).Preload("Role").Where("is_active = ?", true)

	// 根据角色过滤可见用户
	if currentUser != nil && currentUser.Role.Key == model.RoleKeyTenant {
		// 租户只能看到自己和下属商户/核销
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
			var merchantUserIDs []uint
			h.DB.Model(&model.Merchant{}).Where("parent_id = ?", tenant.ID).Pluck("system_user_id", &merchantUserIDs)
			var writeoffUserIDs []uint
			h.DB.Model(&model.WriteOff{}).Where("parent_id = ?", tenant.ID).Pluck("system_user_id", &writeoffUserIDs)
			allIDs := append(merchantUserIDs, writeoffUserIDs...)
			allIDs = append(allIDs, currentUser.ID)
			query = query.Where("id IN ?", allIDs)
		}
	}

	// 搜索过滤
	if username := c.Query("username"); username != "" {
		query = query.Where("username LIKE ?", "%"+username+"%")
	}
	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}
	if roleID := c.Query("role"); roleID != "" {
		query = query.Where("role_id = ?", roleID)
	}
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status == "true" || status == "1")
	}

	var total int64
	query.Count(&total)

	var users []model.Users
	query.Order("create_datetime DESC").Offset(offset).Limit(limit).Find(&users)

	// 构建响应数据，隐藏敏感字段
	var result []gin.H
	for _, u := range users {
		item := gin.H{
			"id":              u.ID,
			"username":        u.Username,
			"name":            u.Name,
			"email":           u.Email,
			"mobile":          u.Mobile,
			"avatar":          u.Avatar,
			"gender":          u.Gender,
			"status":          u.Status,
			"create_datetime": u.CreateDatetime,
			"update_datetime": u.UpdateDatetime,
		}
		if u.Role.ID > 0 {
			item["role"] = gin.H{
				"id":   u.Role.ID,
				"name": u.Role.Name,
				"key":  u.Role.Key,
			}
		}
		result = append(result, item)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// Create creates a new user with associated role entity
// POST /api/system/user/
func (h *UserHandler) Create(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Mobile   string `json:"mobile"`
		RoleID   uint   `json:"role" binding:"required"`
		Status   *bool  `json:"status"`
		ParentID *uint  `json:"parent_id"` // 租户ID(商户/核销用)
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)

	// 检查用户名是否已存在
	var count int64
	h.DB.Model(&model.Users{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		response.ErrorResponse(c, "用户名已存在")
		return
	}

	// 获取角色信息
	var role model.Role
	if err := h.DB.First(&role, req.RoleID).Error; err != nil {
		response.ErrorResponse(c, "角色不存在")
		return
	}

	// 密码处理: MD5(明文) → bcrypt
	md5Pwd := sign.MD5Password(req.Password)
	hashedPwd, err := service.HashPassword(md5Pwd)
	if err != nil {
		response.ErrorResponse(c, "密码加密失败")
		return
	}

	status := true
	if req.Status != nil {
		status = *req.Status
	}

	// 生成API Key
	apiKey := uuid.New().String()[:32]

	user := model.Users{
		Username:  req.Username,
		Password:  hashedPwd,
		Name:      req.Name,
		Email:     req.Email,
		Mobile:    req.Mobile,
		RoleID:    req.RoleID,
		Key:       apiKey,
		Status:    status,
		IsActive:  true,
		Gender:    2, // 未知
	}
	if currentUser != nil {
		user.Creator = &currentUser.ID
	}

	// 使用事务创建用户及关联实体
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}

		// 根据角色创建对应实体
		switch role.Key {
		case model.RoleKeyTenant:
			tenant := model.Tenant{
				SystemUserID: user.ID,
				Creator:      user.Creator,
			}
			if err := tx.Create(&tenant).Error; err != nil {
				return err
			}
			// 创建租户预占记录
			if err := tx.Create(&model.TenantTax{TenantID: tenant.ID}).Error; err != nil {
				return err
			}

		case model.RoleKeyMerchant:
			if req.ParentID == nil {
				return fmt.Errorf("商户必须指定所属租户")
			}
			merchant := model.Merchant{
				SystemUserID: user.ID,
				ParentID:     *req.ParentID,
				Creator:      user.Creator,
			}
			if err := tx.Create(&merchant).Error; err != nil {
				return err
			}
			// 创建商户预付记录
			if err := tx.Create(&model.MerchantPre{MerchantID: merchant.ID}).Error; err != nil {
				return err
			}

		case model.RoleKeyWriteoff:
			if req.ParentID == nil {
				return fmt.Errorf("核销必须指定所属租户")
			}
			writeoff := model.WriteOff{
				SystemUserID: user.ID,
				ParentID:     *req.ParentID,
				Creator:      user.Creator,
			}
			if err := tx.Create(&writeoff).Error; err != nil {
				return err
			}
			// 创建核销预占和佣金记录
			if err := tx.Create(&model.WriteoffTax{WriteoffID: writeoff.ID}).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.WriteoffBrokerage{WriteoffID: writeoff.ID}).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.WriteoffPre{WriteoffID: writeoff.ID}).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}

	response.DetailResponse(c, gin.H{"id": user.ID, "username": user.Username}, "创建成功")
}

// Update updates user information
// PUT /api/system/user/:id/
func (h *UserHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var user model.Users
	if err := h.DB.First(&user, id).Error; err != nil {
		response.ErrorResponse(c, "用户不存在")
		return
	}

	var req struct {
		Name   *string `json:"name"`
		Email  *string `json:"email"`
		Mobile *string `json:"mobile"`
		Gender *int    `json:"gender"`
		Status *bool   `json:"status"`
		Avatar *string `json:"avatar"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)

	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Email != nil {
		updates["email"] = *req.Email
	}
	if req.Mobile != nil {
		updates["mobile"] = *req.Mobile
	}
	if req.Gender != nil {
		updates["gender"] = *req.Gender
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Avatar != nil {
		updates["avatar"] = *req.Avatar
	}
	if currentUser != nil {
		updates["modifier"] = currentUser.ID
	}

	if err := h.DB.Model(&user).Updates(updates).Error; err != nil {
		response.ErrorResponse(c, "更新失败")
		return
	}

	response.DetailResponse(c, gin.H{"id": user.ID}, "更新成功")
}

// Delete soft-deletes a user
// DELETE /api/system/user/:id/
func (h *UserHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var user model.Users
	if err := h.DB.First(&user, id).Error; err != nil {
		response.ErrorResponse(c, "用户不存在")
		return
	}

	// 软删除: username加后缀, is_active=false
	var deletedCount int64
	h.DB.Model(&model.Users{}).Where("username LIKE ?", user.Username+"[已删除%").Count(&deletedCount)
	newUsername := fmt.Sprintf("%s[已删除%d]", user.Username, deletedCount+1)

	if err := h.DB.Model(&user).Updates(map[string]interface{}{
		"username":  newUsername,
		"is_active": false,
		"status":    false,
	}).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}

	response.DetailResponse(c, nil, "删除成功")
}

// ChangePassword changes the current user's password
// PUT /api/system/user/change_password/
func (h *UserHandler) ChangePassword(c *gin.Context) {
	currentUser, exists := middleware.GetCurrentUser(c)
	if !exists {
		response.ErrorResponse(c, "未获取到用户信息", 4001)
		return
	}

	var req struct {
		OldPassword string `json:"oldPassword" binding:"required"`
		NewPassword string `json:"newPassword" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	// 验证旧密码
	md5OldPwd := sign.MD5Password(req.OldPassword)
	if !service.CheckPasswordExported(md5OldPwd, currentUser.Password) {
		response.ErrorResponse(c, "旧密码不正确")
		return
	}

	// 设置新密码
	md5NewPwd := sign.MD5Password(req.NewPassword)
	hashedPwd, err := service.HashPassword(md5NewPwd)
	if err != nil {
		response.ErrorResponse(c, "密码加密失败")
		return
	}

	if err := h.DB.Model(currentUser).Update("password", hashedPwd).Error; err != nil {
		response.ErrorResponse(c, "修改密码失败")
		return
	}

	response.DetailResponse(c, nil, "修改密码成功")
}

// ResetPassword resets a user's password (admin only)
// PUT /api/system/user/:id/reset_password/
func (h *UserHandler) ResetPassword(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var req struct {
		NewPassword string `json:"newPassword" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	md5Pwd := sign.MD5Password(req.NewPassword)
	hashedPwd, err := service.HashPassword(md5Pwd)
	if err != nil {
		response.ErrorResponse(c, "密码加密失败")
		return
	}

	if err := h.DB.Model(&model.Users{}).Where("id = ?", id).Update("password", hashedPwd).Error; err != nil {
		response.ErrorResponse(c, "重置密码失败")
		return
	}

	response.DetailResponse(c, nil, "重置密码成功")
}

// LoginAgent generates a token for admin to login as another user
// POST /api/system/user/:id/login_agent/
func (h *UserHandler) LoginAgent(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var user model.Users
	if err := h.DB.Preload("Role").First(&user, id).Error; err != nil {
		response.ErrorResponse(c, "用户不存在")
		return
	}

	if !user.IsActive || !user.Status {
		response.ErrorResponse(c, "用户已被禁用")
		return
	}

	accessToken, err := middleware.GenerateAccessToken(user.ID)
	if err != nil {
		response.ErrorResponse(c, "生成Token失败")
		return
	}
	refreshToken, err := middleware.GenerateRefreshToken(user.ID)
	if err != nil {
		response.ErrorResponse(c, "生成Token失败")
		return
	}

	response.DetailResponse(c, gin.H{
		"access":   accessToken,
		"refresh":  refreshToken,
		"user_id":  user.ID,
		"name":     user.Name,
		"role_key": user.Role.Key,
		"username": user.Username,
	}, "登录成功")
}

// SimpleList returns a simplified user list (id + name)
// GET /api/system/user/simple_list/
func (h *UserHandler) SimpleList(c *gin.Context) {
	var users []model.Users
	query := h.DB.Select("id, name, username").Where("is_active = ?", true)

	if roleKey := c.Query("role_key"); roleKey != "" {
		var role model.Role
		if err := h.DB.Where("`key` = ?", roleKey).First(&role).Error; err == nil {
			query = query.Where("role_id = ?", role.ID)
		}
	}

	query.Find(&users)

	var result []gin.H
	for _, u := range users {
		result = append(result, gin.H{
			"id":       u.ID,
			"name":     u.Name,
			"username": u.Username,
		})
	}

	response.DetailResponse(c, result, "")
}
