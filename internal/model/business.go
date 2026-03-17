package model

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ===== MerchantPreHistory 商户预付历史 =====

// MerchantPreHistory 商户预付历史记录
type MerchantPreHistory struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	MerchantID     uint           `gorm:"index" json:"merchant_id"`
	Merchant       *Merchant      `gorm:"foreignKey:MerchantID" json:"merchant,omitempty"`
	PrePay         int64          `gorm:"default:0" json:"pre_pay"`       // 预付金额
	Before         int64          `gorm:"default:0" json:"before"`        // 改动前金额
	After          int64          `gorm:"default:0" json:"after"`         // 改动后金额
	User           string         `gorm:"size:255" json:"user"`           // 操作人
	Rate           string         `gorm:"size:32;default:'0'" json:"rate"` // USDT汇率
	USDT           string         `gorm:"size:32;default:'0'" json:"usdt"` // USDT
	Cert           string         `gorm:"type:text" json:"cert"`          // 转账凭证
	Version        int            `gorm:"default:0" json:"-"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (MerchantPreHistory) TableName() string {
	return TablePrefix + "merchant_pre_history"
}

// ===== WriteoffPreHistory 核销预付历史 =====

// WriteoffPreHistory 核销预付历史记录
type WriteoffPreHistory struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	WriteoffID     uint           `gorm:"index" json:"writeoff_id"`
	Writeoff       *WriteOff      `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	PrePay         int64          `gorm:"default:0" json:"pre_pay"`       // 预付金额
	Before         int64          `gorm:"default:0" json:"before"`        // 改动前金额
	After          int64          `gorm:"default:0" json:"after"`         // 改动后金额
	User           string         `gorm:"size:255" json:"user"`           // 操作人
	Rate           string         `gorm:"size:32;default:'0'" json:"rate"` // USDT汇率
	USDT           string         `gorm:"size:32;default:'0'" json:"usdt"` // USDT
	Cert           string         `gorm:"type:text" json:"cert"`          // 转账凭证
	Version        int            `gorm:"default:0" json:"-"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (WriteoffPreHistory) TableName() string {
	return TablePrefix + "writeoff_pre_history"
}

// ===== TenantYufuUser Telegram绑定 =====

// TenantYufuUser 租户预付用户(Telegram绑定)
type TenantYufuUser struct {
	ID       uint    `gorm:"primaryKey" json:"id"`
	TenantID *uint   `gorm:"index" json:"tenant_id"`
	Tenant   *Tenant `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Telegram string  `gorm:"size:255" json:"telegram"` // Telegram用户id
}

func (TenantYufuUser) TableName() string {
	return TablePrefix + "tenant_yufu_user"
}

// ===== PhoneProduct 话单产品 =====

// PhoneProduct 话单库存
type PhoneProduct struct {
	ID             string         `gorm:"size:21;primaryKey" json:"id"`
	WriteoffID     uint           `gorm:"index" json:"writeoff_id"`
	Writeoff       *WriteOff      `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	Phone          string         `gorm:"size:11" json:"phone"`
	Province       string         `gorm:"size:100" json:"province"`
	City           string         `gorm:"size:100" json:"city"`
	ProvinceCode   string         `gorm:"size:20" json:"province_code"`
	CityCode       string         `gorm:"size:20" json:"city_code"`
	Company        int            `gorm:"default:0" json:"company"`       // 运营商
	PhoneOrderNo   string         `gorm:"size:255;uniqueIndex" json:"phone_order_no"` // 供货商订单号
	Money          int            `gorm:"default:0" json:"money"`         // 金额(分)
	NotifyURL      string         `gorm:"size:255" json:"notify_url"`
	ChargeType     int            `gorm:"default:0" json:"charge_type"`   // 0=快充 1=慢充
	OrderID        *string        `gorm:"size:30;index" json:"order_id"`
	Order          *Order         `gorm:"foreignKey:OrderID" json:"order,omitempty"`
	OrderStatus    int            `gorm:"default:0" json:"order_status"`  // 0=等待充值...
	FinishDatetime *time.Time     `json:"finish_datetime"`
	Version        int            `gorm:"default:0" json:"-"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PhoneProduct) TableName() string {
	return TablePrefix + "phone_product"
}

// CreatePhoneOrderNo 生成话单订单号
func CreatePhoneOrderNo() string {
	now := time.Now()
	return fmt.Sprintf("P%s%06d", now.Format("20060102150405"), now.Nanosecond()/1e3)
}

// ===== PhoneOrderFlow 话单流水 =====

// PhoneOrderFlow 话单流水
type PhoneOrderFlow struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	WriteoffID uint      `gorm:"index" json:"writeoff_id"`
	Writeoff   *WriteOff `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	Flow       int64     `gorm:"default:0" json:"flow"`
	Refund     int64     `gorm:"default:0" json:"refund"`
	ChargeType int       `gorm:"default:0" json:"charge_type"` // 0=快充 1=慢充
	Date       time.Time `gorm:"type:date;index" json:"date"`
	Version    int       `gorm:"default:0" json:"-"`
}

func (PhoneOrderFlow) TableName() string {
	return TablePrefix + "phone_order_flow"
}
