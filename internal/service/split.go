package service

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"

	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/model"
)

// SplitService 分账用户管理服务
type SplitService struct {
	DB *gorm.DB
}

// NewSplitService 创建分账服务
func NewSplitService(db *gorm.DB) *SplitService {
	return &SplitService{DB: db}
}

// GetRandSplitUser 加权随机选择分账用户
// 使用 -ln(1-U)/weight 优先级算法 (Efraimidis-Spirakis)
func (s *SplitService) GetRandSplitUser(money int, tenantID uint, productID uint) *model.AlipaySplitUser {
	today := time.Now().Format("2006-01-02")

	// 查询符合条件的分账用户
	var users []struct {
		model.AlipaySplitUser
		TodayMoney int64   `gorm:"column:today_money"`
		GroupWeight int     `gorm:"column:group_weight"`
		PreStatus  bool    `gorm:"column:pre_status"`
		PrePay     int64   `gorm:"column:pre_pay"`
		Percentage float64 `gorm:"column:percentage"`
	}

	subQuery := s.DB.Table(model.AlipaySplitUser{}.TableName()+" AS u").
		Select("u.*, COALESCE(SUM(f.flow), 0) AS today_money, g.weight AS group_weight, g.pre_status, COALESCE(p.pre_pay, 0) AS pre_pay, u.percentage").
		Joins("JOIN "+model.AlipaySplitUserGroup{}.TableName()+" AS g ON g.id = u.group_id").
		Joins("LEFT JOIN "+model.AlipaySplitUserFlow{}.TableName()+" AS f ON f.alipay_user_id = u.id AND f.date = ?", today).
		Joins("LEFT JOIN "+model.AlipaySplitUserGroupPre{}.TableName()+" AS p ON p.group_id = g.id").
		Joins("JOIN "+model.AlipaySplitUserGroup{}.TableName()+"_split_alipay_product AS gp ON gp.alipay_split_user_group_id = g.id AND gp.alipay_product_id = ?", productID).
		Where("g.status = ? AND g.tenant_id = ? AND u.status = ?", true, tenantID, true).
		Group("u.id")

	if err := subQuery.Find(&users).Error; err != nil {
		log.Printf("[分账] 查询分账用户失败: %v", err)
		return nil
	}

	// 过滤条件
	var candidates []struct {
		User     model.AlipaySplitUser
		Weight   int
		Priority float64
	}

	for _, u := range users {
		// 预付检查
		if u.PreStatus && u.PrePay < int64(float64(u.Percentage)*float64(money)/100) {
			continue
		}
		// 日限额检查
		if u.LimitMoney > 0 && u.TodayMoney+int64(money) > int64(u.LimitMoney) {
			continue
		}

		weight := u.GroupWeight
		if weight <= 0 {
			weight = 1
		}

		// Efraimidis-Spirakis 加权随机: priority = -ln(1-U) / weight
		r := rand.Float64()
		priority := -math.Log(1.0-r) / float64(weight)

		candidates = append(candidates, struct {
			User     model.AlipaySplitUser
			Weight   int
			Priority float64
		}{
			User:     u.AlipaySplitUser,
			Weight:   weight,
			Priority: priority,
		})
	}

	if len(candidates) == 0 {
		return nil
	}

	// 选择优先级最小的（即权重最大的更可能被选中）
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Priority < best.Priority {
			best = c
		}
	}

	return &best.User
}

// CloseSplitUser 关闭分账用户
func (s *SplitService) CloseSplitUser(userID uint, productName string) {
	s.DB.Model(&model.AlipaySplitUser{}).Where("id = ?", userID).Update("status", false)

	var user model.AlipaySplitUser
	if err := s.DB.Preload("Group").Preload("Group.Tenant").First(&user, userID).Error; err != nil {
		return
	}

	userName := fmt.Sprintf("[%s]%s", user.Username, user.Name)
	message := fmt.Sprintf("未绑定用户,自动关闭用户%s", userName)
	log.Printf("[分账] 主体%s,%s %d", productName, message, userID)

	if user.Group != nil && user.Group.Tenant != nil && user.Group.Tenant.Telegram != "" {
		go GetTelegramService().AlipayUserOfflineForward(productName, message, user.Group.Tenant.Telegram)
	}
}

