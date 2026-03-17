package service

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gamewinner2019/FlowManPay/internal/model"
)

// formatMoney 格式化金额(分→元)，与 Django format_money 一致
func formatMoney(amount int64) string {
	yuan := float64(amount) / 100.0
	return fmt.Sprintf("%.2f", yuan)
}

// ===== 预付余额计算 =====

// GetMerchantPre 计算商户截止到 today 的剩余预付余额
// 逻辑: 当前预付 + today之后的real_money消耗(需加回) - today之后的预付充值(需减去)
func (s *JobsService) GetMerchantPre(merchantID uint, today time.Time) int64 {
	var merchantPre model.MerchantPre
	if err := s.DB.Where("merchant_id = ?", merchantID).First(&merchantPre).Error; err != nil {
		return 0
	}

	tomorrow := time.Date(today.Year(), today.Month(), today.Day()+1, 0, 0, 0, 0, today.Location())

	// today之后消耗的real_money（需要加回来还原到today时的值）
	var afterRealMoney struct {
		Total int64 `gorm:"column:un_real"`
	}
	s.DB.Model(&model.MerchantDayStatistics{}).
		Where("merchant_id = ? AND date > ?", merchantID, today).
		Select("COALESCE(SUM(real_money), 0) as un_real").
		Scan(&afterRealMoney)

	// today之后充值的预付（需要减去还原到today时的值）
	var afterPreHistory struct {
		Total int64 `gorm:"column:un_real"`
	}
	s.DB.Model(&model.MerchantPreHistory{}).
		Where("merchant_id = ? AND create_datetime > ?", merchantID, tomorrow).
		Select("COALESCE(SUM(pre_pay), 0) as un_real").
		Scan(&afterPreHistory)

	return merchantPre.PrePay + afterRealMoney.Total - afterPreHistory.Total
}

// GetWriteoffPre 计算核销截止到 today 的剩余预付余额
func (s *JobsService) GetWriteoffPre(writeoffID uint, today time.Time) int64 {
	var writeoffPre model.WriteoffPre
	if err := s.DB.Where("writeoff_id = ?", writeoffID).First(&writeoffPre).Error; err != nil {
		return 0
	}

	tomorrow := time.Date(today.Year(), today.Month(), today.Day()+1, 0, 0, 0, 0, today.Location())

	// today之后消耗的real_money
	var afterRealMoney struct {
		Total int64 `gorm:"column:un_real"`
	}
	s.DB.Model(&model.WriteOffDayStatistics{}).
		Where("writeoff_id = ? AND date > ?", writeoffID, today).
		Select("COALESCE(SUM(success_money), 0) as un_real").
		Scan(&afterRealMoney)

	// today之后充值的预付
	var afterPreHistory struct {
		Total int64 `gorm:"column:un_real"`
	}
	s.DB.Model(&model.WriteoffPreHistory{}).
		Where("writeoff_id = ? AND create_datetime > ?", writeoffID, tomorrow).
		Select("COALESCE(SUM(pre_pay), 0) as un_real").
		Scan(&afterPreHistory)

	return writeoffPre.PrePay + afterRealMoney.Total - afterPreHistory.Total
}

// ===== 日报函数 =====

