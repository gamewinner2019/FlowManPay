package service

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/sign"
)

// ===== OrderProcessingError 订单处理错误 =====

// OrderProcessingError 订单处理异常
type OrderProcessingError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e *OrderProcessingError) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Msg)
}

// NewOrderProcessingError 创建订单处理异常
func NewOrderProcessingError(code int, msg string) *OrderProcessingError {
	return &OrderProcessingError{Code: code, Msg: msg}
}

// ===== OrderCreateCtx 订单创建上下文 =====

// OrderCreateCtx 创建订单上下文
type OrderCreateCtx struct {
	Compatible  int                // 兼容模式 0=本系统 1=易支付
	OutOrderNo  string             // 商户订单号
	Money       int                // 实际金额(分)
	NotifyMoney int                // 提交金额(分)
	NotifyURL   string             // 通知地址
	Extra       string             // 额外参数
	Test        bool               // 测试模式
	JumpURL     string             // 跳转链接
	ProductID   string             // 产品ID
	CookieID    string             // Cookie ID
	Channel     *model.PayChannel  // 支付通道
	Merchant    *model.Merchant    // 商户
	Writeoff    *model.WriteOff    // 核销
	Domain      *model.PayDomain   // 支付域名
	Checkstand  string             // 收银台URL(无域名记录时)
	SignRaw     string             // 签名原文
	Sign        string             // 签名值
	PluginUpstream model.PluginUpstream // 插件大类
	PayType     *model.PayType     // 支付方式
	Order       *model.Order       // 订单
	Tax         int                // 租户系统费(分)
	MerchantTax int                // 商户手续费(分)
	Detail      *model.OrderDetail // 订单详情
}

// DomainURL 获取收银台URL
func (ctx *OrderCreateCtx) DomainURL() string {
	if ctx.Checkstand != "" {
		return ctx.Checkstand
	}
	if ctx.Domain != nil {
		return ctx.Domain.URL
	}
	return ""
}

// OrderID 获取订单ID
func (ctx *OrderCreateCtx) OrderID() string {
	if ctx.Order != nil {
		return ctx.Order.ID
	}
	return ""
}

// PluginType 获取插件类型标识
func (ctx *OrderCreateCtx) PluginType() string {
	if ctx.PayType != nil {
		return ctx.PayType.Key
	}
	return ""
}

// OrderNo 获取系统订单号
func (ctx *OrderCreateCtx) OrderNo() string {
	if ctx.Order != nil {
		return ctx.Order.OrderNo
	}
	return ""
}

// Tenant 获取租户
func (ctx *OrderCreateCtx) Tenant() *model.Tenant {
	if ctx.Merchant != nil {
		return ctx.Merchant.Parent
	}
	return nil
}

// TenantID 获取租户ID
func (ctx *OrderCreateCtx) TenantID() *uint {
	if t := ctx.Tenant(); t != nil {
		return &t.ID
	}
	return nil
}

// WriteoffID 获取核销ID
func (ctx *OrderCreateCtx) WriteoffID() *uint {
	if ctx.Writeoff != nil {
		return &ctx.Writeoff.ID
	}
	return nil
}

// MerchantID 获取商户ID
func (ctx *OrderCreateCtx) MerchantID() *uint {
	if ctx.Merchant != nil {
		return &ctx.Merchant.ID
	}
	return nil
}

// DomainID 获取域名ID
func (ctx *OrderCreateCtx) DomainID() *uint {
	if ctx.Domain != nil {
		return &ctx.Domain.ID
	}
	return nil
}

// ChannelID 获取通道ID
func (ctx *OrderCreateCtx) ChannelID() *uint {
	if ctx.Channel != nil {
		return &ctx.Channel.ID
	}
	return nil
}

// Plugin 获取插件
func (ctx *OrderCreateCtx) Plugin() *model.PayPlugin {
	if ctx.Channel != nil {
		return ctx.Channel.Plugin
	}
	return nil
}

// PluginID 获取插件ID
func (ctx *OrderCreateCtx) PluginID() *uint {
	if p := ctx.Plugin(); p != nil {
		return &p.ID
	}
	return nil
}

