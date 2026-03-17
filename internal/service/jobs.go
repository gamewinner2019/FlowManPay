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

// ===== 未分账检查 =====

// CheckNoSplitHistory 检查前10分钟内未分账的记录，重新加入分账队列
func (s *JobsService) CheckNoSplitHistory() (int, error) {
	tenMinAgo := time.Now().Add(-10 * time.Minute)
	fiveHoursAgo := time.Now().Add(-5 * time.Hour)

	var histories []model.SplitHistory
	err := s.DB.Where("split_status IN ? AND hide = ? AND alipay_product_id IS NOT NULL AND create_datetime >= ? AND create_datetime <= ?",
		[]int{0}, true, fiveHoursAgo, tenMinAgo).
		Order("create_datetime ASC").
		Find(&histories).Error
	if err != nil {
		return 0, fmt.Errorf("查询未分账记录失败: %v", err)
	}

	count := len(histories)
	if count == 0 {
		return 0, nil
	}

	// 将未分账记录重新标记为待分账（split_status=0），触发重试
	for _, h := range histories {
		s.DB.Model(&model.SplitHistory{}).Where("id = ? AND split_status = ?", h.ID, 0).
			Update("split_status", 0) // 保持待分账状态，等待分账任务拾取
		log.Printf("[Jobs] 重新分账: history_id=%d, order_id=%s, product_id=%v", h.ID, h.OrderID, h.AlipayProductID)
	}

	log.Printf("[Jobs] 未分账检查完成，重新加入 %d 条记录", count)
	return count, nil
}

// ===== 订单删除 =====

// DeleteOrder 批量删除4天前的非封禁订单（每批10000条，带间隔防止锁表）
func (s *JobsService) DeleteOrder() (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -4)
	var totalDeleted int64

	for {
		// 查询一批待删除的订单ID
		var ids []string
		s.DB.Model(&model.Order{}).
			Where("create_datetime < ? AND (remarks NOT LIKE ? OR remarks IS NULL OR remarks = '')", cutoff, "%封禁%").
			Limit(10000).
			Pluck("id", &ids)

		if len(ids) == 0 {
			break
		}

		// 先删除关联的 OrderDetail
		s.DB.Where("order_id IN ?", ids).Delete(&model.OrderDetail{})
		// 删除关联的 OrderDeviceDetails
		s.DB.Where("order_id IN ?", ids).Delete(&model.OrderDeviceDetails{})
		// 删除关联的 ReOrder
		s.DB.Where("order_id IN ?", ids).Delete(&model.ReOrder{})

		// 删除订单本身
		result := s.DB.Where("id IN ?", ids).Delete(&model.Order{})
		totalDeleted += result.RowsAffected

		log.Printf("[Jobs] 删除订单批次: %d 条", result.RowsAffected)

		// 间隔500ms防止锁表
		time.Sleep(500 * time.Millisecond)
	}

	log.Printf("[Jobs] 订单删除完成，共删除 %d 条", totalDeleted)
	return totalDeleted, nil
}

// ===== 日志清理 =====

// DeleteLog 批量删除4天前的操作日志（每批10000条，带间隔防止锁表）
func (s *JobsService) DeleteLog() (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -4)
	var totalDeleted int64

	for {
		var ids []uint
		s.DB.Model(&model.OperationLog{}).
			Where("create_datetime < ?", cutoff).
			Limit(10000).
			Pluck("id", &ids)

		if len(ids) == 0 {
			break
		}

		result := s.DB.Where("id IN ?", ids).Delete(&model.OperationLog{})
		totalDeleted += result.RowsAffected

		log.Printf("[Jobs] 删除日志批次: %d 条", result.RowsAffected)

		time.Sleep(500 * time.Millisecond)
	}

	log.Printf("[Jobs] 日志清理完成，共删除 %d 条", totalDeleted)
	return totalDeleted, nil
}

