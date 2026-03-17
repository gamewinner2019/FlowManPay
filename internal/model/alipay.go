package model

import (
	"time"

	"gorm.io/gorm"
)

// ===== AlipayProduct 支付宝产品 =====

// AlipayProduct 支付宝产品（主体/账户）
type AlipayProduct struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Name           string         `gorm:"size:128" json:"name"`                       // 主体名称
	UID            string         `gorm:"size:128" json:"uid"`                        // 支付宝用户ID
	AppID          string         `gorm:"size:128" json:"app_id"`                     // 应用ID
	Status         bool           `gorm:"default:true" json:"status"`                 // 是否启用
	CanPay         bool           `gorm:"default:true" json:"can_pay"`                // 是否可支付
	IsDelete       bool           `gorm:"default:false" json:"is_delete"`             // 软删除
	WriteoffID     *uint          `gorm:"index" json:"writeoff_id"`                   // 关联核销
	Writeoff       *WriteOff      `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	ParentID       *uint          `gorm:"index" json:"parent_id"`                     // 上级产品
	Parent         *AlipayProduct `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	AccountType    int            `gorm:"default:0" json:"account_type"`              // 账户类型 0=个人 6=服务商 7=企业
	SignType       int            `gorm:"default:0" json:"sign_type"`                 // 签名类型 0=密钥 1=证书
	CollectionType int            `gorm:"default:0" json:"collection_type"`           // 收款类型 0=分账 1=自动转账 2=不操作 3=智能出款
	MaxFailCount   int            `gorm:"default:0" json:"max_fail_count"`            // 最大连续失败次数(自动关闭)
	LimitMoney     int            `gorm:"default:0" json:"limit_money"`               // 日限额(分) 0=不限
	MaxMoney       int            `gorm:"default:0" json:"max_money"`                 // 单笔最大金额(分) 0=不限
	MinMoney       int            `gorm:"default:0" json:"min_money"`                 // 单笔最小金额(分) 0=不限
	FloatMinMoney  int            `gorm:"default:0" json:"float_min_money"`           // 浮动最小金额(分)
	FloatMaxMoney  int            `gorm:"default:0" json:"float_max_money"`           // 浮动最大金额(分)
	DayCountLimit  int            `gorm:"default:0" json:"day_count_limit"`           // 日成功笔数限制 0=不限
	SettledMoneys  JSONIntSlice   `gorm:"type:json" json:"settled_moneys"`            // 固定金额列表
	Subject        string         `gorm:"size:255" json:"subject"`                    // 订单标题模板
	PrivateKey     string         `gorm:"type:text" json:"private_key"`               // 私钥
	PublicKey      string         `gorm:"type:text" json:"public_key"`                // 公钥
	AppPublicCrt   string         `gorm:"type:text" json:"app_public_crt"`            // 应用公钥证书
	AlipayPublicCrt string        `gorm:"type:text" json:"alipay_public_crt"`         // 支付宝公钥证书
	AlipayRootCrt  string         `gorm:"type:text" json:"alipay_root_crt"`           // 支付宝根证书
	SplitAsync     bool           `gorm:"default:false" json:"split_async"`           // 异步分账
	Proxy          string         `gorm:"size:255" json:"proxy"`                      // 代理
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`

	// 多对多: 允许的支付通道
	AllowPayChannels []PayChannel `gorm:"many2many:dvadmin_alipay_product_allow_pay_channels;" json:"allow_pay_channels,omitempty"`
}

func (AlipayProduct) TableName() string {
	return TablePrefix + "alipay_product"
}

// ===== AlipayProductDayStatistics 支付宝产品日统计 =====

// AlipayProductDayStatistics 支付宝产品日统计
type AlipayProductDayStatistics struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ProductID    uint      `gorm:"index" json:"product_id"`
	PayChannelID uint      `gorm:"index" json:"pay_channel_id"`
	SubmitCount  int       `gorm:"default:0" json:"submit_count"`
	SuccessCount int       `gorm:"default:0" json:"success_count"`
	SuccessMoney int64     `gorm:"default:0" json:"success_money"`
	Date         time.Time `gorm:"type:date;index" json:"date"`
	Version      int       `gorm:"default:0" json:"-"`
}

func (AlipayProductDayStatistics) TableName() string {
	return TablePrefix + "alipay_product_day_statistics"
}

// ===== AlipayWeight 支付宝权重 =====

// AlipayWeight 支付宝产品权重（按通道）
type AlipayWeight struct {
	ID           uint `gorm:"primaryKey" json:"id"`
	AlipayID     uint `gorm:"index" json:"alipay_id"`
	PayChannelID uint `gorm:"index" json:"pay_channel_id"`
	Weight       int  `gorm:"default:1" json:"weight"`
}

func (AlipayWeight) TableName() string {
	return TablePrefix + "alipay_weight"
}

// ===== AlipayShenma 支付宝神码 =====

// AlipayShenma 支付宝神码（跨租户共享产品）
type AlipayShenma struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	AlipayID   uint           `gorm:"index" json:"alipay_id"`
	Alipay     *AlipayProduct `gorm:"foreignKey:AlipayID" json:"alipay,omitempty"`
	TenantID   uint           `gorm:"index" json:"tenant_id"`
	Tenant     *Tenant        `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Status     bool           `gorm:"default:true" json:"status"`
	LimitMoney int            `gorm:"default:0" json:"limit_money"`

	// 多对多: 允许的支付通道
	AllowPayChannels []PayChannel `gorm:"many2many:dvadmin_alipay_shenma_allow_pay_channels;" json:"allow_pay_channels,omitempty"`
}

