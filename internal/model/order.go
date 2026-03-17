package model

import (
	"fmt"
	"math/rand"
	"time"

	"gorm.io/gorm"
)

// CreateOrderID 生成订单ID (格式: yyyyMMddHHmmssSSS + 4位随机数)
func CreateOrderID() string {
	now := time.Now()
	return fmt.Sprintf("%s%03d%04d", now.Format("20060102150405"), now.Nanosecond()/1e6, rand.Intn(10000))
}

// CreateOrderNo 生成系统订单号 (格式: G + yyyyMMddHHmmssSSSSSSS + 4位随机数)
func CreateOrderNo() string {
	now := time.Now()
	return fmt.Sprintf("G%s%06d%04d", now.Format("20060102150405"), now.Nanosecond()/1e3, rand.Intn(10000))
}

// CreateRechargeNo 生成充值单号
func CreateRechargeNo() string {
	now := time.Now()
	return fmt.Sprintf("R%s%06d%04d", now.Format("20060102150405"), now.Nanosecond()/1e3, rand.Intn(10000))
}

// ===== Order 订单 =====

// Order 订单主表
type Order struct {
	ID             string         `gorm:"size:30;primaryKey" json:"id"`                // 订单ID
	OrderNo        string         `gorm:"size:32;uniqueIndex" json:"order_no"`         // 系统订单号
	OutOrderNo     string         `gorm:"size:32;uniqueIndex" json:"out_order_no"`     // 商户订单号
	OrderStatus    OrderStatus    `gorm:"default:0;index" json:"order_status"`         // 订单状态
	Money          int            `gorm:"default:0" json:"money"`                      // 金额(分)
	PayChannelID   *uint          `gorm:"index" json:"pay_channel_id"`                 // 支付通道
	PayChannel     *PayChannel    `gorm:"foreignKey:PayChannelID" json:"pay_channel,omitempty"`
	MerchantID     *uint          `gorm:"index" json:"merchant_id"`                    // 商户
	Merchant       *Merchant      `gorm:"foreignKey:MerchantID" json:"merchant,omitempty"`
	Tax            int            `gorm:"default:0" json:"tax"`                        // 系统手续费(分)
	PayDatetime    *time.Time     `json:"pay_datetime"`                                // 支付时间
	ProductName    string         `gorm:"size:255" json:"product_name"`                // 产品名称
	ReqExtra       string         `gorm:"type:text" json:"req_extra"`                  // 请求额外参数
	Compatible     OrderCompatible `gorm:"default:0" json:"compatible"`                // 兼容模式
	Version        int            `gorm:"default:0" json:"-"`                          // 乐观锁
	Remarks        string         `gorm:"type:text" json:"remarks"`                   // 备注
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Order) TableName() string {
	return TablePrefix + "order"
}

// SuccessPreStatus 支付成功但通知未返回的状态
const OrderSuccessPreStatus = 6

// SuccessStatus 支付成功且通知已返回的状态
const OrderSuccessStatus = 4

// ===== OrderDetail 订单详情 =====