// ===== 自动转账 =====

// AutoTransfer 查找所有开启自动转账的快速转账设置，为每个执行转账任务
func (s *JobsService) AutoTransfer() (int, error) {
	// 从系统配置读取间隔和每秒笔数
	interval := s.getSystemConfigInt("main", "quick_interval", 10)
	orderCountPerSecond := s.getSystemConfigInt("main", "quick_pre_count", 50)

	var transfers []model.AlipayQuickTransfer
	err := s.DB.Preload("Alipay").Preload("Alipay.Writeoff").
		Where("auto = ?", true).
		Find(&transfers).Error
	if err != nil {
		return 0, fmt.Errorf("查询快速转账设置失败: %v", err)
	}

	count := 0
	for _, at := range transfers {
		if at.Alipay == nil || !at.Alipay.Status || at.Alipay.IsDelete {
			continue
		}

		go s.executeQuickTransfer(&at, interval, orderCountPerSecond)
		count++
	}

	log.Printf("[Jobs] 自动转账启动 %d 个任务", count)
	return count, nil
}

// executeQuickTransfer 执行单个快速转账任务
func (s *JobsService) executeQuickTransfer(at *model.AlipayQuickTransfer, interval int, orderCountPerSecond int) {
	if at.Alipay == nil || at.Alipay.Writeoff == nil {
		return
	}
	tenantID := at.Alipay.Writeoff.ParentID

	// 计算总转账数量和总金额
	totalCount := orderCountPerSecond * interval
	oneMoney := at.Money
	if oneMoney <= 0 {
		oneMoney = 4900
	}
	planMoney := int64(totalCount) * oneMoney

	// 获取可分配的转账用户
	users := s.getRandTransferUsers(planMoney, tenantID, *at.AlipayID, totalCount, oneMoney)
	if len(users) == 0 {
		log.Printf("[Jobs] 自动转账: product_id=%d, 无可用转账用户", *at.AlipayID)
		return
	}

	sleep := float64(at.RunInterval) / 1000.0
	if sleep <= 0 {
		sleep = 0.8
	}

	// 分批创建转账历史并执行（每批 orderCountPerSecond 条）
	for i := 0; i < len(users); i += orderCountPerSecond {
		end := i + orderCountPerSecond
		if end > len(users) {
			end = len(users)
		}
		batch := users[i:end]

		for _, h := range batch {
			s.DB.Create(&h)
			log.Printf("[Jobs] 自动转账: 创建转账记录 id=%s, product_id=%v, user=%s, money=%d",
				h.ID, h.AlipayProductID, h.UserUsername, h.Money)
		}

		time.Sleep(time.Duration(sleep*1000) * time.Millisecond)
	}

	log.Printf("[Jobs] 自动转账完成: product_id=%d, 共创建 %d 条转账记录", *at.AlipayID, len(users))
}