// ReportTenantPreJob 租户商户日终数据统计
func (s *JobsService) ReportTenantPreJob(tenant *model.Tenant, today time.Time) {
	dateStr := today.Format("01月02日")
	msg := dateStr + "终商户数据统计\n\n"

	// 查询有预付的商户列表
	type merchantInfo struct {
		MerchantID   uint   `gorm:"column:merchant_id"`
		MerchantName string `gorm:"column:merchant_name"`
	}
	var merchantIds []merchantInfo
	s.DB.Table(model.Merchant{}.TableName()+" AS m").
		Select("m.id as merchant_id, u.name as merchant_name").
		Joins("JOIN "+model.Users{}.TableName()+" AS u ON u.id = m.system_user_id").
		Joins("JOIN "+model.MerchantPre{}.TableName()+" AS mp ON mp.merchant_id = m.id").
		Where("u.is_active = ? AND m.parent_id = ? AND mp.pre_pay > 0", true, tenant.ID).
		Scan(&merchantIds)

	// 查询日统计数据
	type reportData struct {
		MerchantID   uint   `gorm:"column:merchant_id"`
		MerchantName string `gorm:"column:merchant_name"`
		SuccessMoney int64  `gorm:"column:success_money"`
		SuccessCount int    `gorm:"column:success_count"`
		SubmitCount  int    `gorm:"column:submit_count"`
		RealMoney    int64  `gorm:"column:real_money"`
		PrePay       int64  `gorm:"column:pre_pay"`
	}
	var data []reportData
	s.DB.Table(model.MerchantDayStatistics{}.TableName()+" AS mds").
		Select("mds.merchant_id, u.name as merchant_name, mds.success_money, mds.success_count, mds.submit_count, mds.real_money, COALESCE(mp.pre_pay, 0) as pre_pay").
		Joins("JOIN "+model.Merchant{}.TableName()+" AS m ON m.id = mds.merchant_id").
		Joins("JOIN "+model.Users{}.TableName()+" AS u ON u.id = m.system_user_id").
		Joins("LEFT JOIN "+model.MerchantPre{}.TableName()+" AS mp ON mp.merchant_id = mds.merchant_id").
		Where("m.parent_id = ? AND mds.date = ?", tenant.ID, today.Format("2006-01-02")).
		Scan(&data)

	if len(data) == 0 && len(merchantIds) == 0 {
		log.Printf("[Jobs] %d %s 通知租户商户暂无数据", tenant.ID, tenant.SystemUser.Name)
		return
	}

	var totalSuccessMoney int64
	var totalProfit int64
	var totalPreMoney int64

	msg += "\n| 商户名称 | 跑量 | 成功率 | 商户收入 | 利润 | 剩余预付 |\n"
	msg += "| ------ | ----- | ----- | ----- | ----- | ----- |\n"

	dataMerchantIDs := make(map[uint]bool)
	for _, d := range data {
		percentage := float64(0)
		if d.SubmitCount > 0 {
			percentage = float64(d.SuccessCount) / float64(d.SubmitCount) * 100
		}
		profit := d.SuccessMoney - d.RealMoney
		prePay := s.GetMerchantPre(d.MerchantID, today)

		msg += fmt.Sprintf("| [%d]%s | %s | %.2f%% | %s | %s | %s |\n",
			d.MerchantID, d.MerchantName,
			formatMoney(d.SuccessMoney),
			percentage,
			formatMoney(d.RealMoney),
			formatMoney(profit),
			formatMoney(prePay))

		totalSuccessMoney += d.SuccessMoney
		totalProfit += profit
		totalPreMoney += prePay
		dataMerchantIDs[d.MerchantID] = true
	}

	// 补充有预付但无统计数据的商户
	for _, m := range merchantIds {
		if !dataMerchantIDs[m.MerchantID] {
			prePay := s.GetMerchantPre(m.MerchantID, today)
			msg += fmt.Sprintf("| [%d]%s | 0.00 | 0.00%% | 0.00 | 0.00 | %s |\n",
				m.MerchantID, m.MerchantName, formatMoney(prePay))
			totalPreMoney += prePay
		}
	}

	msg += fmt.Sprintf("\n- 总跑量: %s\n- 总利润: %s\n- 总剩预付: %s\n",
		formatMoney(totalSuccessMoney), formatMoney(totalProfit), formatMoney(totalPreMoney))

	tg := GetTelegramService()
	ok := tg.ForwardBot(map[string]interface{}{
		"is_md2pic": true,
		"forwards":  msg,
		"chat_id":   tenant.Telegram,
	})
	if ok {
		log.Printf("[Jobs] %d %s 通知租户商户数据成功", tenant.ID, tenant.SystemUser.Name)
	} else {
		log.Printf("[Jobs] %d %s 通知租户商户数据失败", tenant.ID, tenant.SystemUser.Name)
	}
}

