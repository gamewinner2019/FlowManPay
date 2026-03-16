package service

import (
	"fmt"
	"log"

	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/model"
)

// CashFlowService 资金流水服务
type CashFlowService struct {
	DB *gorm.DB
}

// NewCashFlowService 创建资金流水服务
func NewCashFlowService(db *gorm.DB) *CashFlowService {
	return &CashFlowService{DB: db}
}

// CreateTenantCashFlow 创建租户资金流水（扣除手续费）
func (s *CashFlowService) CreateTenantCashFlow(tx *gorm.DB, tenantID uint, flowType model.TenantCashFlowType,
	changeMoney int64, payChannelID *uint, orderID *string, creatorID *uint) error {

	var tenant model.Tenant
	if err := tx.First(&tenant, tenantID).Error; err != nil {
		return fmt.Errorf("租户不存在: %w", err)
	}

	oldMoney := tenant.Balance
	newMoney := oldMoney + changeMoney

	// 更新余额（乐观锁）
	result := tx.Model(&tenant).Where("version = ?", tenant.Version).Updates(map[string]interface{}{
		"balance": newMoney,
		"version": tenant.Version + 1,
	})
	if result.RowsAffected == 0 {
		return fmt.Errorf("租户余额更新冲突，请重试")
	}

	// 创建流水记录
	flow := &model.TenantCashFlow{
		TenantID:     tenantID,
		FlowType:     flowType,
		OldMoney:     oldMoney,
		NewMoney:     newMoney,
		ChangeMoney:  changeMoney,
		PayChannelID: payChannelID,
		OrderID:      orderID,
		Creator:      creatorID,
	}
	return tx.Create(flow).Error
}

// CreateWriteoffCashFlow 创建核销资金流水
func (s *CashFlowService) CreateWriteoffCashFlow(tx *gorm.DB, writeoffID uint, flowType model.WriteoffCashFlowType,
	changeMoney int64, tax float64, payChannelID *uint, orderID *string, creatorID *uint) error {

	var writeoff model.WriteOff
	if err := tx.First(&writeoff, writeoffID).Error; err != nil {
		return fmt.Errorf("核销不存在: %w", err)
	}

	oldMoney := int64(0)
	if writeoff.Balance != nil {
		oldMoney = *writeoff.Balance
	}
	newMoney := oldMoney + changeMoney

	// 更新余额（乐观锁）
	result := tx.Model(&writeoff).Where("version = ?", writeoff.Version).Updates(map[string]interface{}{
		"balance": newMoney,
		"version": writeoff.Version + 1,
	})
	if result.RowsAffected == 0 {
		return fmt.Errorf("核销余额更新冲突，请重试")
	}

	// 创建流水记录
	flow := &model.WriteoffCashFlow{
		WriteoffID:   writeoffID,
		FlowType:     flowType,
		OldMoney:     oldMoney,
		NewMoney:     newMoney,
		ChangeMoney:  changeMoney,
		Tax:          tax,
		PayChannelID: payChannelID,
		OrderID:      orderID,
		Creator:      creatorID,
	}
	return tx.Create(flow).Error
}

// CreateWriteoffBrokerageFlow 创建核销佣金流水
func (s *CashFlowService) CreateWriteoffBrokerageFlow(tx *gorm.DB, writeoffID uint, fromWriteoffID *uint,
	changeMoney int64, tax float64, payChannelID *uint, orderID *string, creatorID *uint) error {

	var writeoff model.WriteOff
	if err := tx.First(&writeoff, writeoffID).Error; err != nil {
		return fmt.Errorf("核销不存在: %w", err)
	}

	// 获取佣金记录
	var brokerage model.WriteoffBrokerage
	if err := tx.Where("writeoff_id = ?", writeoffID).First(&brokerage).Error; err != nil {
		return fmt.Errorf("佣金记录不存在: %w", err)
	}

	oldMoney := brokerage.Brokerage
	newMoney := oldMoney + changeMoney

	// 更新佣金余额（乐观锁）
	result := tx.Model(&brokerage).Where("version = ?", brokerage.Version).Updates(map[string]interface{}{
		"brokerage": newMoney,
		"version":   brokerage.Version + 1,
	})
	if result.RowsAffected == 0 {
		return fmt.Errorf("佣金余额更新冲突，请重试")
	}

	// 创建佣金流水记录
	flow := &model.WriteoffBrokerageFlow{
		WriteoffID:     writeoffID,
		FromWriteoffID: fromWriteoffID,
		OldMoney:       oldMoney,
		NewMoney:       newMoney,
		ChangeMoney:    changeMoney,
		Tax:            tax,
		PayChannelID:   payChannelID,
		OrderID:        orderID,
		Creator:        creatorID,
	}
	return tx.Create(flow).Error
}

// ProcessOrderCommission 处理订单手续费(订单支付成功后调用)
func (s *CashFlowService) ProcessOrderCommission(orderNo string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Preload("Merchant").Preload("Merchant.Parent").
			Where("order_no = ?", orderNo).First(&order).Error; err != nil {
			return fmt.Errorf("订单不存在: %w", err)
		}

		var detail model.OrderDetail
		if err := tx.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
			return fmt.Errorf("订单详情不存在: %w", err)
		}

		// 扣除租户手续费
		if order.Tax > 0 && order.Merchant != nil && order.Merchant.Parent != nil {
			tenantID := order.Merchant.Parent.ID
			if err := s.CreateTenantCashFlow(tx, tenantID, model.TenantCashFlowCommission,
				-int64(order.Tax), order.PayChannelID, &order.ID, nil); err != nil {
				log.Printf("扣除租户手续费失败: %v", err)
			}
		}

		return nil
	})
}