// OrderDetail 订单详情
type OrderDetail struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	OrderID        string         `gorm:"size:30;uniqueIndex" json:"order_id"`         // 关联订单
	Order          *Order         `gorm:"foreignKey:OrderID" json:"order,omitempty"`
	NotifyURL      string         `gorm:"type:text" json:"notify_url"`                 // 通知地址
	JumpURL        string         `gorm:"type:text" json:"jump_url"`                   // 跳转地址
	ProductID      string         `gorm:"size:255;index" json:"product_id"`            // 产品ID
	CookieID       string         `gorm:"size:255" json:"cookie_id"`                   // Cookie ID
	PluginID       *uint          `json:"plugin_id"`                                   // 关联插件
	Plugin         *PayPlugin     `gorm:"foreignKey:PluginID" json:"plugin,omitempty"`
	NotifyMoney    int            `gorm:"default:0" json:"notify_money"`               // 通知金额(分)
	TicketNo       string         `gorm:"size:255" json:"ticket_no"`                   // 上游票据号
	QueryNo        string         `gorm:"size:255" json:"query_no"`                   // 查询号
	PluginType     string         `gorm:"size:255" json:"plugin_type"`                 // 插件类型标识
	WriteoffID     *uint          `json:"writeoff_id"`                                 // 关联核销
	Writeoff       *WriteOff      `gorm:"foreignKey:WriteoffID" json:"writeoff,omitempty"`
	PluginUpstream PluginUpstream `gorm:"default:-1;index" json:"plugin_upstream"`     // 插件大类
	MerchantTax    int            `gorm:"default:0" json:"merchant_tax"`               // 商户手续费(分)
	Extra          JSONMap        `gorm:"type:json" json:"extra"`                      // 额外信息
	Remarks        string         `gorm:"type:text" json:"remarks"`                   // 备注(常存支付链接)
	DomainID       *uint          `json:"domain_id"`                                   // 关联域名
	Domain         *PayDomain     `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (OrderDetail) TableName() string {
	return TablePrefix + "order_detail"
}

// ===== OrderDeviceDetails 订单设备信息 =====

// OrderDeviceDetails 订单设备详情
type OrderDeviceDetails struct {
	ID                uint           `gorm:"primaryKey" json:"id"`
	IPAddress         string         `gorm:"size:255;index" json:"ip_address"`         // IP地址
	Address           string         `gorm:"size:32" json:"address"`                   // 地址(省市)
	DeviceType        DeviceType     `gorm:"default:0;index" json:"device_type"`       // 设备类型
	DeviceFingerprint string         `gorm:"size:255" json:"device_fingerprint"`       // 设备指纹
	OrderID           string         `gorm:"size:30;uniqueIndex" json:"order_id"`      // 关联订单
	Order             *Order         `gorm:"foreignKey:OrderID" json:"order,omitempty"`
	PID               int            `gorm:"default:-1" json:"pid"`                    // 省份ID
	CID               int            `gorm:"default:-1" json:"cid"`                    // 城市ID
	UserID            string         `gorm:"size:32;index" json:"user_id"`             // 用户ID
	Description       string         `gorm:"size:255;default:''" json:"description"`
	Creator           *uint          `gorm:"index" json:"creator"`
	Modifier          *uint          `json:"modifier"`
	CreateDatetime    time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime    time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (OrderDeviceDetails) TableName() string {
	return TablePrefix + "order_device_details"
}

// ===== ReOrder 重新支付 =====

// ReOrder 重新支付记录
type ReOrder struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	OrderID        string         `gorm:"size:30;index" json:"order_id"`              // 关联订单
	Order          *Order         `gorm:"foreignKey:OrderID" json:"order,omitempty"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ReOrder) TableName() string {
	return TablePrefix + "re_order"
}

// ===== OrderLog 订单签名日志 =====

// OrderLog 订单签名日志
type OrderLog struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	OutOrderNo     string         `gorm:"size:32;index" json:"out_order_no"`          // 商户订单号
	SignRaw        string         `gorm:"type:text" json:"sign_raw"`                  // 签名原文
	Sign           string         `gorm:"type:text" json:"sign"`                      // 签名结果
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (OrderLog) TableName() string {
	return TablePrefix + "order_log"
}

// ===== QueryLog 查单日志 =====

// QueryLog 查单日志
type QueryLog struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	OrderNo        string         `gorm:"size:32;index" json:"order_no"`              // 系统订单号
	URL            string         `gorm:"type:text" json:"url"`                       // 请求地址
	RequestBody    string         `gorm:"type:text" json:"request_body"`              // 请求参数
	RequestMethod  string         `gorm:"size:8" json:"request_method"`               // 请求方式
	ResponseCode   int            `gorm:"default:-1" json:"response_code"`            // 响应状态码
	JSONResult     string         `gorm:"type:text" json:"json_result"`               // 返回信息
	Remarks        string         `gorm:"type:text" json:"remarks"`                   // 备注
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (QueryLog) TableName() string {
	return TablePrefix + "query_log"
}