// ReportTenantPre 批量发送所有租户的商户日终报告
func (s *JobsService) ReportTenantPre() {
	today := time.Now().AddDate(0, 0, -1)
	var tenants []model.Tenant
	s.DB.Preload("SystemUser").Where("telegram IS NOT NULL AND telegram != ''").
		Joins("JOIN "+model.Users{}.TableName()+" ON "+model.Users{}.TableName()+".id = "+model.Tenant{}.TableName()+".system_user_id").
		Where(model.Users{}.TableName()+".is_active = ?", true).
		Find(&tenants)

	for _, tenant := range tenants {
		t := tenant // 避免闭包引用
		go s.ReportTenantPreJob(&t, today)
	}
}

// ReportSplitPreJob 租户归集日终数据统计
func (s *JobsService) ReportSplitPreJob(tenant *model.Tenant, today time.Time) {
	dateStr := today.Format("01月02日")
	msg := dateStr + "终归集数据统计\n\n"

	// 查询该租户下的分账用户组
	type groupData struct {
		ID            uint   `gorm:"column:id"`
		Name          string `gorm:"column:name"`
		PrePay        int64  `gorm:"column:pre_pay"`
		YesterdayFlow int64  `gorm:"column:yesterday_flow"`
		TotalFlow     int64  `gorm:"column:total_flow"`
	}

	var groups []model.AlipaySplitUserGroup
	s.DB.Where("tenant_id = ?", tenant.ID).Find(&groups)

	if len(groups) == 0 {
		log.Printf("[Jobs] %d %s 通知租户归集暂无数据", tenant.ID, tenant.SystemUser.Name)
		return
	}

	var results []groupData
	yesterday := today
	for _, g := range groups {
		var ydFlow struct {
			Flow int64 `gorm:"column:flow"`
		}
		s.DB.Model(&model.AlipaySplitUserFlow{}).
			Select("COALESCE(SUM(flow), 0) as flow").
			Joins("JOIN "+model.AlipaySplitUser{}.TableName()+" AS u ON u.id = "+model.AlipaySplitUserFlow{}.TableName()+".alipay_user_id").
			Where("u.group_id = ? AND "+model.AlipaySplitUserFlow{}.TableName()+".date = ?", g.ID, yesterday.Format("2006-01-02")).
			Scan(&ydFlow)

		var totalFlow struct {
			Flow int64 `gorm:"column:flow"`
		}
		s.DB.Model(&model.AlipaySplitUserFlow{}).
			Select("COALESCE(SUM(flow), 0) as flow").
			Joins("JOIN "+model.AlipaySplitUser{}.TableName()+" AS u ON u.id = "+model.AlipaySplitUserFlow{}.TableName()+".alipay_user_id").
			Where("u.group_id = ?", g.ID).
			Scan(&totalFlow)

		var pre model.AlipaySplitUserGroupPre
		prePay := int64(0)
		if err := s.DB.Where("group_id = ?", g.ID).First(&pre).Error; err == nil {
			prePay = pre.PrePay
		}

		results = append(results, groupData{
			ID:            g.ID,
			Name:          g.Name,
			PrePay:        prePay,
			YesterdayFlow: ydFlow.Flow,
			TotalFlow:     totalFlow.Flow,
		})
	}

	if len(results) == 0 {
		log.Printf("[Jobs] %d %s 通知租户归集暂无数据", tenant.ID, tenant.SystemUser.Name)
		return
	}

	var totalPreMoney int64

	msg += "\n| 群组名称 | 今日归集 | 总归集 | 剩余预付 |\n"
	msg += "| ------ | ----- | ----- | -----  |\n"

	for _, r := range results {
		msg += fmt.Sprintf("| %s | %s | %s | %s |\n",
			r.Name, formatMoney(r.YesterdayFlow), formatMoney(r.TotalFlow), formatMoney(r.PrePay))
		totalPreMoney += r.PrePay
	}

	msg += fmt.Sprintf("\n- 总剩预付: %s\n", formatMoney(totalPreMoney))

	tg := GetTelegramService()
	ok := tg.ForwardBot(map[string]interface{}{
		"is_md2pic": true,
		"forwards":  msg,
		"chat_id":   tenant.Telegram,
	})
	if ok {
		log.Printf("[Jobs] %d %s 通知租户归集成功", tenant.ID, tenant.SystemUser.Name)
	} else {
		log.Printf("[Jobs] %d %s 通知租户归集失败", tenant.ID, tenant.SystemUser.Name)
	}
}

