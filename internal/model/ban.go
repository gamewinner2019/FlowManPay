package model

import (

	"gorm.io/gorm"
)

// ===== BanUserId 封禁用户ID =====

// BanUserId 封禁用户ID
type BanUserId struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	UserID         string         `gorm:"size:32" json:"user_id"`                     // 封禁的用户ID
	TenantID       uint           `gorm:"index" json:"tenant_id"`                     // 关联租户
	Tenant         *Tenant        `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime DateTime      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime DateTime      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BanUserId) TableName() string {
	return TablePrefix + "ban_user_id"
}

// ===== BanIp 封禁IP =====

// BanIp 封禁IP地址
type BanIp struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	IPAddress      string         `gorm:"size:255" json:"ip_address"`                 // IP地址
	TenantID       *uint          `gorm:"index" json:"tenant_id"`                     // 关联租户
	Tenant         *Tenant        `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime DateTime      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime DateTime      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BanIp) TableName() string {
	return TablePrefix + "ban_ip"
}
