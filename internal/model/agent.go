package model

import (
	"time"

	"gorm.io/gorm"
)

// Tenant maps to dvadmin_tenant (租户/代理商)
type Tenant struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	SystemUserID   uint           `gorm:"uniqueIndex" json:"system_user_id"`
	SystemUser     Users          `gorm:"foreignKey:SystemUserID" json:"system_user,omitempty"`
	Balance        int64          `gorm:"default:0" json:"balance"`          // 余额(分)
	Telegram       string         `gorm:"size:255" json:"telegram"`          // Telegram群
	Trust          bool           `gorm:"default:false" json:"trust"`        // 是否允许负数余额
	PreTax         int64          `gorm:"default:0" json:"pre_tax"`          // 预占金额
	Polling        bool           `gorm:"default:false" json:"polling"`      // 轮训归集
	BotToken       string         `gorm:"size:255" json:"bot_token"`         // Bot Token
	BotChatID      string         `gorm:"size:255" json:"bot_chat_id"`       // Bot Chat ID
	Version        int            `gorm:"default:0" json:"-"`                // 乐观锁
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Tenant) TableName() string {
	return TablePrefix + "tenant"
}

// Merchant maps to dvadmin_merchant (商户)
type Merchant struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	SystemUserID   uint           `gorm:"uniqueIndex" json:"system_user_id"`
	SystemUser     Users          `gorm:"foreignKey:SystemUserID" json:"system_user,omitempty"`
	ParentID       uint           `gorm:"index" json:"parent_id"`            // 所属租户
	Parent         *Tenant        `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Telegram       string         `gorm:"size:255" json:"telegram"`
	BotToken       string         `gorm:"size:255" json:"bot_token"`
	BotChatID      string         `gorm:"size:255" json:"bot_chat_id"`
	Version        int            `gorm:"default:0" json:"-"`
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Merchant) TableName() string {
	return TablePrefix + "merchant"
}

// WriteOff maps to dvadmin_writeoff (核销)
type WriteOff struct {
	ID               uint           `gorm:"primaryKey" json:"id"`
	SystemUserID     uint           `gorm:"uniqueIndex" json:"system_user_id"`
	SystemUser       Users          `gorm:"foreignKey:SystemUserID" json:"system_user,omitempty"`
	ParentID         uint           `gorm:"index" json:"parent_id"`            // 所属租户
	Parent           *Tenant        `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	ParentWriteoffID *uint          `gorm:"index" json:"parent_writeoff_id"`   // 上级核销(多级)
	ParentWriteoff   *WriteOff      `gorm:"foreignKey:ParentWriteoffID" json:"parent_writeoff,omitempty"`
	Balance          *int64         `json:"balance"`                           // 余额(分), nil=不限额
	White            bool           `gorm:"default:false" json:"white"`        // 白名单
	Telegram         string         `gorm:"size:255" json:"telegram"`
	BotToken         string         `gorm:"size:255" json:"bot_token"`
	BotChatID        string         `gorm:"size:255" json:"bot_chat_id"`
	Version          int            `gorm:"default:0" json:"-"`
	Creator          *uint          `json:"creator"`
	Modifier         *uint          `json:"modifier"`
	CreateDatetime   time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime   time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (WriteOff) TableName() string {
	return TablePrefix + "writeoff"
}

// TenantTax 租户预占用金额
type TenantTax struct {
	TenantID uint   `gorm:"primaryKey" json:"tenant_id"`
	PreTax   int64  `gorm:"default:0" json:"pre_tax"`
	Version  int    `gorm:"default:0" json:"-"`
}

func (TenantTax) TableName() string {
	return TablePrefix + "tenant_tax"
}

// WriteoffTax 核销预占用金额
type WriteoffTax struct {
	WriteoffID uint  `gorm:"primaryKey" json:"writeoff_id"`
	PreTax     int64 `gorm:"default:0" json:"pre_tax"`
	Version    int   `gorm:"default:0" json:"-"`
}

func (WriteoffTax) TableName() string {
	return TablePrefix + "writeoff_tax"
}

// WriteoffBrokerage 核销佣金
type WriteoffBrokerage struct {
	WriteoffID uint  `gorm:"primaryKey" json:"writeoff_id"`
	Brokerage  int64 `gorm:"default:0" json:"brokerage"`
	Version    int   `gorm:"default:0" json:"-"`
}

func (WriteoffBrokerage) TableName() string {
	return TablePrefix + "writeoff_brokerage"
}