// ReportSplitPre 批量发送所有租户的归集日终报告
func (s *JobsService) ReportSplitPre() {
	today := time.Now().AddDate(0, 0, -1)
	var tenants []model.Tenant
	s.DB.Preload("SystemUser").Where("telegram IS NOT NULL AND telegram != ''").
		Joins("JOIN "+model.Users{}.TableName()+" ON "+model.Users{}.TableName()+".id = "+model.Tenant{}.TableName()+".system_user_id").
		Where(model.Users{}.TableName()+".is_active = ?", true).
		Find(&tenants)

	for _, tenant := range tenants {
		t := tenant
		go s.ReportSplitPreJob(&t, today)
	}
}

// ReportMerchantPreJob 商户日终数据统计（按通道细分）
func (s *JobsService) ReportMerchantPreJob(today time.Time, merchantID *uint) {
	type channelData struct {
		PayChannelID uint    `gorm:"column:pay_channel_id"`
		MerchantID   uint    `gorm:"column:merchant_id"`
		SuccessMoney int64   `gorm:"column:success_money"`
		RealMoney    int64   `gorm:"column:real_money"`
		SuccessCount int     `gorm:"column:success_count"`
		SubmitCount  int     `gorm:"column:submit_count"`
		Fee          float64 `gorm:"column:fee"`
	}

	query := s.DB.Table(model.PayChannelDayStatistics{}.TableName()+" AS pds").
		Select("pds.pay_channel_id, pds.merchant_id, "+
			"COALESCE(SUM(pds.success_money), 0) as success_money, "+
			"COALESCE(SUM(pds.real_money), 0) as real_money, "+
			"COALESCE(SUM(pds.success_count), 0) as success_count, "+
			"COALESCE(SUM(pds.submit_count), 0) as submit_count, "+
			"COALESCE(mpc.tax, 0) as fee").
		Joins("JOIN "+model.Merchant{}.TableName()+" AS m ON m.id = pds.merchant_id").
		Joins("LEFT JOIN "+model.MerchantPayChannel{}.TableName()+" AS mpc ON mpc.merchant_id = pds.merchant_id AND mpc.pay_channel_id = pds.pay_channel_id").
		Where("m.telegram IS NOT NULL AND m.telegram != '' AND pds.date = ?", today.Format("2006-01-02")).
		Group("pds.merchant_id, pds.pay_channel_id").
		Order("pds.merchant_id, pds.pay_channel_id")

	if merchantID != nil {
		query = query.Where("pds.merchant_id = ?", *merchantID)
	}

	var data []channelData
	query.Scan(&data)

	if len(data) == 0 {
		log.Printf("[Jobs] 通知商户暂无数据")
		return
	}

	// 按 merchant_id 分组
	grouped := make(map[uint][]channelData)
	var groupOrder []uint
	for _, d := range data {
		if _, exists := grouped[d.MerchantID]; !exists {
			groupOrder = append(groupOrder, d.MerchantID)
		}
		grouped[d.MerchantID] = append(grouped[d.MerchantID], d)
	}

	dateStr := today.Format("01月02日")
	for _, mid := range groupOrder {
		merchantData := grouped[mid]
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[Jobs] 商户报告异常: merchant_id=%d, err=%v", mid, r)
				}
			}()

			prePay := s.GetMerchantPre(mid, today)
			if len(merchantData) == 0 && prePay <= 0 {
				return
			}

			msg := dateStr + "终商户数据统计\n\n"
			msg += "\n| 通道ID |    跑量    |    商户进账    | 成功率 | 费率 |手续费    | 剩余预付 |\n"
			msg += "| ----- | ----- | ----- | ----- | ----- | ----- | -----|\n"

			for _, cd := range merchantData {
				percentage := float64(0)
				if cd.SubmitCount > 0 {
					percentage = float64(cd.SuccessCount) / float64(cd.SubmitCount) * 100
				}
				profit := cd.SuccessMoney - cd.RealMoney
				feeStr := fmt.Sprintf("%.2f%%", cd.Fee)

				msg += fmt.Sprintf(" | %d | %s | %s | %.2f%% |  %s | %s |   |\n",
					cd.PayChannelID,
					formatMoney(cd.SuccessMoney),
					formatMoney(cd.RealMoney),
					percentage,
					feeStr,
					formatMoney(profit))
			}

			msg += fmt.Sprintf("|  |  |  |  |  |  | %s |", formatMoney(prePay))

			// 获取商户 telegram
			var merchant model.Merchant
			if err := s.DB.First(&merchant, mid).Error; err != nil {
				log.Printf("[Jobs] 查找商户失败: merchant_id=%d", mid)
				return
			}

			tg := GetTelegramService()
			ok := tg.ForwardBot(map[string]interface{}{
				"is_md2pic": true,
				"forwards":  msg,
				"chat_id":   merchant.Telegram,
			})
			if ok {
				log.Printf("[Jobs] %d 通知商户数据成功", mid)
			} else {
				log.Printf("[Jobs] %d 通知商户数据失败", mid)
			}

			time.Sleep(500 * time.Millisecond)
		}()
	}
}

