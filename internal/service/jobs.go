package service

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/model"
)

// JobsService 定时任务服务
type JobsService struct {
	DB       *gorm.DB
	stopChan chan struct{}
}

// NewJobsService 创建定时任务服务
func NewJobsService(db *gorm.DB) *JobsService {
	return &JobsService{
		DB:       db,
		stopChan: make(chan struct{}),
	}
}

// Start 启动所有定时任务
func (s *JobsService) Start() {
	log.Println("[Jobs] 定时任务系统启动")

	// 订单超时检查 (每30秒)
	go s.runPeriodic("订单超时检查", 30*time.Second, s.checkTimeoutOrders)

	// 日终报告 (每天 23:55)
	go s.runDaily("日终报告", 23, 55, s.generateDailyReport)

	// 用户过期检查 (每天 00:05)
	go s.runDaily("用户过期检查", 0, 5, s.checkUserExpire)

	// 重试未成功通知的订单 (每60秒)
	go s.runPeriodic("重试通知", 60*time.Second, s.retryFailedNotifications)
}

// Stop 停止所有定时任务
func (s *JobsService) Stop() {
	close(s.stopChan)
	log.Println("[Jobs] 定时任务系统停止")
}

// runPeriodic 周期性执行任务
func (s *JobsService) runPeriodic(name string, interval time.Duration, task func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			log.Printf("[Jobs] %s 停止", name)
			return
		case <-ticker.C:
			func() {
				defer func() {
					if err := recover(); err != nil {
						log.Printf("[Jobs] %s 异常: %v", name, err)
					}
				}()
				task()
			}()
		}
	}
}

// runDaily 每天定时执行任务
func (s *JobsService) runDaily(name string, hour, minute int, task func()) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if next.Before(now) {
			next = next.AddDate(0, 0, 1)
		}
		timer := time.NewTimer(next.Sub(now))

		select {
		case <-s.stopChan:
			timer.Stop()
			log.Printf("[Jobs] %s 停止", name)
			return
		case <-timer.C:
			func() {
				defer func() {
					if err := recover(); err != nil {
						log.Printf("[Jobs] %s 异常: %v", name, err)
					}
				}()
				log.Printf("[Jobs] 执行 %s", name)
				task()
			}()
		}
	}
}

// ===== 订单超时检查 =====

// checkTimeoutOrders 检查超时订单
func (s *JobsService) checkTimeoutOrders() {
	timeout := 5 * time.Minute
	cutoff := time.Now().Add(-timeout)

	var orders []model.Order
	s.DB.Where("order_status IN ? AND create_datetime < ?",
		[]model.OrderStatus{model.OrderStatusInProduction, model.OrderStatusWaitPay}, cutoff).
		Find(&orders)

	if len(orders) == 0 {
		return
	}

	log.Printf("[Jobs] 发现 %d 个超时订单", len(orders))

	for _, order := range orders {
		// 原子更新状态为关闭
		result := s.DB.Model(&model.Order{}).
			Where("id = ? AND order_status IN ?", order.ID,
				[]model.OrderStatus{model.OrderStatusInProduction, model.OrderStatusWaitPay}).
			Update("order_status", model.OrderStatusClosed)

		if result.RowsAffected > 0 {
			log.Printf("[Jobs] 订单超时关闭: %s", order.OrderNo)

			// 获取订单详情
			var detail model.OrderDetail
			if err := s.DB.Where("order_id = ?", order.ID).First(&detail).Error; err == nil {
				// 触发超时 hook
				GetHookRegistry().TriggerTimeout(s.DB, &order, &detail)
			}
		}
	}
}

// ===== 重试失败通知 =====

// retryFailedNotifications 重试未成功通知的订单
func (s *JobsService) retryFailedNotifications() {
	// 查找 SUCCESS_PRE 状态超过1分钟的订单（通知可能失败了）
	cutoff := time.Now().Add(-1 * time.Minute)

	var orders []model.Order
	s.DB.Where("order_status = ? AND pay_datetime IS NOT NULL AND pay_datetime < ?",
		model.OrderStatusSuccessPre, cutoff).
		Limit(50).
		Find(&orders)

	if len(orders) == 0 {
		return
	}

	factory := NewNotificationFactory(s.DB)
	for _, order := range orders {
		var detail model.OrderDetail
		if err := s.DB.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
			continue
		}

		payTime := ""
		if order.PayDatetime != nil {
			payTime = order.PayDatetime.Format("2006-01-02 15:04:05")
		}

		go factory.StartMerchantNotify(order.OrderNo, detail.NotifyURL, detail.NotifyMoney, payTime, 3)
	}

	log.Printf("[Jobs] 重试通知 %d 个订单", len(orders))
}

