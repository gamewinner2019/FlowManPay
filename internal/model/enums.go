package model

// ===== Role Keys =====

const (
	RoleKeyAdmin     = "admin"
	RoleKeyOperation = "operation"
	RoleKeyTenant    = "tenant"
	RoleKeyMerchant  = "merchant"
	RoleKeyWriteoff  = "writeoff"
	RoleKeyCoder     = "coder"
)

// ===== Order Status =====

type OrderStatus int

const (
	OrderStatusInProduction    OrderStatus = 0 // 生成中
	OrderStatusErrorProduction OrderStatus = 1 // 出码失败
	OrderStatusWaitPay         OrderStatus = 2 // 等待支付
	OrderStatusErrorPay        OrderStatus = 3 // 支付失败
	OrderStatusSuccess         OrderStatus = 4 // 支付成功，通知已返回
	OrderStatusRefund          OrderStatus = 5 // 已退款
	OrderStatusSuccessPre      OrderStatus = 6 // 支付成功，通知未返回
	OrderStatusClosed          OrderStatus = 7 // 已关闭
)

func (s OrderStatus) Label() string {
	switch s {
	case OrderStatusInProduction:
		return "生成中"
	case OrderStatusErrorProduction:
		return "出码失败"
	case OrderStatusWaitPay:
		return "等待支付"
	case OrderStatusErrorPay:
		return "支付失败"
	case OrderStatusSuccess:
		return "支付成功，通知已返回"
	case OrderStatusRefund:
		return "已退款"
	case OrderStatusSuccessPre:
		return "支付成功，通知未返回"
	case OrderStatusClosed:
		return "已关闭"
	default:
		return "未知"
	}
}

// ===== Device Type =====

type DeviceType int

const (
	DeviceTypeUnknown DeviceType = 0
	DeviceTypeAndroid DeviceType = 1
	DeviceTypeIOS     DeviceType = 2
	DeviceTypePC      DeviceType = 4
)

func (d DeviceType) Label() string {
	switch d {
	case DeviceTypeUnknown:
		return "未知设备"
	case DeviceTypeAndroid:
		return "Android"
	case DeviceTypeIOS:
		return "IOS"
	case DeviceTypePC:
		return "PC"
	default:
		return "未知"
	}
}

// ===== Support Device Type =====

type SupportDeviceType int

const (
	SupportDeviceAndroid       SupportDeviceType = 1
	SupportDeviceIOS           SupportDeviceType = 2
	SupportDeviceAndroidIOS    SupportDeviceType = 3
	SupportDevicePC            SupportDeviceType = 4
	SupportDeviceAndroidPC     SupportDeviceType = 5
	SupportDeviceIOSPC         SupportDeviceType = 6
	SupportDeviceAndroidIOSPC  SupportDeviceType = 7
)

func (s SupportDeviceType) SupportsIOS() bool {
	return s == 2 || s == 3 || s == 6 || s == 7
}

func (s SupportDeviceType) SupportsAndroid() bool {
	return s == 1 || s == 3 || s == 5 || s == 7
}

func (s SupportDeviceType) SupportsPC() bool {
	return s == 4 || s == 5 || s == 6 || s == 7
}

// ===== Order Compatibles =====

type OrderCompatible int

const (
	OrderCompatibleGooglePay OrderCompatible = 0 // 本系统
	OrderCompatibleYiPay     OrderCompatible = 1 // 易支付
)

// ===== Tenant Cash Flow Type =====

type TenantCashFlowType int

const (
	TenantCashFlowCommission   TenantCashFlowType = 1 // 手续费
	TenantCashFlowRecharge     TenantCashFlowType = 2 // 充值
	TenantCashFlowAdjust       TenantCashFlowType = 3 // 调额
	TenantCashFlowOrderRefunds TenantCashFlowType = 4 // 订单退款
)

func (t TenantCashFlowType) Label() string {
	switch t {
	case TenantCashFlowCommission:
		return "手续费"
	case TenantCashFlowRecharge:
		return "充值"
	case TenantCashFlowAdjust:
		return "调额"
	case TenantCashFlowOrderRefunds:
		return "订单退款"
	default:
		return "未知类型"
	}
}

// ===== Writeoff Cash Flow Type =====

type WriteoffCashFlowType int