// ReportMerchantPre 批量发送所有商户的日终报告
func (s *JobsService) ReportMerchantPre() {
	today := time.Now().AddDate(0, 0, -1)
	s.ReportMerchantPreJob(today, nil)
}

// ReportWriteoffPreJob 核销日终数据统计
func (s *JobsService) ReportWriteoffPreJob(tenantID uint, tenantName string, tenantTelegram string, today time.Time) {
	dateStr := today.Format("01月02日")
	msg := dateStr + "终核销数据统计\n\n"

	// 查询该租户下的核销
	var writeoffs []model.WriteOff
	s.DB.Preload("SystemUser").
		Where("parent_id = ?", tenantID).
		Joins("JOIN "+model.Users{}.TableName()+" ON "+model.Users{}.TableName()+".id = "+model.WriteOff{}.TableName()+".system_user_id").
		Where(model.Users{}.TableName()+".is_active = ?", true).
		Order(model.WriteOff{}.TableName() + ".id").
		Find(&writeoffs)

	type writeoffReport struct {
		WriteoffID     uint
		WriteoffName   string
		SuccessCount   int
		SubmitCount    int
		SuccessMoney   int64
		Percentage     float64
		PercentageMsg  string
		SuccessMoneyMsg string
		SuccessTax     int64
		SuccessTaxMsg  string
		Brokerage      int64
		BrokerageMsg   string
	}

	var reportData []writeoffReport
	todayStr := today.Format("2006-01-02")
	tomorrow := today.AddDate(0, 0, 1)

	for _, wo := range writeoffs {
		// 核销日统计
		var stats struct {
			SuccessCount int   `gorm:"column:success_count"`
			SubmitCount  int   `gorm:"column:submit_count"`
			SuccessMoney int64 `gorm:"column:success_money"`
		}
		s.DB.Model(&model.WriteOffDayStatistics{}).
			Where("writeoff_id = ? AND date = ?", wo.ID, todayStr).
			Select("COALESCE(SUM(success_count), 0) as success_count, COALESCE(SUM(submit_count), 0) as submit_count, COALESCE(SUM(success_money), 0) as success_money").
			Scan(&stats)

		percentage := float64(0)
		if stats.SubmitCount > 0 {
			percentage = float64(stats.SuccessCount) / float64(stats.SubmitCount) * 100
			percentage = float64(int(percentage*100)) / 100 // 保留2位小数
		}

		// 核销通道手续费
		var taxResult struct {
			SuccessTax int64 `gorm:"column:success_tax"`
		}
		s.DB.Model(&model.WriteOffChannelDayStatistics{}).
			Where("writeoff_id = ? AND date = ?", wo.ID, todayStr).
			Select("COALESCE(SUM(total_tax), 0) as success_tax").
			Scan(&taxResult)

		// 佣金（flow_type=7 的流水）
		var brokerageResult struct {
			Brokerage int64 `gorm:"column:brokerage"`
		}
		s.DB.Model(&model.WriteoffCashFlow{}).
			Where("writeoff_id = ? AND create_datetime >= ? AND create_datetime < ? AND flow_type = ?",
				wo.ID, todayStr, tomorrow.Format("2006-01-02"), model.WriteoffCashFlowBrokerage).
			Select("COALESCE(SUM(change_money), 0) as brokerage").
			Scan(&brokerageResult)

		if stats.SubmitCount == 0 && brokerageResult.Brokerage == 0 {
			continue
		}

		reportData = append(reportData, writeoffReport{
			WriteoffID:      wo.ID,
			WriteoffName:    wo.SystemUser.Name,
			SuccessCount:    stats.SuccessCount,
			SubmitCount:     stats.SubmitCount,
			SuccessMoney:    stats.SuccessMoney,
			Percentage:      percentage,
			PercentageMsg:   fmt.Sprintf("%.2f%%", percentage),
			SuccessMoneyMsg: formatMoney(stats.SuccessMoney),
			SuccessTax:      taxResult.SuccessTax,
			SuccessTaxMsg:   formatMoney(taxResult.SuccessTax),
			Brokerage:       brokerageResult.Brokerage,
			BrokerageMsg:    formatMoney(brokerageResult.Brokerage),
		})
	}

	if len(reportData) == 0 {
		log.Printf("[Jobs] %d %s 通知租户核销暂无数据", tenantID, tenantName)
		return
	}

	var totalSuccessMoney int64
	var totalProfit int64

	msg += "\n| 核销名称 | 跑量 | 成功率 | 手续费 | 佣金  |\n"
	msg += "| ------ | ----- | ----- | ----- | ----- |\n"

	for _, r := range reportData {
		msg += fmt.Sprintf("| [%d]%s | %s | %s | %s | %s |\n",
			r.WriteoffID, r.WriteoffName,
			r.SuccessMoneyMsg, r.PercentageMsg,
			r.SuccessTaxMsg, r.BrokerageMsg)
		totalSuccessMoney += r.SuccessMoney
		totalProfit += r.SuccessTax
	}

	msg += fmt.Sprintf("\n- 总跑量: %s\n- 总手续费: %s\n",
		formatMoney(totalSuccessMoney), formatMoney(totalProfit))

	tg := GetTelegramService()
	ok := tg.ForwardBot(map[string]interface{}{
		"is_md2pic": true,
		"forwards":  msg,
		"chat_id":   tenantTelegram,
	})
	if ok {
		log.Printf("[Jobs] %d %s 通知租户核销数据成功", tenantID, tenantName)
	} else {
		log.Printf("[Jobs] %d %s 通知租户核销数据失败", tenantID, tenantName)
	}
}

