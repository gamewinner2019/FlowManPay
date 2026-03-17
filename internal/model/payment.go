package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ===== JSON 辅助类型 =====

// JSONStringSlice 用于存储JSON数组字段(如ban_ip, moneys)
type JSONStringSlice []string

func (j JSONStringSlice) Value() (driver.Value, error) {
	if j == nil {
		return "[]", nil
	}
	data, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (j *JSONStringSlice) Scan(value interface{}) error {
	if value == nil {
		*j = JSONStringSlice{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		return fmt.Errorf("无法将 %T 转换为 JSONStringSlice", value)
	}
	return json.Unmarshal(bytes, j)
}

// JSONIntSlice 用于存储JSON整数数组字段(如moneys)
type JSONIntSlice []int

func (j JSONIntSlice) Value() (driver.Value, error) {
	if j == nil {
		return "[]", nil
	}
	data, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (j *JSONIntSlice) Scan(value interface{}) error {
	if value == nil {
		*j = JSONIntSlice{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		return fmt.Errorf("无法将 %T 转换为 JSONIntSlice", value)
	}
	return json.Unmarshal(bytes, j)
}

// JSONMap 用于存储JSON对象字段
type JSONMap map[string]interface{}

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return "{}", nil
	}
	data, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = JSONMap{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		return fmt.Errorf("无法将 %T 转换为 JSONMap", value)
	}
	return json.Unmarshal(bytes, j)
}

// ===== PayType 支付方式 =====

// PayType 支付方式 (如支付宝、微信等)
type PayType struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Name           string         `gorm:"size:64;uniqueIndex" json:"name"`           // 支付方式名称
	Key            string         `gorm:"size:64;uniqueIndex" json:"key"`            // 支付方式标识
	Status         bool           `gorm:"default:true" json:"status"`                // 是否启用
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PayType) TableName() string {
	return TablePrefix + "pay_type"
}

// ===== PayPlugin 支付插件 =====

// FormItemType 表单项类型
type FormItemType int

const (
	FormItemTypeInput    FormItemType = 0  // 输入框
	FormItemTypeTextArea FormItemType = 3  // 文本域
	FormItemTypeSelect   FormItemType = 4  // 选择框
	FormItemTypeNumber   FormItemType = 10 // 数字输入框
	FormItemTypeJSON     FormItemType = 16 // JSON编辑器
)

// PayPlugin 支付插件
type PayPlugin struct {
	ID             uint              `gorm:"primaryKey" json:"id"`
	Name           string            `gorm:"size:64;uniqueIndex" json:"name"`          // 插件名称
	Description    string            `gorm:"type:text" json:"description"`              // 插件描述
	Status         bool              `gorm:"default:false" json:"status"`               // 是否启用
	CanDivide      bool              `gorm:"default:false" json:"can_divide"`           // 是否支持分账
	CanTransfer    bool              `gorm:"default:false" json:"can_transfer"`         // 是否支持转账
	SupportDevice  SupportDeviceType `gorm:"default:7" json:"support_device"`           // 支持设备类型
	PayTypes       []PayType         `gorm:"many2many:dvadmin_pay_plugin_pay_types;" json:"pay_types,omitempty"` // 关联支付方式
	Menus          []Menu            `gorm:"many2many:dvadmin_pay_plugin_menus;" json:"menus,omitempty"`
	Creator        *uint             `gorm:"index" json:"creator"`
	Modifier       *uint             `json:"modifier"`
	CreateDatetime time.Time         `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time         `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt    `gorm:"index" json:"-"`
}

func (PayPlugin) TableName() string {
	return TablePrefix + "pay_plugin"
}

// ===== PayPluginConfig 插件配置 =====

// PayPluginConfig 支付插件配置(KV对)
type PayPluginConfig struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	ParentID       uint           `gorm:"index" json:"parent_id"`                  // 关联插件
	Parent         *PayPlugin     `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Title          string         `gorm:"size:50" json:"title"`                    // 配置标题
	Key            string         `gorm:"size:20;index" json:"key"`                // 配置键名
	Value          *string        `gorm:"type:json" json:"value"`                  // 配置值(JSON)
	Sort           int            `gorm:"default:0" json:"sort"`                   // 排序
	Status         bool           `gorm:"default:true" json:"status"`              // 是否启用
	DataOptions    *string        `gorm:"type:json" json:"data_options"`           // 数据选项
	FormItemType   FormItemType   `gorm:"default:0" json:"form_item_type"`         // 表单项类型
	Rule           *string        `gorm:"type:json" json:"rule"`                   // 校验规则
	Placeholder    string         `gorm:"size:50" json:"placeholder"`              // 占位文本
	Setting        *string        `gorm:"type:json" json:"setting"`                // 设置项
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PayPluginConfig) TableName() string {
	return TablePrefix + "pay_plugin_config"
}

// ===== PayChannel 支付通道 =====

// PayChannel 支付通道
type PayChannel struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Name           string         `gorm:"size:64;uniqueIndex" json:"name"`          // 通道名称
	Status         bool           `gorm:"default:true" json:"status"`               // 是否启用
	PluginID       uint           `gorm:"index" json:"plugin_id"`                   // 关联插件
	Plugin         *PayPlugin     `gorm:"foreignKey:PluginID" json:"plugin,omitempty"`
	MaxMoney       int            `gorm:"default:0" json:"max_money"`               // 最大金额(分)
	MinMoney       int            `gorm:"default:0" json:"min_money"`               // 最小金额(分)
	FloatMaxMoney  int            `gorm:"default:0" json:"float_max_money"`         // 浮动最大金额(分)
	FloatMinMoney  int            `gorm:"default:0" json:"float_min_money"`         // 浮动最小金额(分)
	Settled        bool           `gorm:"default:false" json:"settled"`             // 固定金额模式
	Moneys         JSONIntSlice   `gorm:"type:json" json:"moneys"`                  // 固定金额列表
	StartTime      string         `gorm:"size:20;default:'00:00:00'" json:"start_time"` // 开始时间
	EndTime        string         `gorm:"size:20;default:'00:00:00'" json:"end_time"`   // 结束时间
	ExtraArg       *int           `json:"extra_arg"`                                // 额外参数
	BanIP          JSONStringSlice `gorm:"type:json" json:"ban_ip"`                 // 封禁IP列表
	Logo           string         `gorm:"type:text" json:"logo"`                    // Logo
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PayChannel) TableName() string {
	return TablePrefix + "pay_channel"
}

// ===== PayChannelTax 通道费率 =====

// PayChannelTax 支付通道费率(租户维度)
type PayChannelTax struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	PayChannelID   uint           `gorm:"index" json:"pay_channel_id"`              // 关联通道
	PayChannel     *PayChannel    `gorm:"foreignKey:PayChannelID" json:"pay_channel,omitempty"`
	TenantID       uint           `gorm:"index" json:"tenant_id"`                   // 关联租户
	Tenant         *Tenant        `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
	Tax            float64        `gorm:"type:decimal(5,2)" json:"tax"`             // 费率
	Mark           string         `gorm:"size:100;uniqueIndex" json:"mark"`         // 唯一标识
	Status         bool           `gorm:"default:true" json:"status"`               // 是否启用
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PayChannelTax) TableName() string {
	return TablePrefix + "pay_channel_tax"
}