func (AlipayShenma) TableName() string {
	return TablePrefix + "alipay_shenma"
}

// ===== AlipayShenmaDayStatistics 神码日统计 =====

// AlipayShenmaDayStatistics 神码日统计
type AlipayShenmaDayStatistics struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ShenmaID     uint      `gorm:"index" json:"shenma_id"`
	PayChannelID uint      `gorm:"index" json:"pay_channel_id"`
	SubmitCount  int       `gorm:"default:0" json:"submit_count"`
	SuccessCount int       `gorm:"default:0" json:"success_count"`
	SuccessMoney int64     `gorm:"default:0" json:"success_money"`
	Date         time.Time `gorm:"type:date;index" json:"date"`
	Version      int       `gorm:"default:0" json:"-"`
}

func (AlipayShenmaDayStatistics) TableName() string {
	return TablePrefix + "alipay_shenma_day_statistics"
}

// ===== AlipayPublicPool 支付宝公池 =====

// AlipayPublicPool 支付宝公池（公池模式产品）
type AlipayPublicPool struct {
	ID       uint           `gorm:"primaryKey" json:"id"`
	AlipayID uint           `gorm:"index" json:"alipay_id"`
	Alipay   *AlipayProduct `gorm:"foreignKey:AlipayID" json:"alipay,omitempty"`
	Status   bool           `gorm:"default:true" json:"status"`
}

func (AlipayPublicPool) TableName() string {
	return TablePrefix + "alipay_public_pool"
}

// ===== AlipayPublicPoolDayStatistics 公池日统计 =====

// AlipayPublicPoolDayStatistics 公池日统计
type AlipayPublicPoolDayStatistics struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	PoolID       uint      `gorm:"index" json:"pool_id"`
	PayChannelID uint      `gorm:"index" json:"pay_channel_id"`
	SubmitCount  int       `gorm:"default:0" json:"submit_count"`
	SuccessCount int       `gorm:"default:0" json:"success_count"`
	SuccessMoney int64     `gorm:"default:0" json:"success_money"`
	Date         time.Time `gorm:"type:date;index" json:"date"`
	Version      int       `gorm:"default:0" json:"-"`
}

func (AlipayPublicPoolDayStatistics) TableName() string {
	return TablePrefix + "alipay_public_pool_day_statistics"
}

// ===== AlipaySplitUserGroup 支付宝分账用户组 =====

// AlipaySplitUserGroup 支付宝分账用户组
type AlipaySplitUserGroup struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Name           string         `gorm:"size:128" json:"name"`
	Telegram       string         `gorm:"size:256" json:"telegram"`
	PreStatus      bool           `gorm:"default:false" json:"pre_status"`              // 预付模式
	Status         bool           `gorm:"default:true" json:"status"`
	TenantID       uint           `gorm:"index" json:"tenant_id"`
	Tenant         *Tenant        `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Weight         int            `gorm:"default:1" json:"weight"`
	Tax            float64        `gorm:"type:decimal(5,2);default:0" json:"tax"`       // 费率
	WriteoffID     *uint          `gorm:"index" json:"writeoff_id"`
	Writeoff       *WriteOff      `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (AlipaySplitUserGroup) TableName() string {
	return TablePrefix + "alipay_split_user_group"
}