// ===== OrderService 订单服务 =====

// OrderService 订单服务
type OrderService struct {
	DB *gorm.DB
}

// NewOrderService 创建订单服务
func NewOrderService(db *gorm.DB) *OrderService {
	return &OrderService{DB: db}
}

// GetPluginUpstream 获取插件大类
func (s *OrderService) GetPluginUpstream(pluginID uint) model.PluginUpstream {
	var config model.PayPluginConfig
	if err := s.DB.Where("parent_id = ? AND `key` = ?", pluginID, "type").First(&config).Error; err != nil {
		return model.PluginUpstreamOther
	}
	if config.Value == nil {
		return model.PluginUpstreamOther
	}
	val, err := strconv.Atoi(*config.Value)
	if err != nil {
		return model.PluginUpstreamOther
	}
	return model.PluginUpstream(val)
}

// GetPluginPayDomain 获取插件独立域名配置
func (s *OrderService) GetPluginPayDomain(pluginID uint, channelID uint) string {
	var config model.PayPluginConfig
	if err := s.DB.Where("parent_id = ? AND `key` = ?", pluginID, "pay_domain").First(&config).Error; err != nil {
		return ""
	}
	if config.Value == nil || *config.Value == "" {
		return ""
	}
	// 尝试解析为JSON对象
	var domains map[string]string
	if err := json.Unmarshal([]byte(*config.Value), &domains); err == nil {
		channelKey := strconv.FormatUint(uint64(channelID), 10)
		if url, ok := domains[channelKey]; ok {
			return url
		}
		if url, ok := domains["else"]; ok {
			return url
		}
		return ""
	}
	// 非JSON，直接返回值
	return *config.Value
}

// CheckMerchant 检查商户
func (s *OrderService) CheckMerchant(ctx *OrderCreateCtx, merchantID uint) error {
	var merchant model.Merchant
	err := s.DB.Preload("SystemUser").Preload("SystemUser.Role").
		Preload("Parent").Preload("Parent.SystemUser").
		Where("id = ?", merchantID).First(&merchant).Error

	if ctx.Test {
		if err == nil {
			ctx.Merchant = &merchant
		}
		return nil
	}

	if err != nil {
		return NewOrderProcessingError(7301, "商户不存在")
	}
	if !merchant.SystemUser.IsActive {
		return NewOrderProcessingError(7301, "商户不存在")
	}
	if !merchant.SystemUser.Status {
		return NewOrderProcessingError(7302, "商户已被禁用,请联系管理员")
	}
	ctx.Merchant = &merchant
	return nil
}

// CheckTenant 检查租户
func (s *OrderService) CheckTenant(ctx *OrderCreateCtx) error {
	if ctx.Merchant == nil || ctx.Merchant.Parent == nil {
		return NewOrderProcessingError(7302, "商户上级不存在")
	}
	if !ctx.Merchant.Parent.SystemUser.Status {
		return NewOrderProcessingError(7302, "商户上级已被禁用,请联系管理员")
	}
	return nil
}

// CheckSign 检查签名
func (s *OrderService) CheckSign(ctx *OrderCreateCtx, reqData map[string]string) error {
	reqSign := reqData["sign"]
	delete(reqData, "sign")

	if ctx.Compatible == 0 || ctx.Compatible == 1 {
		signRaw, encrypted, err := sign.GetSign(reqData, ctx.Merchant.SystemUser.Key, nil, []string{"extra"}, ctx.Compatible)
		if err != nil {
			return NewOrderProcessingError(7303, err.Error())
		}
		if encrypted != reqSign {
			log.Printf("%s 加密%s, 预期:%s, 实际:%s", ctx.OutOrderNo, signRaw, encrypted, reqSign)
			return NewOrderProcessingError(7304, "签名错误")
		}
		ctx.Sign = encrypted
		ctx.SignRaw = signRaw
		return nil
	}
	return NewOrderProcessingError(7304, "签名错误")
}

