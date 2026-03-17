package service

import (
	"log"
	"sync"

	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/model"
)

// ===== Hook System =====
// Go 版本的 hook 系统，替代 Django 的装饰器注册模式
// 使用函数切片实现回调注册

// OrderSuccessHandler 订单成功回调函数类型
type OrderSuccessHandler func(db *gorm.DB, order *model.Order, detail *model.OrderDetail)

// OrderTimeoutHandler 订单超时回调函数类型
type OrderTimeoutHandler func(db *gorm.DB, order *model.Order, detail *model.OrderDetail)

// OrderDeviceHandler 订单设备回调函数类型
type OrderDeviceHandler func(db *gorm.DB, order *model.Order, device *model.OrderDeviceDetails)

// OrderRefundHandler 订单退款回调函数类型
type OrderRefundHandler func(db *gorm.DB, order *model.Order, detail *model.OrderDetail)

// HookRegistry hook 注册表
type HookRegistry struct {
	mu              sync.RWMutex
	successHandlers []OrderSuccessHandler
	timeoutHandlers []OrderTimeoutHandler
	deviceHandlers  []OrderDeviceHandler
	refundHandlers  []OrderRefundHandler
}

var hookRegistry *HookRegistry
var hookOnce sync.Once

// GetHookRegistry 获取 hook 注册表单例
func GetHookRegistry() *HookRegistry {
	hookOnce.Do(func() {
		hookRegistry = &HookRegistry{}
	})
	return hookRegistry
}

// RegisterSuccess 注册订单成功回调
func (r *HookRegistry) RegisterSuccess(handler OrderSuccessHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.successHandlers = append(r.successHandlers, handler)
}

// RegisterTimeout 注册订单超时回调
func (r *HookRegistry) RegisterTimeout(handler OrderTimeoutHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timeoutHandlers = append(r.timeoutHandlers, handler)
}

// RegisterDevice 注册订单设备回调
func (r *HookRegistry) RegisterDevice(handler OrderDeviceHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deviceHandlers = append(r.deviceHandlers, handler)
}

// RegisterRefund 注册订单退款回调
func (r *HookRegistry) RegisterRefund(handler OrderRefundHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refundHandlers = append(r.refundHandlers, handler)
}

// TriggerSuccess 触发订单成功回调
func (r *HookRegistry) TriggerSuccess(db *gorm.DB, order *model.Order, detail *model.OrderDetail) {
	r.mu.RLock()
	handlers := make([]OrderSuccessHandler, len(r.successHandlers))
	copy(handlers, r.successHandlers)
	r.mu.RUnlock()

	for _, handler := range handlers {
		func() {
			defer func() {
				if err := recover(); err != nil {
					log.Printf("[Hook] 订单成功回调异常: %v", err)
				}
			}()
			handler(db, order, detail)
		}()
	}
}

// TriggerTimeout 触发订单超时回调
func (r *HookRegistry) TriggerTimeout(db *gorm.DB, order *model.Order, detail *model.OrderDetail) {
	r.mu.RLock()
	handlers := make([]OrderTimeoutHandler, len(r.timeoutHandlers))
	copy(handlers, r.timeoutHandlers)
	r.mu.RUnlock()

	for _, handler := range handlers {
		func() {
			defer func() {
				if err := recover(); err != nil {
					log.Printf("[Hook] 订单超时回调异常: %v", err)
				}
			}()
			handler(db, order, detail)
		}()
	}
}

// TriggerDevice 触发订单设备回调
func (r *HookRegistry) TriggerDevice(db *gorm.DB, order *model.Order, device *model.OrderDeviceDetails) {
	r.mu.RLock()
	handlers := make([]OrderDeviceHandler, len(r.deviceHandlers))
	copy(handlers, r.deviceHandlers)
	r.mu.RUnlock()

	for _, handler := range handlers {
		func() {
			defer func() {
				if err := recover(); err != nil {
					log.Printf("[Hook] 订单设备回调异常: %v", err)
				}
			}()
			handler(db, order, device)
		}()
	}
}

