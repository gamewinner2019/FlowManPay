package service

import (
	"log"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gamewinner2019/FlowManPay/internal/model"
)

// StatisticsService 数据统计服务
type StatisticsService struct {
	DB *gorm.DB
}

// NewStatisticsService 创建数据统计服务
func NewStatisticsService(db *gorm.DB) *StatisticsService {
	return &StatisticsService{DB: db}
}

// today 获取今天的日期(零点)
func today() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// SubmitDayStatistics 提交订单时更新统计(提交数+1)
func (s *StatisticsService) SubmitDayStatistics(tenantID *uint, merchantID *uint, writeoffID *uint, payChannelID *uint) {
	date := today()

	// 全局统计
	s.upsertDayStatistics(date)

	// 租户统计
	if tenantID != nil {
		s.upsertTenantDayStatistics(date, *tenantID)
	}

	// 商户统计
	if merchantID != nil {
		s.upsertMerchantDayStatistics(date, *merchantID)
	}

	// 核销统计
	if writeoffID != nil {
		s.upsertWriteOffDayStatistics(date, *writeoffID)
	}

	// 通道统计
	if payChannelID != nil {
		s.upsertPayChannelDayStatistics(date, tenantID, merchantID, writeoffID, *payChannelID)
	}
}

// SuccessDayStatistics 订单成功时更新统计(成功数+1, 成功金额+money)
func (s *StatisticsService) SuccessDayStatistics(tenantID *uint, merchantID *uint, writeoffID *uint,
	payChannelID *uint, money int64, tax int64, merchantTax int64, deviceType model.DeviceType) {
	date := today()

	// 全局统计
	s.DB.Model(&model.DayStatistics{}).
		Where("date = ?", date).
		Updates(map[string]interface{}{
			"success_count": gorm.Expr("success_count + 1"),
			"success_money": gorm.Expr("success_money + ?", money),
			"total_tax":     gorm.Expr("total_tax + ?", tax),
		})

	// 租户统计
	if tenantID != nil {
		s.DB.Model(&model.TenantDayStatistics{}).
			Where("date = ? AND tenant_id = ?", date, *tenantID).
			Updates(map[string]interface{}{
				"success_count": gorm.Expr("success_count + 1"),
				"success_money": gorm.Expr("success_money + ?", money),
				"total_tax":     gorm.Expr("total_tax + ?", tax),
			})
	}

	// 商户统计
	if merchantID != nil {
		s.DB.Model(&model.MerchantDayStatistics{}).
			Where("date = ? AND merchant_id = ?", date, *merchantID).
			Updates(map[string]interface{}{
				"success_count": gorm.Expr("success_count + 1"),
				"success_money": gorm.Expr("success_money + ?", money),
				"real_money":    gorm.Expr("real_money + ?", money-merchantTax),
				"total_tax":     gorm.Expr("total_tax + ?", merchantTax),
			})
	}

	// 核销统计
	if writeoffID != nil {
		s.DB.Model(&model.WriteOffDayStatistics{}).
			Where("date = ? AND writeoff_id = ?", date, *writeoffID).
			Updates(map[string]interface{}{
				"success_count": gorm.Expr("success_count + 1"),
				"success_money": gorm.Expr("success_money + ?", money),
				"total_tax":     gorm.Expr("total_tax + ?", tax),
			})
	}

	// 通道统计
	if payChannelID != nil {
		s.DB.Model(&model.PayChannelDayStatistics{}).
			Where("date = ? AND pay_channel_id = ?", date, *payChannelID).
			Updates(map[string]interface{}{
				"success_count": gorm.Expr("success_count + 1"),
				"success_money": gorm.Expr("success_money + ?", money),
				"total_tax":     gorm.Expr("total_tax + ?", tax),
			})
	}

	// 设备类型统计
	s.DeviceDayStatistics(tenantID, merchantID, writeoffID, payChannelID, deviceType)
}