// CheckOutOrderNo 检查商户订单号是否重复
// 当 tx 不为 nil 时使用事务内的数据库连接，避免 TOCTOU 竞态条件
func (s *OrderService) CheckOutOrderNo(ctx *OrderCreateCtx, tx *gorm.DB) error {
	if ctx.OutOrderNo == "" {
		return nil
	}
	db := s.DB
	if tx != nil {
		db = tx
	}
	var count int64
	db.Model(&model.Order{}).Where("out_order_no = ?", ctx.OutOrderNo).Count(&count)
	if count > 0 {
		return NewOrderProcessingError(7321, "商户订单号重复")
	}
	return nil
}

// checkMerchantChannel 检查商户通道
func (s *OrderService) checkMerchantChannel(ctx *OrderCreateCtx) error {
	var mc model.MerchantPayChannel
	err := s.DB.Where("merchant_id = ? AND pay_channel_id = ?", *ctx.MerchantID(), *ctx.ChannelID()).First(&mc).Error
	if err != nil {
		return NewOrderProcessingError(7307, "商户通道不存在")
	}
	if !mc.Status {
		return NewOrderProcessingError(7308, "商户通道已被禁用,请联系管理员")
	}
	if mc.Tax != 0 {
		// 商户费率
		ctx.MerchantTax = int(mc.Tax * float64(ctx.Money) / 100)
	}
	// 并发限制检查
	if mc.Limit > 0 {
		var orderCount int64
		oneMinuteAgo := time.Now().Add(-60 * time.Second)
		s.DB.Model(&model.Order{}).
			Where("merchant_id = ? AND pay_channel_id = ? AND create_datetime >= ?",
				*ctx.MerchantID(), *ctx.ChannelID(), oneMinuteAgo).
			Count(&orderCount)
		if int(orderCount) >= mc.Limit {
			return NewOrderProcessingError(7321, "并发数太大，请减少并发量")
		}
	}
	return nil
}

// checkTenantChannel 检查租户通道
func (s *OrderService) checkTenantChannel(ctx *OrderCreateCtx) error {
	var channelTax model.PayChannelTax
	err := s.DB.Where("pay_channel_id = ? AND tenant_id = ?", *ctx.ChannelID(), *ctx.TenantID()).First(&channelTax).Error
	if err != nil {
		return NewOrderProcessingError(7310, "该通道对商户不可用")
	}
	if !channelTax.Status {
		return NewOrderProcessingError(7311, "该通道对商户不可用")
	}
	if channelTax.Tax != 0 {
		// 手续费，四舍五入，最低扣除1分
		tax := int(channelTax.Tax*float64(ctx.Money)/100 + 0.5)
		if tax < 1 {
			tax = 1
		}
		ctx.Tax = tax
	}
	return nil
}