// TriggerRefund 触发订单退款回调
func (r *HookRegistry) TriggerRefund(db *gorm.DB, order *model.Order, detail *model.OrderDetail) {
	r.mu.RLock()
	handlers := make([]OrderRefundHandler, len(r.refundHandlers))
	copy(handlers, r.refundHandlers)
	r.mu.RUnlock()

	for _, handler := range handlers {
		func() {
			defer func() {
				if err := recover(); err != nil {
					log.Printf("[Hook] 订单退款回调异常: %v", err)
				}
			}()
			handler(db, order, detail)
		}()
	}
}

// ===== 内置 Hook 注册 =====

// RegisterBuiltinHooks 注册内置 hook
func RegisterBuiltinHooks(db *gorm.DB) {
	registry := GetHookRegistry()
	statsSvc := NewStatisticsService(db)
	splitSvc := NewSplitService(db)
	cashFlowSvc := NewCashFlowService(db)

	// 订单成功 hook: 更新统计
	registry.RegisterSuccess(func(db *gorm.DB, order *model.Order, detail *model.OrderDetail) {
		// 更新统计
		statsSvc.SuccessDayStatistics(
			tenantIDFromOrder(db, order),
			order.MerchantID,
			detail.WriteoffID,
			order.PayChannelID,
			int64(order.Money),
			int64(order.Tax),
			int64(detail.MerchantTax),
			model.DeviceTypeUnknown,
		)

		// 扣减租户余额（手续费）
		if tid := tenantIDFromOrder(db, order); tid != nil {
			cashFlowSvc.DeductTenantBalance(*tid, int64(order.Tax), order.PayChannelID, &order.ID)
		}
	})

	// 订单成功 hook: 分账预付扣减
	registry.RegisterSuccess(func(db *gorm.DB, order *model.Order, detail *model.OrderDetail) {
		if detail.PluginID == nil {
			return
		}
		// 查找分账历史
		var splitHistory model.SplitHistory
		if err := db.Where("order_id = ?", order.ID).First(&splitHistory).Error; err != nil {
			return
		}
		if splitHistory.AlipayUserID != nil && *splitHistory.AlipayUserID > 0 {
			splitSvc.SaveSplitPrePay(*splitHistory.AlipayUserID, order.Money)
		}
	})

	// 订单设备 hook: 更新设备类型统计
	registry.RegisterDevice(func(db *gorm.DB, order *model.Order, device *model.OrderDeviceDetails) {
		var detail model.OrderDetail
		if err := db.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
			return
		}
		statsSvc.DeviceDayStatistics(
			tenantIDFromOrder(db, order),
			order.MerchantID,
			detail.WriteoffID,
			order.PayChannelID,
			device.DeviceType,
		)
	})

	// 订单退款 hook: 更新退款统计
	registry.RegisterRefund(func(db *gorm.DB, order *model.Order, detail *model.OrderDetail) {
		statsSvc.RefundDayStatistics(
			tenantIDFromOrder(db, order),
			order.MerchantID,
			detail.WriteoffID,
			order.PayChannelID,
			int64(order.Money),
		)
	})

	// 订单超时 hook: 关闭订单
	registry.RegisterTimeout(func(db *gorm.DB, order *model.Order, detail *model.OrderDetail) {
		log.Printf("[Hook] 订单超时关闭: %s", order.OrderNo)
	})
}

// tenantIDFromOrder 从订单获取租户ID
func tenantIDFromOrder(db *gorm.DB, order *model.Order) *uint {
	if order.Merchant != nil && order.Merchant.Parent != nil {
		return &order.Merchant.Parent.ID
	}
	if order.MerchantID != nil {
		var merchant model.Merchant
		if err := db.First(&merchant, *order.MerchantID).Error; err == nil {
			return &merchant.ParentID
		}
	}
	return nil
}

