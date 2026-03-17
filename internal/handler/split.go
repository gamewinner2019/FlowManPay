package handler

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
)

// SplitHandler 分账管理处理器
type SplitHandler struct {
	DB *gorm.DB
}

// NewSplitHandler 创建分账管理处理器
func NewSplitHandler(db *gorm.DB) *SplitHandler {
	return &SplitHandler{DB: db}
}

// ===== 分账用户组 =====

// GroupList 分账用户组列表
// GET /api/split/groups/
func (h *SplitHandler) GroupList(c *gin.Context) {
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

	query := h.DB.Model(&model.AlipaySplitUserGroup{})

	roleKey := user.Role.Key
	if roleKey == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.DetailResponse(c, gin.H{"data": []interface{}{}, "total": 0}, "")
			return
		}
		query = query.Where("tenant_id = ?", tenant.ID)
	}

	// 搜索
	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}

	var total int64
	query.Count(&total)

	var groups []model.AlipaySplitUserGroup
	query.Preload("Tenant").Preload("Writeoff").
		Order("id DESC").Offset(offset).Limit(limit).Find(&groups)

	response.DetailResponse(c, gin.H{"data": groups, "total": total}, "")
}

// GroupCreate 创建分账用户组
// POST /api/split/groups/
func (h *SplitHandler) GroupCreate(c *gin.Context) {
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
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

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
	}

	if err := h.DB.Create(&group).Error; err != nil {
		response.ErrorResponse(c, "创建失败")
		return
	}

	// 创建对应的预付记录
	if req.PreStatus {
		pre := model.AlipaySplitUserGroupPre{
			GroupID: group.ID,
			PrePay:  0,
		}
		h.DB.Create(&pre)
	}

	response.DetailResponse(c, group, "")
}

// GroupRetrieve 获取分账用户组详情
// GET /api/split/groups/:id/
func (h *SplitHandler) GroupRetrieve(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var group model.AlipaySplitUserGroup
	if err := h.DB.Preload("Tenant").Preload("Writeoff").First(&group, id).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}

	response.DetailResponse(c, group, "")
}

// GroupUpdate 更新分账用户组
// PUT /api/split/groups/:id/
func (h *SplitHandler) GroupUpdate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id, _ := strconv.Atoi(c.Param("id"))

	var group model.AlipaySplitUserGroup
	if err := h.DB.First(&group, id).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}

	var req struct {
		Name        *string  `json:"name"`
		Telegram    *string  `json:"telegram"`
		PreStatus   *bool    `json:"pre_status"`
		Status      *bool    `json:"status"`
		Weight      *int     `json:"weight"`
		Tax         *float64 `json:"tax"`
		WriteoffID  *uint    `json:"writeoff_id"`
		Description *string  `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	updates := map[string]interface{}{
		"modifier": user.ID,
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Telegram != nil {
		updates["telegram"] = *req.Telegram
	}
	if req.PreStatus != nil {
		updates["pre_status"] = *req.PreStatus
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Weight != nil {
		updates["weight"] = *req.Weight
	}
	if req.Tax != nil {
		updates["tax"] = *req.Tax
	}
	if req.WriteoffID != nil {
		updates["writeoff_id"] = *req.WriteoffID
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}

	h.DB.Model(&group).Updates(updates)
	h.DB.Preload("Tenant").Preload("Writeoff").First(&group, id)
	response.DetailResponse(c, group, "")
}

// GroupDelete 删除分账用户组
// DELETE /api/split/groups/:id/
func (h *SplitHandler) GroupDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.DB.Delete(&model.AlipaySplitUserGroup{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "")
}

// GroupPrePay 分账组预付操作
// POST /api/split/groups/:id/pre_pay/
func (h *SplitHandler) GroupPrePay(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id, _ := strconv.Atoi(c.Param("id"))

	var req struct {
		ChangeMoney int64  `json:"change_money" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	// 获取或创建预付记录
	var pre model.AlipaySplitUserGroupPre
	if err := h.DB.Where("group_id = ?", id).First(&pre).Error; err != nil {
		pre = model.AlipaySplitUserGroupPre{
			GroupID: uint(id),
			PrePay:  0,
		}
		h.DB.Create(&pre)
	}

	oldMoney := pre.PrePay
	newMoney := oldMoney + req.ChangeMoney

	// 乐观锁更新
	result := h.DB.Model(&pre).Where("version = ?", pre.Version).Updates(map[string]interface{}{
		"pre_pay": newMoney,
		"version": gorm.Expr("version + 1"),
	})
	if result.RowsAffected == 0 {
		response.ErrorResponse(c, "操作冲突，请重试")
		return
	}

	// 创建历史记录
	h.DB.Create(&model.AlipaySplitUserGroupPreHistory{
		GroupID:     uint(id),
		ChangeMoney: req.ChangeMoney,
		OldMoney:    oldMoney,
		NewMoney:    newMoney,
		Description: req.Description,
		Creator:     &user.ID,
	})

	response.DetailResponse(c, gin.H{"old_money": oldMoney, "new_money": newMoney}, "")
}