// ===== AlipaySplitUserGroupPre 分账用户组预付 =====

// AlipaySplitUserGroupPre 分账用户组预付
type AlipaySplitUserGroupPre struct {
	ID      uint  `gorm:"primaryKey" json:"id"`
	GroupID uint  `gorm:"uniqueIndex" json:"group_id"`
	PrePay  int64 `gorm:"default:0" json:"pre_pay"`
	Version int   `gorm:"default:0" json:"-"`
}

func (AlipaySplitUserGroupPre) TableName() string {
	return TablePrefix + "alipay_split_user_group_pre"
}

// ===== AlipaySplitUserGroupPreHistory 分账用户组预付历史 =====

// AlipaySplitUserGroupPreHistory 分账用户组预付历史
type AlipaySplitUserGroupPreHistory struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	GroupID        uint      `gorm:"index" json:"group_id"`
	ChangeMoney    int64     `gorm:"default:0" json:"change_money"`
	OldMoney       int64     `gorm:"default:0" json:"old_money"`
	NewMoney       int64     `gorm:"default:0" json:"new_money"`
	Description    string    `gorm:"size:255;default:''" json:"description"`
	Creator        *uint     `gorm:"index" json:"creator"`
	CreateDatetime time.Time `gorm:"autoCreateTime;index" json:"create_datetime"`
}

func (AlipaySplitUserGroupPreHistory) TableName() string {
	return TablePrefix + "alipay_split_user_group_prehistory"
}

// ===== AlipaySplitUserGroupAddMoney 分账组打款记录 =====

// AlipaySplitUserGroupAddMoney 分账组打款记录
type AlipaySplitUserGroupAddMoney struct {
	ID       uint      `gorm:"primaryKey" json:"id"`
	GroupID  uint      `gorm:"index" json:"group_id"`
	Date     time.Time `gorm:"type:date" json:"date"`
	AddMoney int64     `gorm:"default:0" json:"add_money"`
	Version  int       `gorm:"default:0" json:"-"`
}

func (AlipaySplitUserGroupAddMoney) TableName() string {
	return TablePrefix + "alipay_split_user_group_add_money"
}

// ===== AlipaySplitUser 支付宝分账用户 =====

// AlipaySplitUser 支付宝分账用户
type AlipaySplitUser struct {
	ID             uint                 `gorm:"primaryKey" json:"id"`
	UsernameType   int                  `gorm:"default:0" json:"username_type"`            // 0=UID 1=账户 2=微信商户号
	Username       string               `gorm:"size:255" json:"username"`
	Name           string               `gorm:"size:255" json:"name"`
	Status         bool                 `gorm:"default:true" json:"status"`
	LimitMoney     int64                `gorm:"default:0" json:"limit_money"`
	GroupID        uint                 `gorm:"index" json:"group_id"`
	Group          *AlipaySplitUserGroup `gorm:"foreignKey:GroupID" json:"group,omitempty"`
	Percentage     float64              `gorm:"type:decimal(5,2);default:100" json:"percentage"`
	Risk           int                  `gorm:"default:0" json:"risk"`
	Description    string               `gorm:"size:255;default:''" json:"description"`
	Creator        *uint                `gorm:"index" json:"creator"`
	Modifier       *uint                `json:"modifier"`
	CreateDatetime time.Time            `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time            `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt       `gorm:"index" json:"-"`
}

func (AlipaySplitUser) TableName() string {
	return TablePrefix + "alipay_split_user"
}

// ===== AlipaySplitUserFlow 分账用户日流水 =====

// AlipaySplitUserFlow 分账用户日流水
type AlipaySplitUserFlow struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	AlipayProductID uint      `gorm:"index" json:"alipay_product_id"`
	AlipayUserID    uint      `gorm:"index" json:"alipay_user_id"`
	Flow            int64     `gorm:"default:0" json:"flow"`
	Date            time.Time `gorm:"type:date;index" json:"date"`
	TenantID        uint      `gorm:"index" json:"tenant_id"`
}