// CheckChannel 检查通道
func (s *OrderService) CheckChannel(ctx *OrderCreateCtx, channelID uint) error {
	var channel model.PayChannel
	if err := s.DB.Preload("Plugin").Preload("Plugin.PayTypes").First(&channel, channelID).Error; err != nil {
		return NewOrderProcessingError(7305, "通道不存在")
	}
	if ctx.Test {
		ctx.Channel = &channel
		return nil
	}
	if !channel.Status {
		return NewOrderProcessingError(7306, "通道已被禁用,请联系管理员")
	}
	ctx.Channel = &channel

	// 检查通道时间
	now := time.Now()
	if channel.StartTime != channel.EndTime && channel.StartTime != "00:00:00" {
		startTime, err1 := time.Parse("15:04:05", channel.StartTime)
		endTime, err2 := time.Parse("15:04:05", channel.EndTime)
		if err1 == nil && err2 == nil {
			currentTime, _ := time.Parse("15:04:05", now.Format("15:04:05"))
			if currentTime.Before(startTime) || currentTime.After(endTime) {
				return NewOrderProcessingError(7309,
					fmt.Sprintf("通道不在可使用时间[%s-%s]", channel.StartTime, channel.EndTime))
			}
		}
	}

	// 通道固定金额模式（在浮动调整前验证基础金额）
	if len(channel.Moneys) > 0 {
		found := false
		for _, m := range channel.Moneys {
			if m == ctx.Money {
				found = true
				break
			}
		}
		if !found {
			return NewOrderProcessingError(7313,
				fmt.Sprintf("金额%d不在范围内", ctx.Money))
		}
	}

	// 增加浮动金额
	if !(channel.FloatMinMoney == 0 && channel.FloatMaxMoney == 0) {
		if channel.FloatMaxMoney > channel.FloatMinMoney {
			ctx.Money += rand.Intn(channel.FloatMaxMoney-channel.FloatMinMoney+1) + channel.FloatMinMoney
		} else {
			ctx.Money += channel.FloatMinMoney
		}
	}

	// 检查金额不能为0或负数
	if ctx.Money <= 0 {
		return NewOrderProcessingError(7312, "金额必须大于0")
	}

	// 检查单笔金额范围
	if channel.MinMoney != channel.MaxMoney && channel.MaxMoney != 0 {
		if ctx.Money < channel.MinMoney || ctx.Money > channel.MaxMoney {
			return NewOrderProcessingError(7313,
				fmt.Sprintf("金额%d不在范围[%d,%d]内", ctx.Money, channel.MinMoney, channel.MaxMoney))
		}
	}

	// 检查商户通道和租户通道（在浮动调整后计算手续费，确保基于最终金额）
	if err := s.checkMerchantChannel(ctx); err != nil {
		return err
	}
	if err := s.checkTenantChannel(ctx); err != nil {
		return err
	}

	return nil
}

// CheckPlugin 检查插件
func (s *OrderService) CheckPlugin(ctx *OrderCreateCtx) error {
	plugin := ctx.Plugin()
	if plugin == nil {
		return NewOrderProcessingError(7316, "该通道不可用")
	}
	if !plugin.Status {
		log.Printf("%s 插件%d未开启", ctx.OutOrderNo, plugin.ID)
		return NewOrderProcessingError(7316, "该通道不可用")
	}
	ctx.PluginUpstream = s.GetPluginUpstream(plugin.ID)

	// 检查支付方式
	if len(plugin.PayTypes) == 0 {
		log.Printf("%s 插件%d没有支付方式", ctx.OutOrderNo, plugin.ID)
		return NewOrderProcessingError(7317, "该通道不可用")
	}
	payType := plugin.PayTypes[0]
	if !payType.Status {
		log.Printf("%s 插件%d的支付方式已关闭", ctx.OutOrderNo, plugin.ID)
		return NewOrderProcessingError(7317, "该通道不可用")
	}
	ctx.PayType = &payType
	return nil
}

// CheckDomain 检查收银台
func (s *OrderService) CheckDomain(ctx *OrderCreateCtx) error {
	plugin := ctx.Plugin()
	if plugin == nil {
		return NewOrderProcessingError(7314, "无可用收银台")
	}

	// 检查插件是否设置了独立域名
	domainURL := s.GetPluginPayDomain(plugin.ID, *ctx.ChannelID())
	if domainURL != "" {
		var dom model.PayDomain
		if err := s.DB.Where("url = ?", domainURL).First(&dom).Error; err == nil {
			ctx.Domain = &dom
			return nil
		}
		ctx.Checkstand = domainURL
		return nil
	}

	// 根据插件类型选择域名
	query := s.DB.Model(&model.PayDomain{}).Where("status = ?", true)
	switch ctx.PluginUpstream {
	case model.PluginUpstreamWechat:
		query = query.Where("wechat_status = ?", true)
	case model.PluginUpstreamAlipay:
		query = query.Where("pay_status = ?", true)
	}

	var domain model.PayDomain
	if err := query.Order("RAND()").First(&domain).Error; err != nil {
		return NewOrderProcessingError(7314, "无可用收银台")
	}
	ctx.Domain = &domain
	return nil
}