// DeviceDayStatistics 更新设备类型统计
func (s *StatisticsService) DeviceDayStatistics(tenantID *uint, merchantID *uint, writeoffID *uint,
	payChannelID *uint, deviceType model.DeviceType) {
	date := today()
	col := deviceTypeColumn(deviceType)
	if col == "" {
		return
	}

	// 更新全局统计
	s.DB.Model(&model.DayStatistics{}).
		Where("date = ?", date).
		Update(col, gorm.Expr(col+" + 1"))

	// 更新租户统计
	if tenantID != nil {
		s.DB.Model(&model.TenantDayStatistics{}).
			Where("date = ? AND tenant_id = ?", date, *tenantID).
			Update(col, gorm.Expr(col+" + 1"))
	}

	// 更新商户统计
	if merchantID != nil {
		s.DB.Model(&model.MerchantDayStatistics{}).
			Where("date = ? AND merchant_id = ?", date, *merchantID).
			Update(col, gorm.Expr(col+" + 1"))
	}

	// 更新核销统计
	if writeoffID != nil {
		s.DB.Model(&model.WriteOffDayStatistics{}).
			Where("date = ? AND writeoff_id = ?", date, *writeoffID).
			Update(col, gorm.Expr(col+" + 1"))
	}

	// 更新通道统计
	if payChannelID != nil {
		s.DB.Model(&model.PayChannelDayStatistics{}).
			Where("date = ? AND pay_channel_id = ?", date, *payChannelID).
			Update(col, gorm.Expr(col+" + 1"))
	}
}

// deviceTypeColumn 获取设备类型对应的数据库列名
func deviceTypeColumn(dt model.DeviceType) string {
	switch dt {
	case model.DeviceTypeAndroid:
		return "android_count"
	case model.DeviceTypeIOS:
		return "ios_count"
	case model.DeviceTypePC:
		return "pc_count"
	case model.DeviceTypeUnknown:
		return "unknown_count"
	default:
		return "unknown_count"
	}
}

// ===== Upsert 辅助函数 =====

func (s *StatisticsService) upsertDayStatistics(date time.Time) {
	stat := model.DayStatistics{}
	stat.Date = date
	stat.SubmitCount = 1
	if err := s.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "date"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"submit_count": gorm.Expr("submit_count + 1"),
		}),
	}).Create(&stat).Error; err != nil {
		log.Printf("更新全局日统计失败: %v", err)
	}
}

func (s *StatisticsService) upsertTenantDayStatistics(date time.Time, tenantID uint) {
	stat := model.TenantDayStatistics{}
	stat.Date = date
	stat.TenantID = &tenantID
	stat.SubmitCount = 1
	if err := s.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "date"}, {Name: "tenant_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"submit_count": gorm.Expr("submit_count + 1"),
		}),
	}).Create(&stat).Error; err != nil {
		log.Printf("更新租户日统计失败: %v", err)
	}
}

func (s *StatisticsService) upsertMerchantDayStatistics(date time.Time, merchantID uint) {
	stat := model.MerchantDayStatistics{}
	stat.Date = date
	stat.MerchantID = &merchantID
	stat.SubmitCount = 1
	if err := s.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "date"}, {Name: "merchant_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"submit_count": gorm.Expr("submit_count + 1"),
		}),
	}).Create(&stat).Error; err != nil {
		log.Printf("更新商户日统计失败: %v", err)
	}
}

func (s *StatisticsService) upsertWriteOffDayStatistics(date time.Time, writeoffID uint) {
	stat := model.WriteOffDayStatistics{}
	stat.Date = date
	stat.WriteoffID = &writeoffID
	stat.SubmitCount = 1
	if err := s.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "date"}, {Name: "writeoff_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"submit_count": gorm.Expr("submit_count + 1"),
		}),
	}).Create(&stat).Error; err != nil {
		log.Printf("更新核销日统计失败: %v", err)
	}
}

func (s *StatisticsService) upsertPayChannelDayStatistics(date time.Time, tenantID *uint, merchantID *uint, writeoffID *uint, payChannelID uint) {
	stat := model.PayChannelDayStatistics{}
	stat.Date = date
	stat.PayChannelID = &payChannelID
	stat.TenantID = tenantID
	stat.MerchantID = merchantID
	stat.WriteoffID = writeoffID
	stat.SubmitCount = 1
	if err := s.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "date"}, {Name: "pay_channel_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"submit_count": gorm.Expr("submit_count + 1"),
		}),
	}).Create(&stat).Error; err != nil {
		log.Printf("更新通道日统计失败: %v", err)
	}
}
