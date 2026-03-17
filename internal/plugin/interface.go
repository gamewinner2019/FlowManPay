package plugin

import (
	"gorm.io/gorm"
)

// PluginProperties 插件属性配置
type PluginProperties struct {
	Key              string // 插件唯一标识
	NeedCookie       bool   // 是否需要cookie
	NeedProduct      bool   // 是否需要产品
	CouldExtra       bool   // 是否支持额外参数代拉单
	ExtraPayURL      bool   // 是否直接返回支付链接
	ExtraNeedCookie  bool   // 代拉单是否需要cookie
	ExtraNeedProduct bool   // 代拉单是否需要产品
	NeedPayDomain    bool   // 是否需要域名
	AutoExtra        bool   // 是否自动额外参数
	Timeout          int    // 请求超时时间(秒)
	FlowPK           string // 流水主键字段名
}

// DefaultProperties 返回默认属性
func DefaultProperties() PluginProperties {
	return PluginProperties{
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
}

// CallbackArgs 回调参数
type CallbackArgs struct {
	PluginType     string
	ProductID      int
	PluginID       int
	Money          int
	OrderNo        string
	OutOrderNo     string
	OrderID        int
	CookieID       int
	WriteoffID     int
	TenantID       int
	ChannelID      int
	OrderBefore    int // 回调前订单状态
	OrderAfter     int // 回调后订单状态
	PluginUpstream int
	CreateDatetime interface{} // time.Time
	Extra          map[string]interface{}
}

// CreateOrderArgs 创建订单参数
type CreateOrderArgs struct {
	RawOrderNo string
	OrderNo    string
	OutOrderNo string
	ProductID  int
	PluginID   int
	Money      int
	OrderID    int
	TenantID   int
	ChannelID  int
	DomainID   int
	CookieID   int
	IP         string
	Extra      map[string]interface{}
}

// CreateOrderResult 创建订单返回
type CreateOrderResult struct {
	Code int                    `json:"code"`
	Msg  string                 `json:"msg"`
	Data map[string]interface{} `json:"data,omitempty"`
}

// SuccessResult 成功返回
func SuccessResult(payURL string) *CreateOrderResult {
	return &CreateOrderResult{
		Code: 0,
		Msg:  "成功",
		Data: map[string]interface{}{
			"pay_url": payURL,
		},
	}
}

// ErrorResult 错误返回
func ErrorResult(code int, msg string) *CreateOrderResult {
	return &CreateOrderResult{
		Code: code,
		Msg:  msg,
	}
}

// QueryOrderArgs 查询订单参数
type QueryOrderArgs struct {
	OrderNo       string
	QueryInterval int
	Actively      bool
	Remarks       string
	Callback      bool
	Extra         map[string]interface{}
}

// WriteoffProductResult 核销产品选择结果
type WriteoffProductResult struct {
	ProductID  int
	WriteoffID int
	Money      int
}

// WriteoffProductArgs 选择核销产品参数
type WriteoffProductArgs struct {
	WriteoffIDs    []int
	Money          int
	PluginUpstream int
	TenantID       int
	ChannelID      int
	MerchantID     int
	PluginID       int
	OutOrderNo     string
	CookieID       int
	IsReplace      bool
	Channel        interface{} // *model.PayChannel
	Extra          map[string]interface{}
}

// PluginResponder 支付插件接口
// 对应 Django 的 BasePluginResponder
type PluginResponder interface {
	// Properties 获取插件属性
	Properties() PluginProperties

	// CreateOrder 创建订单
	CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error)

	// QueryOrder 查询订单
	QueryOrder(db *gorm.DB, args QueryOrderArgs) (bool, error)

	// CallbackSuccess 支付成功回调
	CallbackSuccess(db *gorm.DB, args CallbackArgs) error

	// CallbackSubmit 订单提交回调（下单成功）
	CallbackSubmit(db *gorm.DB, args CallbackArgs) error

	// CallbackTimeout 订单超时回调
	CallbackTimeout(db *gorm.DB, args CallbackArgs) error

	// CallbackRefund 退款回调
	CallbackRefund(db *gorm.DB, args CallbackArgs) error

	// CheckNotifySuccess 检查回调通知是否表示成功
	CheckNotifySuccess(data map[string]interface{}) bool

	// GetWriteoffProduct 选择核销产品
	GetWriteoffProduct(db *gorm.DB, args WriteoffProductArgs) *WriteoffProductResult

	// GetChannelExtraArgs 获取通道额外参数选项
	GetChannelExtraArgs() []map[string]interface{}
}