// MerchantPre 商户预付
type MerchantPre struct {
	ID         uint  `gorm:"primaryKey" json:"id"`
	MerchantID uint  `gorm:"index" json:"merchant_id"`
	PrePay     int64 `gorm:"default:0" json:"pre_pay"`
	Version    int   `gorm:"default:0" json:"-"`
}

func (MerchantPre) TableName() string {
	return TablePrefix + "merchant_pre"
}

// WriteoffPre 核销预付
type WriteoffPre struct {
	ID         uint  `gorm:"primaryKey" json:"id"`
	WriteoffID uint  `gorm:"index" json:"writeoff_id"`
	PrePay     int64 `gorm:"default:0" json:"pre_pay"`
	Version    int   `gorm:"default:0" json:"-"`
}

func (WriteoffPre) TableName() string {
	return TablePrefix + "writeoff_pre"
}

// TenantCashFlow 租户资金流水
type TenantCashFlow struct {
	ID             uint               `gorm:"primaryKey" json:"id"`
	TenantID       uint               `gorm:"index" json:"tenant_id"`
	Tenant         *Tenant            `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	FlowType       TenantCashFlowType `gorm:"default:0" json:"flow_type"`
	OldMoney       int64              `gorm:"default:0" json:"old_money"`       // 变更前余额
	NewMoney       int64              `gorm:"default:0" json:"new_money"`       // 变更后余额
	ChangeMoney    int64              `gorm:"default:0" json:"change_money"`    // 变更余额
	PayChannelID   *uint              `json:"pay_channel_id"`                   // 支付通道
	OrderID        *string            `gorm:"size:30" json:"order_id"`          // 系统订单
	Description    string             `gorm:"size:255;default:''" json:"description"`
	Creator        *uint              `gorm:"index" json:"creator"`
	Modifier       *uint              `json:"modifier"`
	CreateDatetime time.Time          `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time          `gorm:"autoUpdateTime" json:"update_datetime"`
}

func (TenantCashFlow) TableName() string {
	return TablePrefix + "tenant_cashflow"
}

// WriteoffCashFlow 核销资金流水
type WriteoffCashFlow struct {
	ID             uint                 `gorm:"primaryKey" json:"id"`
	WriteoffID     uint                 `gorm:"index" json:"writeoff_id"`
	Writeoff       *WriteOff            `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	FlowType       WriteoffCashFlowType `gorm:"default:0" json:"flow_type"`
	OldMoney       int64                `gorm:"default:0" json:"old_money"`       // 变更前余额
	NewMoney       int64                `gorm:"default:0" json:"new_money"`       // 变更后余额
	ChangeMoney    int64                `gorm:"default:0" json:"change_money"`    // 变更余额
	Tax            float64              `gorm:"type:decimal(5,2);default:0" json:"tax"`
	PayChannelID   *uint                `json:"pay_channel_id"`                   // 支付通道
	OrderID        *string              `gorm:"size:30" json:"order_id"`          // 系统订单
	Description    string               `gorm:"size:255;default:''" json:"description"`
	Creator        *uint                `gorm:"index" json:"creator"`
	Modifier       *uint                `json:"modifier"`
	CreateDatetime time.Time            `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time            `gorm:"autoUpdateTime" json:"update_datetime"`
}

func (WriteoffCashFlow) TableName() string {
	return TablePrefix + "writeoff_cashflow"
}

// WriteoffBrokerageFlow 核销佣金流水
type WriteoffBrokerageFlow struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	WriteoffID     uint      `gorm:"index" json:"writeoff_id"`
	Writeoff       *WriteOff `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	FromWriteoffID *uint     `json:"from_writeoff_id"`
	FromWriteoff   *WriteOff `gorm:"foreignKey:FromWriteoffID" json:"from_writeoff,omitempty"`
	OldMoney       int64     `gorm:"default:0" json:"old_money"`       // 变更前余额
	NewMoney       int64     `gorm:"default:0" json:"new_money"`       // 变更后余额
	ChangeMoney    int64     `gorm:"default:0" json:"change_money"`    // 变更余额
	Tax            float64   `gorm:"type:decimal(5,2);default:0" json:"tax"`
	PayChannelID   *uint     `json:"pay_channel_id"`                   // 支付通道
	OrderID        *string   `gorm:"size:30" json:"order_id"`          // 系统订单
	Description    string    `gorm:"size:255;default:''" json:"description"`
	Creator        *uint     `gorm:"index" json:"creator"`
	Modifier       *uint     `json:"modifier"`
	CreateDatetime time.Time `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time `gorm:"autoUpdateTime" json:"update_datetime"`
}

func (WriteoffBrokerageFlow) TableName() string {
	return TablePrefix + "writeoff_brokerage_flow"
}