// ===== MerchantPayChannel 商户支付通道 =====

// MerchantPayChannel 商户支付通道
type MerchantPayChannel struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	MerchantID     uint           `gorm:"index" json:"merchant_id"`
	Merchant       *Merchant      `gorm:"foreignKey:MerchantID" json:"merchant,omitempty"`
	PayChannelID   uint           `gorm:"index" json:"pay_channel_id"`
	PayChannel     *PayChannel    `gorm:"foreignKey:PayChannelID" json:"pay_channel,omitempty"`
	Status         bool           `gorm:"default:true" json:"status"`
	Tax            float64        `gorm:"type:decimal(5,2);default:0" json:"tax"`   // 商户费率
	Limit          int            `gorm:"default:0" json:"limit"`                   // 并发限制(0=不限)
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (MerchantPayChannel) TableName() string {
	return TablePrefix + "merchant_pay_channel"
}

// ===== WriteoffPayChannel 核销支付通道 =====

// WriteoffPayChannel 核销支付通道
type WriteoffPayChannel struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	WriteoffID     uint           `gorm:"index" json:"writeoff_id"`
	Writeoff       *WriteOff      `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	PayChannelID   uint           `gorm:"index" json:"pay_channel_id"`
	PayChannel     *PayChannel    `gorm:"foreignKey:PayChannelID" json:"pay_channel,omitempty"`
	Status         bool           `gorm:"default:true" json:"status"`
	Tax            float64        `gorm:"type:decimal(5,2);default:0" json:"tax"`   // 核销费率
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (WriteoffPayChannel) TableName() string {
	return TablePrefix + "writeoff_pay_channel"
}

// ===== PayDomain 支付域名 =====

// SignType 签名类型
type SignType int

const (
	SignTypeKey  SignType = 0 // 密钥
	SignTypeCert SignType = 1 // 证书
)

// PayDomain 支付域名(RSA签名管理)
type PayDomain struct {
	ID               uint           `gorm:"primaryKey" json:"id"`
	URL              string         `gorm:"size:255;uniqueIndex" json:"url"`          // 域名URL
	AppID            string         `gorm:"size:255" json:"app_id"`                   // 应用ID
	Status           bool           `gorm:"default:true" json:"status"`               // 是否启用
	PayStatus        bool           `gorm:"default:false" json:"pay_status"`          // 支付宝是否可用
	WechatStatus     bool           `gorm:"default:false" json:"wechat_status"`       // 微信是否可用
	SignType         SignType       `gorm:"default:0" json:"sign_type"`               // 签名类型
	PublicKey        string         `gorm:"type:text" json:"public_key"`              // 公钥
	PrivateKey       string         `gorm:"type:text" json:"private_key"`             // 私钥
	AppPublicCrt     string         `gorm:"type:text" json:"app_public_crt"`          // 应用公钥证书
	AlipayPublicCrt  string         `gorm:"type:text" json:"alipay_public_crt"`       // 支付宝公钥证书
	AlipayRootCrt    string         `gorm:"type:text" json:"alipay_root_crt"`         // 支付宝根证书
	AuthStatus       bool           `gorm:"default:true" json:"auth_status"`          // 授权状态
	AuthTimeout      int            `gorm:"default:0" json:"auth_timeout"`            // 授权超时(秒)
	AuthKey          string         `gorm:"size:255" json:"auth_key"`                 // 授权密钥
	Description      string         `gorm:"size:255;default:''" json:"description"`
	Creator          *uint          `gorm:"index" json:"creator"`
	Modifier         *uint          `json:"modifier"`
	CreateDatetime   time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime   time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PayDomain) TableName() string {
	return TablePrefix + "pay_domain"
}

// ===== RechargeHistory USDT充值记录 =====

// RechargeHistory USDT充值记录
type RechargeHistory struct {
	ID             string         `gorm:"size:30;primaryKey" json:"id"`              // 充值单号
	UserID         *uint          `gorm:"index" json:"user_id"`                      // 关联用户
	User           *Users         `gorm:"foreignKey:UserID" json:"user,omitempty"`
	ExchangeRates  int            `json:"exchange_rates"`                             // 汇率
	CNYAmount      int64          `json:"cny_amount"`                                 // 人民币金额(分)
	USDTAmount     int64          `json:"usdt_amount"`                                // USDT金额
	PayHash        string         `gorm:"size:100" json:"pay_hash"`                  // 支付哈希
	PaymentAddress string         `gorm:"size:100" json:"payment_address"`           // 付款地址
	PayeeAddress   string         `gorm:"size:100" json:"payee_address"`             // 收款地址
	Version        int            `gorm:"default:0" json:"-"`                        // 乐观锁
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (RechargeHistory) TableName() string {
	return TablePrefix + "recharge_history"
}

// ===== ProductTax 产品预占 =====

// ProductTax 产品预占金额
type ProductTax struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	ProductID string `gorm:"size:255" json:"product_id"`    // 产品ID
	PluginKey string `gorm:"size:255" json:"plugin_key"`    // 插件标识
	ExtraID   int    `gorm:"default:0" json:"extra_id"`     // 额外ID
	Version   int    `gorm:"default:0" json:"-"`            // 乐观锁
	PreTax    int64  `gorm:"default:0" json:"pre_tax"`      // 预占金额
}

func (ProductTax) TableName() string {
	return TablePrefix + "product_tax"
}