// getRandTransferUsers 按权重随机选择转账用户，生成批量转账历史记录
func (s *JobsService) getRandTransferUsers(planMoney int64, tenantID uint, productID uint, totalCount int, oneMoney int64) []model.QuickTransferHistory {
	today := time.Now().Format("2006-01-02")

	// 查询符合条件的分账用户（按组分组）
	type userWithGroup struct {
		model.AlipaySplitUser
		TodayMoney  int64  `gorm:"column:today_money"`
		GroupWeight int    `gorm:"column:group_weight"`
		GroupID     uint   `gorm:"column:group_id"`
		PreStatus   bool   `gorm:"column:pre_status"`
		PrePay      int64  `gorm:"column:pre_pay"`
	}

	var users []userWithGroup
	s.DB.Table(model.AlipaySplitUser{}.TableName()+" AS u").
		Select("u.*, COALESCE(SUM(f.flow), 0) AS today_money, g.weight AS group_weight, u.group_id, g.pre_status, COALESCE(p.pre_pay, 0) AS pre_pay").
		Joins("JOIN "+model.AlipaySplitUserGroup{}.TableName()+" AS g ON g.id = u.group_id").
		Joins("LEFT JOIN "+model.AlipaySplitUserFlow{}.TableName()+" AS f ON f.alipay_user_id = u.id AND f.date = ?", today).
		Joins("LEFT JOIN "+model.AlipaySplitUserGroupPre{}.TableName()+" AS p ON p.group_id = g.id").
		Joins("JOIN "+model.AlipaySplitUserGroup{}.TableName()+"_transfer_alipay_product AS gp ON gp.alipay_split_user_group_id = g.id AND gp.alipay_product_id = ?", productID).
		Where("g.status = ? AND g.tenant_id = ? AND u.status = ?", true, tenantID, true).
		Group("u.id").
		Find(&users)

	if len(users) == 0 {
		return nil
	}

	// 按组聚合
	type groupInfo struct {
		Weight int
		Users  []userWithGroup
	}
	groupMap := make(map[uint]*groupInfo)
	var totalWeight int
	for _, u := range users {
		// 预付检查
		if u.PreStatus && u.PrePay < int64(u.Percentage*float64(oneMoney)/100) {
			continue
		}
		// 日限额检查
		if u.LimitMoney > 0 && u.TodayMoney+oneMoney > int64(u.LimitMoney) {
			continue
		}

		gid := u.GroupID
		if _, ok := groupMap[gid]; !ok {
			groupMap[gid] = &groupInfo{Weight: u.GroupWeight}
			totalWeight += u.GroupWeight
		}
		groupMap[gid].Users = append(groupMap[gid].Users, u)
	}

	if len(groupMap) == 0 || totalWeight == 0 {
		return nil
	}

	// 按权重分配转账数量到各组
	groups := make([]*groupInfo, 0, len(groupMap))
	for _, g := range groupMap {
		groups = append(groups, g)
	}

	var result []model.QuickTransferHistory
	remainCount := totalCount

	for i, g := range groups {
		var weightCount int
		if i == len(groups)-1 {
			weightCount = remainCount
		} else {
			percent := float64(g.Weight) / float64(totalWeight)
			weightCount = int(float64(totalCount) * percent)
			remainCount -= weightCount
		}

		if len(g.Users) == 0 {
			continue
		}

		for j := 0; j < weightCount; j++ {
			// 随机选择用户
			user := g.Users[j%len(g.Users)]

			// 查找产品信息
			var product model.AlipayProduct
			s.DB.First(&product, productID)

			history := model.QuickTransferHistory{
				ID:              model.CreateQuickTransferOrderNo(),
				AlipayProductID: &productID,
				AlipayUserID:    &user.ID,
				AlipayUserGroupID: &user.GroupID,
				Money:           int(oneMoney),
				TransferStatus:  0,
				UID:             product.UID,
				ProductName:     product.Name,
				UserUsername:     user.Username,
				UserUsernameType: user.UsernameType,
				UserName:        user.Name,
				TenantID:        int64(tenantID),
			}

			result = append(result, history)
		}
	}

	return result
}

// getSystemConfigInt 从数据库读取系统配置整数值
func (s *JobsService) getSystemConfigInt(parentKey string, childKey string, defaultVal int) int {
	var parent model.SystemConfig
	if err := s.DB.Where("`key` = ?", parentKey).First(&parent).Error; err != nil {
		return defaultVal
	}
	var child model.SystemConfig
	if err := s.DB.Where("parent_id = ? AND `key` = ?", parent.ID, childKey).First(&child).Error; err != nil {
		return defaultVal
	}
	if child.Value == nil {
		return defaultVal
	}

	// 尝试解析为数字
	val := strings.Trim(*child.Value, "\"")
	var result int
	if _, err := fmt.Sscanf(val, "%d", &result); err != nil {
		return defaultVal
	}
	return result
}