const (
	WriteoffCashFlowCommission   WriteoffCashFlowType = 1 // 手续费
	WriteoffCashFlowRecharge     WriteoffCashFlowType = 2 // 充值
	WriteoffCashFlowAdjust       WriteoffCashFlowType = 3 // 调额
	WriteoffCashFlowOrderRefunds WriteoffCashFlowType = 4 // 退款
	WriteoffCashFlowSend         WriteoffCashFlowType = 5 // 下发
	WriteoffCashFlowTransfer     WriteoffCashFlowType = 6 // 转赠
	WriteoffCashFlowBrokerage    WriteoffCashFlowType = 7 // 佣金转余额
)

func (w WriteoffCashFlowType) Label() string {
	switch w {
	case WriteoffCashFlowCommission:
		return "手续费"
	case WriteoffCashFlowRecharge:
		return "充值"
	case WriteoffCashFlowAdjust:
		return "调额"
	case WriteoffCashFlowOrderRefunds:
		return "退款"
	case WriteoffCashFlowSend:
		return "下发"
	case WriteoffCashFlowTransfer:
		return "转赠"
	case WriteoffCashFlowBrokerage:
		return "佣金转余额"
	default:
		return "未知类型"
	}
}

// ===== Plugin Upstream =====

type PluginUpstream int

const (
	PluginUpstreamUnknown       PluginUpstream = -1
	PluginUpstreamOther         PluginUpstream = 0
	PluginUpstreamPhone         PluginUpstream = 1 // 三网话费
	PluginUpstreamStrategicGood PluginUpstream = 2 // 战略物资
	PluginUpstreamOilGun        PluginUpstream = 3 // 加油站油枪
	PluginUpstreamCardKey       PluginUpstream = 4 // 卡密
	PluginUpstreamAlipay        PluginUpstream = 5 // 支付宝官方
	PluginUpstreamWechat        PluginUpstream = 6 // 微信官方
	PluginUpstreamETC           PluginUpstream = 7 // ETC
	PluginUpstreamHouse         PluginUpstream = 8 // 国网电费
)

func (p PluginUpstream) Label() string {
	switch p {
	case PluginUpstreamUnknown:
		return "未知"
	case PluginUpstreamOther:
		return "其他"
	case PluginUpstreamPhone:
		return "三网话费"
	case PluginUpstreamStrategicGood:
		return "战略物资"
	case PluginUpstreamOilGun:
		return "加油站油枪"
	case PluginUpstreamCardKey:
		return "卡密"
	case PluginUpstreamAlipay:
		return "支付宝官方"
	case PluginUpstreamWechat:
		return "微信官方"
	case PluginUpstreamETC:
		return "ETC"
	case PluginUpstreamHouse:
		return "国网电费"
	default:
		return "未知"
	}
}

// ===== Gender =====

type Gender int

const (
	GenderFemale  Gender = 0
	GenderMale    Gender = 1
	GenderUnknown Gender = 2
)

func (g Gender) Label() string {
	switch g {
	case GenderFemale:
		return "女"
	case GenderMale:
		return "男"
	case GenderUnknown:
		return "未知"
	default:
		return "未知"
	}
}

// ===== Agent Payment Order Status =====

type AgentPaymentOrderStatus int

const (
	AgentPaymentWait    AgentPaymentOrderStatus = 0 // 等待拉取
	AgentPaymentIng     AgentPaymentOrderStatus = 1 // 支付中
	AgentPaymentFail    AgentPaymentOrderStatus = 2 // 支付失败
	AgentPaymentSuccess AgentPaymentOrderStatus = 3 // 支付成功
)

// ===== Weibo Account Status =====

type WeiboAccountStatus int

const (
	WeiboAccountOffline WeiboAccountStatus = -1
	WeiboAccountClosed  WeiboAccountStatus = 0
	WeiboAccountNormal  WeiboAccountStatus = 1
)

// ===== Common Recharge Shop Type =====

type CommonRechargeShopType int

const (
	CommonRechargeBiliBili CommonRechargeShopType = 0
	CommonRechargeDouYin   CommonRechargeShopType = 1
	CommonRechargeBaoXue   CommonRechargeShopType = 2
	CommonRechargePuPu     CommonRechargeShopType = 3
	CommonRechargeGDHF     CommonRechargeShopType = 4
	CommonRechargeZGSY     CommonRechargeShopType = 5
	CommonRechargeZGSH     CommonRechargeShopType = 6
	CommonRechargeXJZ      CommonRechargeShopType = 7
	CommonRechargeWFC      CommonRechargeShopType = 8
	CommonRechargeMQ       CommonRechargeShopType = 9
)