// ===== CashFlowService 扩展 =====

// DeductTenantBalance 扣减租户余额
func (s *CashFlowService) DeductTenantBalance(tenantID uint, tax int64, payChannelID *uint, orderID *string) {
	if tax <= 0 {
		return
	}
	var tenant model.Tenant
	if err := s.DB.First(&tenant, tenantID).Error; err != nil {
		return
	}

	oldBalance := tenant.Balance
	newBalance := oldBalance - tax

	result := s.DB.Model(&tenant).Where("version = ?", tenant.Version).Updates(map[string]interface{}{
		"balance": newBalance,
		"version": gorm.Expr("version + 1"),
	})
	if result.RowsAffected == 0 {
		log.Printf("[CashFlow] 租户%d余额扣减冲突(version=%d), 手续费%d分丢失", tenantID, tenant.Version, tax)
		return
	}

	// 记录流水
	s.DB.Create(&model.TenantCashFlow{
		TenantID:     tenantID,
		FlowType:     model.TenantCashFlowCommission,
		OldMoney:     oldBalance,
		NewMoney:     newBalance,
		ChangeMoney:  -tax,
		PayChannelID: payChannelID,
		OrderID:      orderID,
		Description:  "订单手续费",
	})

	// 检查余额警告
	if newBalance < 100000 { // 低于1000元
		go func() {
			var t model.Tenant
			if err := s.DB.Preload("SystemUser").First(&t, tenantID).Error; err != nil {
				return
			}
			if t.Telegram != "" {
				GetTelegramService().CheckBalanceForward(newBalance, t.Telegram, tenantID, t.SystemUser.Username)
			}
		}()
	}
}

// ===== StatisticsService 退款扩展 =====

// RefundDayStatistics 订单退款时更新统计
func (s *StatisticsService) RefundDayStatistics(tenantID *uint, merchantID *uint, writeoffID *uint, payChannelID *uint, money int64) {
	date := today()

	// 全局统计
	s.DB.Model(&model.DayStatistics{}).
		Where("date = ?", date).
		Updates(map[string]interface{}{
			"success_count": gorm.Expr("GREATEST(success_count - 1, 0)"),
			"success_money": gorm.Expr("GREATEST(success_money - ?, 0)", money),
		})

	// 租户统计
	if tenantID != nil {
		s.DB.Model(&model.TenantDayStatistics{}).
			Where("date = ? AND tenant_id = ?", date, *tenantID).
			Updates(map[string]interface{}{
				"success_count": gorm.Expr("GREATEST(success_count - 1, 0)"),
				"success_money": gorm.Expr("GREATEST(success_money - ?, 0)", money),
			})
	}

	// 商户统计
	if merchantID != nil {
		s.DB.Model(&model.MerchantDayStatistics{}).
			Where("date = ? AND merchant_id = ?", date, *merchantID).
			Updates(map[string]interface{}{
				"success_count": gorm.Expr("GREATEST(success_count - 1, 0)"),
				"success_money": gorm.Expr("GREATEST(success_money - ?, 0)", money),
			})
	}

	// 核销统计
	if writeoffID != nil {
		s.DB.Model(&model.WriteOffDayStatistics{}).
			Where("date = ? AND writeoff_id = ?", date, *writeoffID).
			Updates(map[string]interface{}{
				"success_count": gorm.Expr("GREATEST(success_count - 1, 0)"),
				"success_money": gorm.Expr("GREATEST(success_money - ?, 0)", money),
			})
	}

	// 通道统计
	if payChannelID != nil {
		s.DB.Model(&model.PayChannelDayStatistics{}).
			Where("date = ? AND pay_channel_id = ?", date, *payChannelID).
			Updates(map[string]interface{}{
				"success_count": gorm.Expr("GREATEST(success_count - 1, 0)"),
				"success_money": gorm.Expr("GREATEST(success_money - ?, 0)", money),
			})
	}
}
