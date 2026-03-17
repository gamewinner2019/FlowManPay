package model

import (

	"gorm.io/gorm"
)

// ===== NotifyStatus 通知状态 =====

type NotifyStatus int

const (
	NotifyStatusPending NotifyStatus = 0 // 未通知
	NotifyStatusFailed  NotifyStatus = 1 // 通知失败
	NotifyStatusSuccess NotifyStatus = 2 // 通知成功
)

func (s NotifyStatus) Label() string {
	switch s {
	case NotifyStatusPending:
		return "未通知"
	case NotifyStatusFailed:
		return "通知失败"
	case NotifyStatusSuccess:
		return "通知成功"
	default:
		return "未知"
	}
}

// ===== MerchantNotification 商户通知 =====

// MerchantNotification 商户通知
type MerchantNotification struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	OrderID        string         `gorm:"size:30;uniqueIndex" json:"order_id"`        // 关联订单
	Order          *Order         `gorm:"foreignKey:OrderID" json:"order,omitempty"`
	Status         NotifyStatus   `gorm:"default:0" json:"status"`                    // 通知状态
	Version        int            `gorm:"default:0" json:"-"`                         // 乐观锁
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime DateTime      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime DateTime      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (MerchantNotification) TableName() string {
	return TablePrefix + "merchant_notification"
}

// ===== MerchantNotificationHistory 商户通知记录 =====

// MerchantNotificationHistory 商户通知记录
type MerchantNotificationHistory struct {
	ID             uint                  `gorm:"primaryKey" json:"id"`
	NotificationID uint                  `gorm:"index" json:"notification_id"`          // 关联通知
	Notification   *MerchantNotification `gorm:"foreignKey:NotificationID" json:"notification,omitempty"`
	URL            string                `gorm:"type:text" json:"url"`                  // 通知地址
	RequestBody    string                `gorm:"type:text" json:"request_body"`         // 请求参数
	RequestMethod  string                `gorm:"size:8" json:"request_method"`          // 请求方式
	ResponseCode   int                   `gorm:"default:-1" json:"response_code"`       // 响应状态码
	JSONResult     string                `gorm:"type:text" json:"json_result"`          // 返回信息
	Description    string                `gorm:"size:255;default:''" json:"description"`
	Creator        *uint                 `gorm:"index" json:"creator"`
	Modifier       *uint                 `json:"modifier"`
	CreateDatetime DateTime             `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime DateTime             `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt        `gorm:"index" json:"-"`
}

func (MerchantNotificationHistory) TableName() string {
	return TablePrefix + "merchant_notification_history"
}

// ===== PhoneProductNotificationHistory 话单通知记录 =====

// PhoneProductNotificationHistory 话单通知记录
type PhoneProductNotificationHistory struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	ProductID      uint           `gorm:"index" json:"product_id"`                    // 关联话单
	URL            string         `gorm:"type:text" json:"url"`
	RequestBody    string         `gorm:"type:text" json:"request_body"`
	RequestMethod  string         `gorm:"size:8" json:"request_method"`
	ResponseCode   int            `gorm:"default:-1" json:"response_code"`
	JSONResult     string         `gorm:"type:text" json:"json_result"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime DateTime      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime DateTime      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PhoneProductNotificationHistory) TableName() string {
	return TablePrefix + "phone_product_notification_history"
}

// ===== HouseProductNotificationHistory 户号通知记录 =====

// HouseProductNotificationHistory 户号通知记录
type HouseProductNotificationHistory struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	HouseID        uint           `gorm:"index" json:"house_id"`                      // 关联户号
	URL            string         `gorm:"type:text" json:"url"`
	RequestBody    string         `gorm:"type:text" json:"request_body"`
	RequestMethod  string         `gorm:"size:8" json:"request_method"`
	ResponseCode   int            `gorm:"default:-1" json:"response_code"`
	JSONResult     string         `gorm:"type:text" json:"json_result"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime DateTime      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime DateTime      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (HouseProductNotificationHistory) TableName() string {
	return TablePrefix + "house_notification_history"
}