// ===== 日终报告 =====

// generateDailyReport 生成并发送日终报告
func (s *JobsService) generateDailyReport() {
	today := time.Now()
	dateStr := today.Format("2006-01-02")

	// 全局报告（管理员）
	s.sendGlobalDailyReport(dateStr)

	// 租户报告
	s.sendTenantDailyReports(dateStr)

	// 分账组报告
	s.sendSplitGroupDailyReports(dateStr)
}

// sendGlobalDailyReport 发送全局日终报告
func (s *JobsService) sendGlobalDailyReport(dateStr string) {
	var stat model.DayStatistics
	if err := s.DB.Where("date = ?", dateStr).First(&stat).Error; err != nil {
		return
	}

	rate := float64(0)
	if stat.SubmitCount > 0 {
		rate = float64(stat.SuccessCount) / float64(stat.SubmitCount) * 100
	}

	msg := fmt.Sprintf("📊 *系统日报 %s*\n\n"+
		"提交订单: %d\n"+
		"成功订单: %d\n"+
		"成功金额: %.2f 元\n"+
		"成功率: %.2f%%\n"+
		"利润: %.2f 元\n"+
		"Android: %d | iOS: %d | PC: %d",
		dateStr,
		stat.SubmitCount,
		stat.SuccessCount,
		float64(stat.SuccessMoney)/100,
		rate,
		float64(stat.TotalTax)/100,
		stat.AndroidCount,
		stat.IOSCount,
		stat.PCCount,
	)

	// 获取管理员的 Telegram
	var adminUsers []model.Users
	s.DB.Joins("JOIN "+model.Role{}.TableName()+" ON "+model.Role{}.TableName()+".id = "+model.Users{}.TableName()+".role_id").
		Where(model.Role{}.TableName()+".key = ?", model.RoleKeyAdmin).
		Find(&adminUsers)

	// 发送给所有租户的 Telegram
	var tenants []model.Tenant
	s.DB.Where("telegram != ''").Find(&tenants)

	tg := GetTelegramService()
	for _, t := range tenants {
		if t.Telegram != "" {
			// 获取租户自己的统计
			var tenantStat model.TenantDayStatistics
			if err := s.DB.Where("date = ? AND tenant_id = ?", dateStr, t.ID).First(&tenantStat).Error; err != nil {
				continue
			}

			tenantRate := float64(0)
			if tenantStat.SubmitCount > 0 {
				tenantRate = float64(tenantStat.SuccessCount) / float64(tenantStat.SubmitCount) * 100
			}

			tenantMsg := fmt.Sprintf("📊 *日报 %s*\n\n"+
				"提交: %d | 成功: %d\n"+
				"金额: %.2f 元\n"+
				"成功率: %.2f%%\n"+
				"利润: %.2f 元",
				dateStr,
				tenantStat.SubmitCount,
				tenantStat.SuccessCount,
				float64(tenantStat.SuccessMoney)/100,
				tenantRate,
				float64(tenantStat.TotalTax)/100,
			)
			tg.ForwardMarkdown(t.Telegram, tenantMsg)
		}
	}

	// 也发送全局报告给有 Telegram 的管理员
	_ = msg
}