// TryCreateOrder 尝试创建订单
func (s *OrderService) TryCreateOrder(ctx *OrderCreateCtx, creatorID *uint, tx ...*gorm.DB) error {
	db := s.DB
	if len(tx) > 0 && tx[0] != nil {
		db = tx[0]
	}

	orderNo := model.CreateOrderNo()
	orderID := model.CreateOrderID()
	log.Printf("%s | 尝试创建订单%s", ctx.OutOrderNo, orderNo)

	productName := ""
	if ctx.Channel != nil {
		productName = fmt.Sprintf("[%d]%s", ctx.Channel.ID, ctx.Channel.Name)
	}

	order := &model.Order{
		ID:           orderID,
		OrderNo:      orderNo,
		OutOrderNo:   ctx.OutOrderNo,
		OrderStatus:  model.OrderStatusInProduction,
		Money:        ctx.Money,
		PayChannelID: ctx.ChannelID(),
		MerchantID:   ctx.MerchantID(),
		Tax:          ctx.Tax,
		ProductName:  productName,
		ReqExtra:     ctx.Extra,
		Compatible:   model.OrderCompatible(ctx.Compatible),
		Creator:      creatorID,
	}

	if err := db.Create(order).Error; err != nil {
		return fmt.Errorf("创建订单失败: %w", err)
	}
	ctx.Order = order
	return nil
}

// TryCreateOrderDetail 创建订单详情
func (s *OrderService) TryCreateOrderDetail(ctx *OrderCreateCtx, creatorID *uint, tx ...*gorm.DB) error {
	db := s.DB
	if len(tx) > 0 && tx[0] != nil {
		db = tx[0]
	}

	detail := &model.OrderDetail{
		OrderID:        ctx.OrderID(),
		NotifyURL:      ctx.NotifyURL,
		JumpURL:        ctx.JumpURL,
		NotifyMoney:    ctx.NotifyMoney,
		ProductID:      ctx.ProductID,
		CookieID:       ctx.CookieID,
		PluginID:       ctx.PluginID(),
		DomainID:       ctx.DomainID(),
		PluginType:     ctx.PluginType(),
		WriteoffID:     ctx.WriteoffID(),
		PluginUpstream: ctx.PluginUpstream,
		MerchantTax:    ctx.MerchantTax,
		Creator:        creatorID,
	}

	if err := db.Create(detail).Error; err != nil {
		return fmt.Errorf("创建订单详情失败: %w", err)
	}
	ctx.Detail = detail
	return nil
}

// CreateOrderLog 创建订单签名日志
func (s *OrderService) CreateOrderLog(ctx *OrderCreateCtx) {
	if ctx.SignRaw == "" && ctx.Sign == "" {
		return
	}
	orderLog := &model.OrderLog{
		OutOrderNo: ctx.OutOrderNo,
		SignRaw:    ctx.SignRaw,
		Sign:       ctx.Sign,
	}
	if err := s.DB.Create(orderLog).Error; err != nil {
		log.Printf("创建订单签名日志失败: %v", err)
	}
}

// ProcessOrderCreation 执行完整的订单创建流程
func (s *OrderService) ProcessOrderCreation(ctx *OrderCreateCtx, merchantID uint, channelID uint, creatorID *uint) error {
	// 1. 检查商户
	if err := s.CheckMerchant(ctx, merchantID); err != nil {
		return err
	}

	// 2. 检查租户
	if err := s.CheckTenant(ctx); err != nil {
		return err
	}

	// 3. 检查商户订单号（传 nil 表示不在事务内）
	if err := s.CheckOutOrderNo(ctx, nil); err != nil {
		return err
	}

	// 4. 检查通道
	if err := s.CheckChannel(ctx, channelID); err != nil {
		return err
	}

	// 5. 检查插件
	if err := s.CheckPlugin(ctx); err != nil {
		return err
	}

	// 6. 检查收银台域名
	if err := s.CheckDomain(ctx); err != nil {
		return err
	}

	// 7. 创建订单
	if err := s.TryCreateOrder(ctx, creatorID); err != nil {
		return err
	}

	// 8. 创建订单详情
	if err := s.TryCreateOrderDetail(ctx, creatorID); err != nil {
		return err
	}

	// 9. 记录签名日志
	s.CreateOrderLog(ctx)

	return nil
}
