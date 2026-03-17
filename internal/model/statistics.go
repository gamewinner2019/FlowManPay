package model

// statistics models

// ===== BaseDayStatistics 日统计基础字段 =====

// baseDayStatisticsFields 日统计通用字段(嵌入用)
type baseDayStatisticsFields struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	SuccessCount int       `gorm:"default:0" json:"success_count"`    // 成功订单数
	SubmitCount  int       `gorm:"default:0" json:"submit_count"`     // 总提交订单数
	SuccessMoney int64     `gorm:"default:0" json:"success_money"`    // 总收入(分)
	UnknownCount int       `gorm:"default:0" json:"unknown_count"`    // 未知设备订单数
	AndroidCount int       `gorm:"default:0" json:"android_count"`    // 安卓订单数
	IOSCount     int       `gorm:"default:0" json:"ios_count"`        // 苹果订单数
	PCCount      int       `gorm:"default:0" json:"pc_count"`         // PC订单数
	TotalTax     int64     `gorm:"default:0" json:"total_tax"`        // 总利润(分)
	Date         DateTime `gorm:"type:date" json:"date"`             // 统计日期
	Version      int       `gorm:"default:0" json:"-"`                // 乐观锁
}

// ===== TenantDayStatistics 租户日统计 =====

// TenantDayStatistics 租户日统计
type TenantDayStatistics struct {
	baseDayStatisticsFields
	TenantID *uint   `gorm:"index" json:"tenant_id"`
	Tenant   *Tenant `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
}

func (TenantDayStatistics) TableName() string {
	return TablePrefix + "day_statistics_tenant"
}

// ===== MerchantDayStatistics 商户日统计 =====

// MerchantDayStatistics 商户日统计
type MerchantDayStatistics struct {
	baseDayStatisticsFields
	MerchantID *uint     `gorm:"index" json:"merchant_id"`
	Merchant   *Merchant `gorm:"foreignKey:MerchantID" json:"merchant,omitempty"`
	RealMoney  int64     `gorm:"default:0" json:"real_money"` // 实际收入(分)
}

func (MerchantDayStatistics) TableName() string {
	return TablePrefix + "day_statistics_merchant"
}

// ===== WriteOffDayStatistics 核销日统计 =====

// WriteOffDayStatistics 核销日统计
type WriteOffDayStatistics struct {
	baseDayStatisticsFields
	WriteoffID  *uint     `gorm:"index" json:"writeoff_id"`
	Writeoff    *WriteOff `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	SubmitMoney int64     `gorm:"default:0" json:"submit_money"` // 总提交收入(分)
}

func (WriteOffDayStatistics) TableName() string {
	return TablePrefix + "day_statistics_writeoff"
}

// ===== WriteOffChannelDayStatistics 核销通道日统计 =====

// WriteOffChannelDayStatistics 核销通道日统计
type WriteOffChannelDayStatistics struct {
	baseDayStatisticsFields
	WriteoffID   *uint       `gorm:"index" json:"writeoff_id"`
	Writeoff     *WriteOff   `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	PayChannelID *uint       `gorm:"index" json:"pay_channel_id"`
	PayChannel   *PayChannel `gorm:"foreignKey:PayChannelID" json:"pay_channel,omitempty"`
}

func (WriteOffChannelDayStatistics) TableName() string {
	return TablePrefix + "day_statistics_channel_writeoff"
}

// ===== PayChannelDayStatistics 支付通道日统计 =====

// PayChannelDayStatistics 支付通道日统计
type PayChannelDayStatistics struct {
	baseDayStatisticsFields
	PayChannelID *uint       `gorm:"index" json:"pay_channel_id"`
	PayChannel   *PayChannel `gorm:"foreignKey:PayChannelID" json:"pay_channel,omitempty"`
	TenantID     *uint       `gorm:"index" json:"tenant_id"`
	Tenant       *Tenant     `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	MerchantID   *uint       `gorm:"index" json:"merchant_id"`
	Merchant     *Merchant   `gorm:"foreignKey:MerchantID" json:"merchant,omitempty"`
	WriteoffID   *uint       `gorm:"index" json:"writeoff_id"`
	Writeoff     *WriteOff   `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	RealMoney    int64       `gorm:"default:0" json:"real_money"` // 实际收入(分)
}

func (PayChannelDayStatistics) TableName() string {
	return TablePrefix + "day_statistics_pay_channel"
}

// ===== DayStatistics 全局日统计 =====

// DayStatistics 全局日统计
type DayStatistics struct {
	baseDayStatisticsFields
	SubmitMoney int64 `gorm:"default:0" json:"submit_money"` // 总提交收入(分)
}

func (DayStatistics) TableName() string {
	return TablePrefix + "day_statistics"
}