// UpdateSplitHistory 更新分账历史状态
func (s *SplitService) UpdateSplitHistory(id uint, splitStatus int, errMsg string) bool {
	updates := map[string]interface{}{
		"split_status": splitStatus,
	}
	if errMsg != "" {
		updates["error"] = errMsg
	}

	result := s.DB.Model(&model.SplitHistory{}).
		Where("id = ? AND split_status IN ?", id, []int{0, 3}).
		Updates(updates)
	return result.RowsAffected > 0
}

// SaveSplitPrePay 扣减分账预付（含税率）
func (s *SplitService) SaveSplitPrePay(userID uint, change int) {
	var group model.AlipaySplitUserGroup
	if err := s.DB.Joins("JOIN "+model.AlipaySplitUser{}.TableName()+" AS u ON u.group_id = "+model.AlipaySplitUserGroup{}.TableName()+".id").
		Where("u.id = ?", userID).First(&group).Error; err != nil {
		return
	}

	if group.Tax > 0 {
		change = int(float64(100-int(group.Tax)) * float64(change) / 100)
	}

	s.beforeSaveSplitPrePay(userID, change, &group)
}

// beforeSaveSplitPrePay 实际扣减预付并检查阈值通知
func (s *SplitService) beforeSaveSplitPrePay(userID uint, change int, group *model.AlipaySplitUserGroup) {
	var pre model.AlipaySplitUserGroupPre
	if err := s.DB.Where("group_id = ?", group.ID).First(&pre).Error; err != nil {
		return
	}

	before := pre.PrePay
	result := s.DB.Model(&pre).Update("pre_pay", gorm.Expr("pre_pay - ?", change))
	if result.Error != nil {
		return
	}

	after := before - int64(change)

	// 检查阈值通知
	thresholds := []int64{5000000, 4000000, 3000000, 2000000, 1000000, 300000}
	for _, threshold := range thresholds {
		if before >= threshold && after < threshold && group.PreStatus {
			telegram := group.Telegram
			if telegram != "" {
				go GetTelegramService().CheckSplitPreForward(after, telegram)
			}
			break
		}
	}
}

// SaveSplitUserFlow 保存分账用户日流水
func (s *SplitService) SaveSplitUserFlow(productID uint, userID uint, change int, date time.Time, tenantID uint) {
	dateStr := date.Format("2006-01-02")
	var flow model.AlipaySplitUserFlow
	err := s.DB.Where("alipay_user_id = ? AND alipay_product_id = ? AND date = ?", userID, productID, dateStr).First(&flow).Error
	if err == nil {
		s.DB.Model(&flow).Update("flow", gorm.Expr("flow + ?", change))
	} else {
		s.DB.Create(&model.AlipaySplitUserFlow{
			AlipayUserID:    userID,
			AlipayProductID: productID,
			Flow:            int64(change),
			Date:            date,
			TenantID:        tenantID,
		})
	}
}

// SaveCollectionFlow 回调记录归集
func (s *SplitService) SaveCollectionFlow(username string, change int, date time.Time, name string, remarks string, tenantID uint) {
	var user model.CollectionUser
	err := s.DB.Where("username = ? AND tenant_id = ? AND remarks = ?", username, tenantID, remarks).First(&user).Error
	if err != nil {
		user = model.CollectionUser{
			Username: username,
			TenantID: tenantID,
			Name:     name,
			Remarks:  remarks,
		}
		s.DB.Create(&user)
	}

	s.updateCollectionFlow(user.ID, change, date)
}

// updateCollectionFlow 更新归集日流水
func (s *SplitService) updateCollectionFlow(userID uint, change int, date time.Time) {
	dateStr := date.Format("2006-01-02")
	var flow model.CollectionDayFlow
	err := s.DB.Where("user_id = ? AND date = ?", userID, dateStr).First(&flow).Error
	if err == nil {
		s.DB.Model(&flow).Update("flow", gorm.Expr("flow + ?", change))
	} else {
		s.DB.Create(&model.CollectionDayFlow{
			UserID: userID,
			Flow:   int64(change),
			Date:   date,
		})
	}
}