func (AlipaySplitUserFlow) TableName() string {
	return TablePrefix + "alipay_split_user_flow"
}

// ===== CollectionUser 归集用户 =====

// CollectionUser 归集用户
type CollectionUser struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Username string `gorm:"size:255" json:"username"`
	Name     string `gorm:"size:255" json:"name"`
	Remarks  string `gorm:"type:text" json:"remarks"`
	TenantID uint   `gorm:"index" json:"tenant_id"`
}

func (CollectionUser) TableName() string {
	return TablePrefix + "collection_user"
}

// ===== CollectionDayFlow 归集日流水 =====

// CollectionDayFlow 归集日流水
type CollectionDayFlow struct {
	ID     uint      `gorm:"primaryKey" json:"id"`
	UserID uint      `gorm:"index" json:"user_id"`
	Flow   int64     `gorm:"default:0" json:"flow"`
	Date   time.Time `gorm:"type:date;index" json:"date"`
}

func (CollectionDayFlow) TableName() string {
	return TablePrefix + "collection_day_flow"
}

// ===== AlipayTransferUserFlow 支付宝用户流水 =====

// AlipayTransferUserFlow 支付宝转账用户流水
type AlipayTransferUserFlow struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	AlipayUserID   uint      `gorm:"index" json:"alipay_user_id"`
	AlipayProductID uint     `gorm:"index" json:"alipay_product_id"`
	Flow           int64     `gorm:"default:0" json:"flow"`
	Date           time.Time `gorm:"type:date;index" json:"date"`
	Version        int       `gorm:"default:0" json:"-"`
}

func (AlipayTransferUserFlow) TableName() string {
	return TablePrefix + "alipay_transfer_user_flow"
}

// ===== SplitHistory 分账历史 =====

// SplitHistory 分账历史记录
type SplitHistory struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	TicketOrderNo  string         `gorm:"size:128" json:"ticket_order_no"`
	AlipayUserID   *uint          `gorm:"index" json:"alipay_user"`
	AlipayUser     *AlipaySplitUser `gorm:"foreignKey:AlipayUserID" json:"alipay_user_obj,omitempty"`
	Money          int            `gorm:"default:0" json:"money"`
	Percentage     float64        `gorm:"type:decimal(5,2)" json:"percentage"`
	SplitStatus    int            `gorm:"default:0" json:"split_status"`              // 0=待分账 1=分账中 2=已分账 3=分账失败
	SplitType      int            `gorm:"default:0" json:"split_type"`                // 0=分账 1=转账
	Error          string         `gorm:"type:text" json:"error"`
	OrderID        string         `gorm:"size:30;index" json:"order"`
	AlipayProductID *uint         `gorm:"index" json:"alipay_product"`
	IsAsync        bool           `gorm:"default:false" json:"is_async"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (SplitHistory) TableName() string {
	return TablePrefix + "split_history"
}

// ===== TenantCookie 租户Cookie =====

// TenantCookie 租户Cookie/小号库存
type TenantCookie struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	PluginID       uint           `gorm:"index" json:"plugin_id"`
	TenantID       uint           `gorm:"index" json:"tenant_id"`
	Content        string         `gorm:"type:text" json:"content"`                   // Cookie内容(JSON)
	Extra          string         `gorm:"type:text" json:"extra"`                     // 额外信息(JSON)
	Status         bool           `gorm:"default:true" json:"status"`                 // 是否启用
	Remarks        string         `gorm:"size:255" json:"remarks"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (TenantCookie) TableName() string {
	return TablePrefix + "tenant_cookie"
}

// ===== TenantCookieDayStatistics 租户Cookie日统计 =====

// TenantCookieDayStatistics 租户Cookie日统计
type TenantCookieDayStatistics struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	CookieID     uint      `gorm:"index" json:"cookie_id"`
	SubmitCount  int       `gorm:"default:0" json:"submit_count"`
	SuccessCount int       `gorm:"default:0" json:"success_count"`
	SuccessMoney int64     `gorm:"default:0" json:"success_money"`
	Date         time.Time `gorm:"type:date;index" json:"date"`
	Version      int       `gorm:"default:0" json:"-"`
}

func (TenantCookieDayStatistics) TableName() string {
	return TablePrefix + "tenant_cookie_day_statistics"
}