// GroupPrePayHistory 分账组预付历史
// GET /api/split/groups/:id/pre_pay_history/
func (h *SplitHandler) GroupPrePayHistory(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	var total int64
	h.DB.Model(&model.AlipaySplitUserGroupPreHistory{}).Where("group_id = ?", id).Count(&total)

	var history []model.AlipaySplitUserGroupPreHistory
	h.DB.Where("group_id = ?", id).Order("id DESC").Offset(offset).Limit(limit).Find(&history)

	response.DetailResponse(c, gin.H{"data": history, "total": total}, "")
}

// GroupAddMoney 分账组打款
// POST /api/split/groups/:id/add_money/
func (h *SplitHandler) GroupAddMoney(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var req struct {
		AddMoney int64  `json:"add_money" binding:"required"`
		Date     string `json:"date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	date := time.Now()
	if req.Date != "" {
		parsed, err := time.Parse("2006-01-02", req.Date)
		if err == nil {
			date = parsed
		}
	}

	// 查找或创建打款记录
	var record model.AlipaySplitUserGroupAddMoney
	err := h.DB.Where("group_id = ? AND date = ?", id, date.Format("2006-01-02")).First(&record).Error
	if err != nil {
		record = model.AlipaySplitUserGroupAddMoney{
			GroupID:  uint(id),
			Date:     model.DateTime{Time: date},
			AddMoney: req.AddMoney,
		}
		h.DB.Create(&record)
	} else {
		result := h.DB.Model(&record).Where("version = ?", record.Version).Updates(map[string]interface{}{
			"add_money": gorm.Expr("add_money + ?", req.AddMoney),
			"version":   gorm.Expr("version + 1"),
		})
		if result.RowsAffected == 0 {
			response.ErrorResponse(c, "操作冲突，请重试")
			return
		}
	}

	response.DetailResponse(c, nil, "")
}

// ===== 分账用户 =====

// UserList 分账用户列表
// GET /api/split/users/
func (h *SplitHandler) UserList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	query := h.DB.Model(&model.AlipaySplitUser{})

	if groupID := c.Query("group_id"); groupID != "" {
		query = query.Where("group_id = ?", groupID)
	}
	if name := c.Query("name"); name != "" {
		query = query.Where("name LIKE ?", "%"+name+"%")
	}

	var total int64
	query.Count(&total)

	var users []model.AlipaySplitUser
	query.Preload("Group").Order("id DESC").Offset(offset).Limit(limit).Find(&users)

	response.DetailResponse(c, gin.H{"data": users, "total": total}, "")
}

// UserCreate 创建分账用户
// POST /api/split/users/
func (h *SplitHandler) UserCreate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	var req struct {
		UsernameType int     `json:"username_type"`
		Username     string  `json:"username" binding:"required"`
		Name         string  `json:"name" binding:"required"`
		Status       bool    `json:"status"`
		LimitMoney   int64   `json:"limit_money"`
		GroupID      uint    `json:"group_id" binding:"required"`
		Percentage   float64 `json:"percentage"`
		Risk         int     `json:"risk"`
		Description  string  `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	splitUser := model.AlipaySplitUser{
		UsernameType: req.UsernameType,
		Username:     req.Username,
		Name:         req.Name,
		Status:       req.Status,
		LimitMoney:   req.LimitMoney,
		GroupID:       req.GroupID,
		Percentage:   req.Percentage,
		Risk:         req.Risk,
		Description:  req.Description,
		Creator:      &user.ID,
	}

	if err := h.DB.Create(&splitUser).Error; err != nil {
		response.ErrorResponse(c, "创建失败")
		return
	}

	response.DetailResponse(c, splitUser, "")
}

// UserUpdate 更新分账用户
// PUT /api/split/users/:id/
func (h *SplitHandler) UserUpdate(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	id, _ := strconv.Atoi(c.Param("id"))

	var splitUser model.AlipaySplitUser
	if err := h.DB.First(&splitUser, id).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}

	var req struct {
		UsernameType *int     `json:"username_type"`
		Username     *string  `json:"username"`
		Name         *string  `json:"name"`
		Status       *bool    `json:"status"`
		LimitMoney   *int64   `json:"limit_money"`
		Percentage   *float64 `json:"percentage"`
		Risk         *int     `json:"risk"`
		Description  *string  `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	updates := map[string]interface{}{
		"modifier": user.ID,
	}
	if req.UsernameType != nil {
		updates["username_type"] = *req.UsernameType
	}
	if req.Username != nil {
		updates["username"] = *req.Username
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.LimitMoney != nil {
		updates["limit_money"] = *req.LimitMoney
	}
	if req.Percentage != nil {
		updates["percentage"] = *req.Percentage
	}
	if req.Risk != nil {
		updates["risk"] = *req.Risk
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}

	h.DB.Model(&splitUser).Updates(updates)
	h.DB.Preload("Group").First(&splitUser, id)
	response.DetailResponse(c, splitUser, "")
}

// UserDelete 删除分账用户
// DELETE /api/split/users/:id/
func (h *SplitHandler) UserDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.DB.Delete(&model.AlipaySplitUser{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "")
}

// UserFlowList 分账用户日流水列表
// GET /api/split/users/:id/flow/
func (h *SplitHandler) UserFlowList(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	query := h.DB.Model(&model.AlipaySplitUserFlow{}).Where("alipay_user_id = ?", id)

	if startDate := c.Query("start_date"); startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate := c.Query("end_date"); endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var flows []model.AlipaySplitUserFlow
	query.Order("date DESC").Offset(offset).Limit(limit).Find(&flows)

	response.DetailResponse(c, gin.H{"data": flows, "total": total}, "")
}

// ===== 归集用户 =====

// CollectionUserList 归集用户列表
// GET /api/collection/users/
func (h *SplitHandler) CollectionUserList(c *gin.Context) {
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

	response.DetailResponse(c, gin.H{"data": users, "total": total}, "")
}

// CollectionUserCreate 创建归集用户
// POST /api/collection/users/
func (h *SplitHandler) CollectionUserCreate(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Name     string `json:"name" binding:"required"`
		Remarks  string `json:"remarks"`
		TenantID uint   `json:"tenant_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	collectionUser := model.CollectionUser{
		Username: req.Username,
		Name:     req.Name,
		Remarks:  req.Remarks,
		TenantID: req.TenantID,
	}

	if err := h.DB.Create(&collectionUser).Error; err != nil {
		response.ErrorResponse(c, "创建失败")
		return
	}

	response.DetailResponse(c, collectionUser, "")
}