// ReportWriteoffPre 批量发送所有租户的核销日终报告
func (s *JobsService) ReportWriteoffPre() {
	today := time.Now().AddDate(0, 0, -1)
	var tenants []model.Tenant
	s.DB.Preload("SystemUser").Where("telegram IS NOT NULL AND telegram != ''").
		Joins("JOIN "+model.Users{}.TableName()+" ON "+model.Users{}.TableName()+".id = "+model.Tenant{}.TableName()+".system_user_id").
		Where(model.Users{}.TableName()+".is_active = ?", true).
		Find(&tenants)

	for _, tenant := range tenants {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[Jobs] 核销报告异常: tenant_id=%d, err=%v", tenant.ID, r)
				}
			}()
			s.ReportWriteoffPreJob(tenant.ID, tenant.SystemUser.Name, tenant.Telegram, today)
		}()
	}
}

// ===== 用户登录检查 =====

// CheckUserLogin 检查两天内未登录/未进账的用户，自动关闭
func (s *JobsService) CheckUserLogin() (int, error) {
	now := time.Now()
	threeDaysAgo := now.AddDate(0, 0, -3)
	twoDaysAgo := now.AddDate(0, 0, -2)

	// 查询所有活跃用户
	var users []model.Users
	s.DB.Preload("Role").Where("is_active = ? AND status = ?", true, true).Find(&users)

	type expireInfo struct {
		UserID uint
		RoleID string // role key: merchant/tenant/writeoff
		EntityID uint
		Msg    string
	}

	var expireList []expireInfo
	// 存储 entity_id -> msg 的映射（用于汇总通知）
	resMsgs := make(map[string]string)

	for _, user := range users {
		var entityID uint
		var orderTime, flowTime string

		switch user.Role.Key {
		case model.RoleKeyMerchant:
			// 查商户
			var merchant model.Merchant
			if err := s.DB.Where("system_user_id = ?", user.ID).First(&merchant).Error; err != nil {
				continue
			}
			entityID = merchant.ID

			// 3天内有订单则跳过
			var recentOrder int64
			s.DB.Model(&model.Order{}).Where("merchant_id = ? AND create_datetime >= ?", merchant.ID, threeDaysAgo).Count(&recentOrder)
			if recentOrder > 0 {
				continue
			}

			// 最后进单时间
			var lastOrder model.Order
			if err := s.DB.Where("merchant_id = ?", merchant.ID).Order("create_datetime DESC").First(&lastOrder).Error; err == nil {
				orderTime = lastOrder.CreateDatetime.Format("2006-01-02 15:04:05")
			}

		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := s.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
				continue
			}
			entityID = tenant.ID

			var recentOrder int64
			s.DB.Model(&model.Order{}).
				Joins("JOIN "+model.Merchant{}.TableName()+" ON "+model.Merchant{}.TableName()+".id = "+model.Order{}.TableName()+".merchant_id").
				Where(model.Merchant{}.TableName()+".parent_id = ? AND "+model.Order{}.TableName()+".create_datetime >= ?", tenant.ID, threeDaysAgo).
				Count(&recentOrder)
			if recentOrder > 0 {
				continue
			}

			var lastOrder model.Order
			if err := s.DB.Joins("JOIN "+model.Merchant{}.TableName()+" ON "+model.Merchant{}.TableName()+".id = "+model.Order{}.TableName()+".merchant_id").
				Where(model.Merchant{}.TableName()+".parent_id = ?", tenant.ID).
				Order(model.Order{}.TableName() + ".create_datetime DESC").First(&lastOrder).Error; err == nil {
				orderTime = lastOrder.CreateDatetime.Format("2006-01-02 15:04:05")
			}

		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := s.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
				continue
			}
			entityID = writeoff.ID

			var recentFlow int64
			s.DB.Model(&model.WriteoffCashFlow{}).Where("writeoff_id = ? AND create_datetime >= ?", writeoff.ID, threeDaysAgo).Count(&recentFlow)
			if recentFlow > 0 {
				continue
			}

			var lastFlow model.WriteoffCashFlow
			if err := s.DB.Where("writeoff_id = ?", writeoff.ID).Order("create_datetime DESC").First(&lastFlow).Error; err == nil {
				flowTime = lastFlow.CreateDatetime.Format("2006-01-02 15:04:05")
			}

		default:
			continue
		}

		// 检查最后登录时间
		if user.LastLogin == nil || user.LastLogin.After(twoDaysAgo) {
			continue
		}

		lastLogin := ""
		if user.LastLogin != nil {
			lastLogin = user.LastLogin.Format("2006-01-02 15:04:05")
		}

		msgLine := fmt.Sprintf("[%s%d]%s\n最后登录: %s", user.Role.Name, entityID, user.Name, lastLogin)
		if flowTime != "" {
			msgLine += fmt.Sprintf("\n最后流水: %s", flowTime)
		}
		if orderTime != "" {
			msgLine += fmt.Sprintf("\n最后进单: %s", orderTime)
		}

		entityKey := fmt.Sprintf("%d", entityID)
		resMsgs[entityKey] = msgLine

		expireList = append(expireList, expireInfo{
			UserID:   user.ID,
			RoleID:   user.Role.Key,
			EntityID: entityID,
		})
	}

	// 汇总日志
	sortedKeys := make([]string, 0, len(resMsgs))
	for k := range resMsgs {
		sortedKeys = append(sortedKeys, k)
	}
	var msgLines []string
	for _, k := range sortedKeys {
		msgLines = append(msgLines, resMsgs[k])
	}
	summaryMsg := fmt.Sprintf("%s即将关闭%d个\n%s", now.Format("2006-01-02 15:04:05"), len(expireList), strings.Join(msgLines, "\n"))
	log.Printf("[Jobs] %s", summaryMsg)

	// 关闭用户
	for _, e := range expireList {
		s.DB.Model(&model.Users{}).Where("id = ?", e.UserID).Update("status", false)
	}

	// 发送全局通知
	chatID := s.getSystemConfigString("tg_bot", "user_expire_chat")
	if chatID != "" {
		tg := GetTelegramService()
		tg.ForwardBot(map[string]interface{}{
			"forwards": summaryMsg,
			"chat_id":  chatID,
		})
	}

	// 按租户汇总并发送通知
	noti := make(map[uint]map[string]interface{})
	for _, e := range expireList {
		var tenantID uint
		var tenantTelegram string

		switch e.RoleID {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := s.DB.Where("id = ?", e.EntityID).First(&tenant).Error; err != nil {
				continue
			}
			tenantID = tenant.ID
			tenantTelegram = tenant.Telegram
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := s.DB.Preload("Parent").Where("id = ?", e.EntityID).First(&writeoff).Error; err != nil || writeoff.Parent == nil {
				continue
			}
			tenantID = writeoff.ParentID
			tenantTelegram = writeoff.Parent.Telegram
		case model.RoleKeyMerchant:
			var merchant model.Merchant
			if err := s.DB.Preload("Parent").Where("id = ?", e.EntityID).First(&merchant).Error; err != nil || merchant.Parent == nil {
				continue
			}
			tenantID = merchant.ParentID
			tenantTelegram = merchant.Parent.Telegram
		}

		entityKey := fmt.Sprintf("%d", e.EntityID)
		msgContent := resMsgs[entityKey]

		if _, ok := noti[tenantID]; !ok {
			noti[tenantID] = map[string]interface{}{
				"forwards": "即将关闭\n" + msgContent,
				"chat_id":  tenantTelegram,
			}
		} else {
			noti[tenantID]["forwards"] = noti[tenantID]["forwards"].(string) + msgContent
		}
	}

	tg := GetTelegramService()
	for _, data := range noti {
		chatIDVal, ok := data["chat_id"].(string)
		if !ok || chatIDVal == "" {
			continue
		}
		tg.ForwardBot(data)
	}

	return len(expireList), nil
}