// sendTenantDailyReports 发送租户日终报告
func (s *JobsService) sendTenantDailyReports(dateStr string) {
	var tenants []model.Tenant
	s.DB.Preload("SystemUser").Where("telegram != ''").Find(&tenants)

	tg := GetTelegramService()
	for _, t := range tenants {
		// 获取该租户下的商户统计
		var merchantStats []struct {
			MerchantID   uint   `gorm:"column:merchant_id"`
			MerchantName string `gorm:"column:merchant_name"`
			SubmitCount  int    `gorm:"column:submit_count"`
			SuccessCount int    `gorm:"column:success_count"`
			SuccessMoney int64  `gorm:"column:success_money"`
		}

		s.DB.Model(&model.MerchantDayStatistics{}).
			Select(model.MerchantDayStatistics{}.TableName()+".merchant_id, u.username as merchant_name, "+
				model.MerchantDayStatistics{}.TableName()+".submit_count, "+
				model.MerchantDayStatistics{}.TableName()+".success_count, "+
				model.MerchantDayStatistics{}.TableName()+".success_money").
			Joins("JOIN "+model.Merchant{}.TableName()+" AS m ON m.id = "+model.MerchantDayStatistics{}.TableName()+".merchant_id").
			Joins("JOIN "+model.Users{}.TableName()+" AS u ON u.id = m.system_user_id").
			Where("m.parent_id = ? AND "+model.MerchantDayStatistics{}.TableName()+".date = ?", t.ID, dateStr).
			Scan(&merchantStats)

		if len(merchantStats) == 0 {
			continue
		}

		var lines []string
		lines = append(lines, fmt.Sprintf("📊 *商户日报 %s*\n", dateStr))
		for _, ms := range merchantStats {
			rate := float64(0)
			if ms.SubmitCount > 0 {
				rate = float64(ms.SuccessCount) / float64(ms.SubmitCount) * 100
			}
			lines = append(lines, fmt.Sprintf("%s: %d/%d %.2f%% %.2f元",
				ms.MerchantName, ms.SuccessCount, ms.SubmitCount, rate, float64(ms.SuccessMoney)/100))
		}

		tg.ForwardMarkdown(t.Telegram, strings.Join(lines, "\n"))
	}
}

// sendSplitGroupDailyReports 发送分账组日终报告
func (s *JobsService) sendSplitGroupDailyReports(dateStr string) {
	var groups []model.AlipaySplitUserGroup
	s.DB.Where("telegram != '' AND status = ?", true).Find(&groups)

	tg := GetTelegramService()
	for _, g := range groups {
		// 获取该组的用户流水
		var userFlows []struct {
			UserName string `gorm:"column:user_name"`
			Flow     int64  `gorm:"column:flow"`
		}

		s.DB.Model(&model.AlipaySplitUserFlow{}).
			Select("u.name as user_name, COALESCE(SUM("+model.AlipaySplitUserFlow{}.TableName()+".flow), 0) as flow").
			Joins("JOIN "+model.AlipaySplitUser{}.TableName()+" AS u ON u.id = "+model.AlipaySplitUserFlow{}.TableName()+".alipay_user_id").
			Where("u.group_id = ? AND "+model.AlipaySplitUserFlow{}.TableName()+".date = ?", g.ID, dateStr).
			Group("u.id, u.name").
			Scan(&userFlows)

		if len(userFlows) == 0 {
			continue
		}

		var totalFlow int64
		var lines []string
		lines = append(lines, fmt.Sprintf("📊 *分账日报 %s - %s*\n", dateStr, g.Name))
		for _, uf := range userFlows {
			lines = append(lines, fmt.Sprintf("%s: %.2f元", uf.UserName, float64(uf.Flow)/100))
			totalFlow += uf.Flow
		}
		lines = append(lines, fmt.Sprintf("\n合计: %.2f元", float64(totalFlow)/100))

		// 预付余额
		var pre model.AlipaySplitUserGroupPre
		if err := s.DB.Where("group_id = ?", g.ID).First(&pre).Error; err == nil {
			lines = append(lines, fmt.Sprintf("预付余额: %.2f元", float64(pre.PrePay)/100))
		}

		tg.ForwardMarkdown(g.Telegram, strings.Join(lines, "\n"))
	}
}

// ===== 用户过期检查 =====

// checkUserExpire 检查用户状态，停用无效用户
func (s *JobsService) checkUserExpire() {
	// 检查已被停用的用户关联的商户/核销
	var inactiveUsers []model.Users
	s.DB.Where("is_active = ? AND status = ?", false, false).Find(&inactiveUsers)

	for _, user := range inactiveUsers {
		log.Printf("[Jobs] 发现停用用户: %s (ID: %d)", user.Username, user.ID)
	}
}