// CollectionUserUpdate 更新归集用户
// PUT /api/collection/users/:id/
func (h *SplitHandler) CollectionUserUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var collectionUser model.CollectionUser
	if err := h.DB.First(&collectionUser, id).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}

	var req struct {
		Username *string `json:"username"`
		Name     *string `json:"name"`
		Remarks  *string `json:"remarks"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	updates := map[string]interface{}{}
	if req.Username != nil {
		updates["username"] = *req.Username
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Remarks != nil {
		updates["remarks"] = *req.Remarks
	}

	h.DB.Model(&collectionUser).Updates(updates)
	response.DetailResponse(c, collectionUser, "")
}

// CollectionUserDelete 删除归集用户
// DELETE /api/collection/users/:id/
func (h *SplitHandler) CollectionUserDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.DB.Delete(&model.CollectionUser{}, id).Error; err != nil {
		response.ErrorResponse(c, "删除失败")
		return
	}
	response.DetailResponse(c, nil, "")
}

// CollectionFlowList 归集用户日流水列表
// GET /api/collection/users/:id/flow/
func (h *SplitHandler) CollectionFlowList(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	query := h.DB.Model(&model.CollectionDayFlow{}).Where("user_id = ?", id)

	if startDate := c.Query("start_date"); startDate != "" {
		query = query.Where("date >= ?", startDate)
	}
	if endDate := c.Query("end_date"); endDate != "" {
		query = query.Where("date <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var flows []model.CollectionDayFlow
	query.Order("date DESC").Offset(offset).Limit(limit).Find(&flows)

	response.DetailResponse(c, gin.H{"data": flows, "total": total}, "")
}

// SplitHistoryList 分账历史列表
// GET /api/split/history/
func (h *SplitHandler) SplitHistoryList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	query := h.DB.Model(&model.SplitHistory{})

	if orderNo := c.Query("order_no"); orderNo != "" {
		query = query.Where("order_no = ?", orderNo)
	}
	if groupID := c.Query("group_id"); groupID != "" {
		query = query.Where("split_group_id = ?", groupID)
	}
	if startDate := c.Query("start_date"); startDate != "" {
		query = query.Where("create_datetime >= ?", startDate)
	}
	if endDate := c.Query("end_date"); endDate != "" {
		query = query.Where("create_datetime <= ?", endDate+" 23:59:59")
	}

	var total int64
	query.Count(&total)

	var history []model.SplitHistory
	query.Order("id DESC").Offset(offset).Limit(limit).Find(&history)

	response.DetailResponse(c, gin.H{"data": history, "total": total}, "")
}