// getSystemConfigString 从数据库读取系统配置字符串值（支持 parent.child 格式）
func (s *JobsService) getSystemConfigString(parentKey string, childKey string) string {
	var parent model.SystemConfig
	if err := s.DB.Where("`key` = ?", parentKey).First(&parent).Error; err != nil {
		return ""
	}
	var child model.SystemConfig
	if err := s.DB.Where("parent_id = ? AND `key` = ?", parent.ID, childKey).First(&child).Error; err != nil {
		return ""
	}
	if child.Value == nil {
		return ""
	}
	val := strings.Trim(*child.Value, "\"")
	return val
}

// ===== 单个报告接口辅助 =====

// ReportTenantPreOne 单个租户商户日终报告
func (s *JobsService) ReportTenantPreOne(tenantID uint) error {
	today := time.Now().AddDate(0, 0, -1)
	var tenant model.Tenant
	if err := s.DB.Preload("SystemUser").Where("telegram IS NOT NULL AND telegram != '' AND id = ?", tenantID).First(&tenant).Error; err != nil {
		return fmt.Errorf("找不到租户")
	}
	s.ReportTenantPreJob(&tenant, today)
	return nil
}

// ReportSplitPreOne 单个租户归集日终报告
func (s *JobsService) ReportSplitPreOne(tenantID uint) error {
	today := time.Now().AddDate(0, 0, -1)
	var tenant model.Tenant
	if err := s.DB.Preload("SystemUser").Where("telegram IS NOT NULL AND telegram != '' AND id = ?", tenantID).First(&tenant).Error; err != nil {
		return fmt.Errorf("找不到租户")
	}
	s.ReportSplitPreJob(&tenant, today)
	return nil
}

// ReportMerchantPreOne 单个商户日终报告
func (s *JobsService) ReportMerchantPreOne(merchantID uint) error {
	today := time.Now().AddDate(0, 0, -1)
	var merchant model.Merchant
	if err := s.DB.Where("telegram IS NOT NULL AND telegram != '' AND id = ?", merchantID).First(&merchant).Error; err != nil {
		return fmt.Errorf("找不到商户")
	}
	s.ReportMerchantPreJob(today, &merchantID)
	return nil
}
