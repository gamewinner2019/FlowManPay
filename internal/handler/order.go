package handler

import (
	"log"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
	"github.com/gamewinner2019/FlowManPay/internal/plugin"
	"github.com/gamewinner2019/FlowManPay/internal/service"
)

// OrderHandler 订单处理器
type OrderHandler struct {
	DB                  *gorm.DB
	OrderService        *service.OrderService
	NotificationFactory *service.NotificationFactory
	StatisticsService   *service.StatisticsService
	CashFlowService     *service.CashFlowService
}

// NewOrderHandler 创建订单处理器
func NewOrderHandler(db *gorm.DB) *OrderHandler {
	return &OrderHandler{
		DB:                  db,
		OrderService:        service.NewOrderService(db),
		NotificationFactory: service.NewNotificationFactory(db),
		StatisticsService:   service.NewStatisticsService(db),
		CashFlowService:     service.NewCashFlowService(db),
	}
}

// CreateOrder 创建订单
// POST /api/pay/create/
func (h *OrderHandler) CreateOrder(c *gin.Context) {
	var req struct {
		MchID      uint   `json:"mchId" form:"mchId" binding:"required"`           // 商户ID
		ChannelID  uint   `json:"channelId" form:"channelId" binding:"required"`    // 通道ID
		MchOrderNo string `json:"mchOrderNo" form:"mchOrderNo" binding:"required"` // 商户订单号
		Amount     int    `json:"amount" form:"amount" binding:"required"`          // 金额(分)
		NotifyURL  string `json:"notifyUrl" form:"notifyUrl" binding:"required"`    // 通知地址
		JumpURL    string `json:"jumpUrl" form:"jumpUrl"`                           // 跳转地址
		Sign       string `json:"sign" form:"sign" binding:"required"`             // 签名
		Extra      string `json:"extra" form:"extra"`                              // 额外参数
		ProductID  string `json:"productId" form:"productId"`                      // 产品ID
		CookieID   string `json:"cookieId" form:"cookieId"`                        // Cookie ID
		Compatible int    `json:"compatible" form:"compatible"`                    // 兼容模式
	}
	if err := c.ShouldBind(&req); err != nil {
		response.ErrorResponseWithCode(c, 7300, "参数错误: "+err.Error())
		return
	}

	// 构建签名验证数据
	reqData := map[string]string{
		"mchId":      strconv.FormatUint(uint64(req.MchID), 10),
		"channelId":  strconv.FormatUint(uint64(req.ChannelID), 10),
		"mchOrderNo": req.MchOrderNo,
		"amount":     strconv.Itoa(req.Amount),
		"notifyUrl":  req.NotifyURL,
		"jumpUrl":    req.JumpURL,
		"sign":       req.Sign,
		"extra":      req.Extra,
	}

	// 创建订单上下文
	ctx := &service.OrderCreateCtx{
		Compatible:  req.Compatible,
		OutOrderNo:  req.MchOrderNo,
		Money:       req.Amount,
		NotifyMoney: req.Amount,
		NotifyURL:   req.NotifyURL,
		Extra:       req.Extra,
		JumpURL:     req.JumpURL,
		ProductID:   req.ProductID,
		CookieID:    req.CookieID,
		Test:        false,
	}

	// 执行订单创建流程（包括签名验证）
	// 1. 检查商户
	if err := h.OrderService.CheckMerchant(ctx, req.MchID); err != nil {
		if ope, ok := err.(*service.OrderProcessingError); ok {
			response.ErrorResponseWithCode(c, ope.Code, ope.Msg)
			return
		}
		response.ErrorResponse(c, err.Error())
		return
	}

	// 2. 检查租户
	if err := h.OrderService.CheckTenant(ctx); err != nil {
		if ope, ok := err.(*service.OrderProcessingError); ok {
			response.ErrorResponseWithCode(c, ope.Code, ope.Msg)
			return
		}
		response.ErrorResponse(c, err.Error())
		return
	}

	// 3. 验证签名
	if err := h.OrderService.CheckSign(ctx, reqData); err != nil {
		if ope, ok := err.(*service.OrderProcessingError); ok {
			response.ErrorResponseWithCode(c, ope.Code, ope.Msg)
			return
		}
		response.ErrorResponse(c, err.Error())
		return
	}

	// 4. 检查商户订单号
	if err := h.OrderService.CheckOutOrderNo(ctx); err != nil {
		if ope, ok := err.(*service.OrderProcessingError); ok {
			response.ErrorResponseWithCode(c, ope.Code, ope.Msg)
			return
		}
		response.ErrorResponse(c, err.Error())
		return
	}

	// 5. 检查通道
	if err := h.OrderService.CheckChannel(ctx, req.ChannelID); err != nil {
		if ope, ok := err.(*service.OrderProcessingError); ok {
			response.ErrorResponseWithCode(c, ope.Code, ope.Msg)
			return
		}
		response.ErrorResponse(c, err.Error())
		return
	}

	// 6. 检查插件
	if err := h.OrderService.CheckPlugin(ctx); err != nil {
		if ope, ok := err.(*service.OrderProcessingError); ok {
			response.ErrorResponseWithCode(c, ope.Code, ope.Msg)
			return
		}
		response.ErrorResponse(c, err.Error())
		return
	}

	// 7. 检查收银台域名
	if err := h.OrderService.CheckDomain(ctx); err != nil {
		if ope, ok := err.(*service.OrderProcessingError); ok {
			response.ErrorResponseWithCode(c, ope.Code, ope.Msg)
			return
		}
		response.ErrorResponse(c, err.Error())
		return
	}

	// 8. 创建订单 + 订单详情（事务保证原子性）
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := h.OrderService.TryCreateOrder(ctx, nil, tx); err != nil {
			return err
		}
		if err := h.OrderService.TryCreateOrderDetail(ctx, nil, tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	// 9. 记录签名日志（事务提交后）
	h.OrderService.CreateOrderLog(ctx)

	// 10. 更新统计（事务提交后）
	h.StatisticsService.SubmitDayStatistics(ctx.TenantID(), ctx.MerchantID(), ctx.WriteoffID(), ctx.ChannelID())

	// 11. 调用插件创建订单
	var payURL string
	pluginType := ctx.PluginType()
	responder := plugin.GetByKey(pluginType)
	if responder != nil {
		// 构建插件参数
		pluginArgs := plugin.CreateOrderArgs{
			RawOrderNo: ctx.OrderID(),
			OrderNo:    ctx.OrderNo(),
			OutOrderNo: ctx.OutOrderNo,
			Money:      ctx.Money,
			OrderID:    0,
		}
		if ctx.PluginID() != nil {
			pluginArgs.PluginID = int(*ctx.PluginID())
		}
		if ctx.TenantID() != nil {
			pluginArgs.TenantID = int(*ctx.TenantID())
		}
		if ctx.ChannelID() != nil {
			pluginArgs.ChannelID = int(*ctx.ChannelID())
		}
		if ctx.Detail != nil {
			pluginArgs.ProductID, _ = strconv.Atoi(ctx.Detail.ProductID)
			pluginArgs.CookieID, _ = strconv.Atoi(ctx.Detail.CookieID)
		}
		if ctx.DomainID() != nil {
			pluginArgs.DomainID = int(*ctx.DomainID())
		}

		// 调用插件创建订单（生成支付URL）
		createResult, createErr := responder.CreateOrder(h.DB, pluginArgs)
		if createErr != nil {
			log.Printf("%s | 插件创建订单失败: %v", ctx.OutOrderNo, createErr)
		} else if createResult != nil && createResult.Code == 0 && createResult.Data != nil {
			if url, ok := createResult.Data["pay_url"]; ok {
				if s, ok := url.(string); ok {
					payURL = s
				}
			}
		}

		// 触发插件提交回调
		cbArgs := plugin.CallbackArgs{
			PluginType: pluginType,
			OrderNo:    ctx.OrderNo(),
			OutOrderNo: ctx.OutOrderNo,
			Money:      ctx.Money,
		}
		if ctx.PluginID() != nil {
			cbArgs.PluginID = int(*ctx.PluginID())
		}
		if ctx.TenantID() != nil {
			cbArgs.TenantID = int(*ctx.TenantID())
		}
		if ctx.ChannelID() != nil {
			cbArgs.ChannelID = int(*ctx.ChannelID())
		}
		if err := responder.CallbackSubmit(h.DB, cbArgs); err != nil {
			log.Printf("%s | 插件提交回调失败: %v", ctx.OutOrderNo, err)
		}
	}

	// 返回订单信息
	result := gin.H{
		"orderNo":   ctx.OrderNo(),
		"orderId":   ctx.OrderID(),
		"amount":    ctx.Money,
		"channelId": req.ChannelID,
	}
	// 优先使用插件返回的支付URL，否则回退到收银台页面
	if payURL != "" {
		result["payUrl"] = payURL
	} else if ctx.DomainURL() != "" {
		result["payUrl"] = ctx.DomainURL() + "/pay/" + ctx.OrderID()
	}

	response.DetailResponse(c, result, "订单创建成功")
}

// List 订单列表
// GET /api/order/
func (h *OrderHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)

	currentUser, _ := middleware.GetCurrentUser(c)
	query := h.DB.Model(&model.Order{}).
		Preload("PayChannel").
		Preload("Merchant").
		Preload("Merchant.SystemUser")

	// 角色过滤
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				// 租户只能看到自己的商户的订单
				query = query.Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = "+model.Order{}.TableName()+".merchant_id").
					Where("m.parent_id = ?", tenant.ID)
			}
		case model.RoleKeyMerchant:
			var merchant model.Merchant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&merchant).Error; err == nil {
				query = query.Where("merchant_id = ?", merchant.ID)
			}
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				query = query.Joins("JOIN "+model.OrderDetail{}.TableName()+" od ON od.order_id = "+model.Order{}.TableName()+".id").
					Where("od.writeoff_id = ?", writeoff.ID)
			}
		}
	}

	// 查询过滤
	if orderNo := c.Query("order_no"); orderNo != "" {
		query = query.Where(model.Order{}.TableName()+".order_no = ?", orderNo)
	}
	if outOrderNo := c.Query("out_order_no"); outOrderNo != "" {
		query = query.Where(model.Order{}.TableName()+".out_order_no = ?", outOrderNo)
	}
	if status := c.Query("order_status"); status != "" {
		query = query.Where(model.Order{}.TableName()+".order_status = ?", status)
	}
	if channelID := c.Query("pay_channel_id"); channelID != "" {
		query = query.Where(model.Order{}.TableName()+".pay_channel_id = ?", channelID)
	}
	if startDate := c.Query("start_date"); startDate != "" {
		query = query.Where(model.Order{}.TableName()+".create_datetime >= ?", startDate)
	}
	if endDate := c.Query("end_date"); endDate != "" {
		query = query.Where(model.Order{}.TableName()+".create_datetime <= ?", endDate)
	}

	var total int64
	query.Count(&total)

	var orders []model.Order
	query.Order(model.Order{}.TableName() + ".create_datetime DESC").Offset(offset).Limit(limit).Find(&orders)

	var result []gin.H
	for _, o := range orders {
		item := gin.H{
			"id":              o.ID,
			"order_no":        o.OrderNo,
			"out_order_no":    o.OutOrderNo,
			"order_status":    o.OrderStatus,
			"money":           o.Money,
			"tax":             o.Tax,
			"pay_datetime":    o.PayDatetime,
			"product_name":    o.ProductName,
			"compatible":      o.Compatible,
			"create_datetime": o.CreateDatetime,
		}
		if o.PayChannel != nil {
			item["channel_name"] = o.PayChannel.Name
			item["pay_channel_id"] = o.PayChannel.ID
		}
		if o.Merchant != nil && o.Merchant.SystemUser.ID > 0 {
			item["merchant_name"] = o.Merchant.SystemUser.Name
			item["merchant_id"] = o.Merchant.ID
		}
		result = append(result, item)
	}

	response.PageResponse(c, result, total, page, limit, "")
}

// Retrieve 订单详情
// GET /api/order/:id/
func (h *OrderHandler) Retrieve(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var order model.Order
	if err := h.DB.Preload("PayChannel").Preload("Merchant").Preload("Merchant.SystemUser").
		Where("id = ? OR order_no = ?", id, id).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	// 检查当前用户是否有权查看该订单
	if !h.checkOrderOwnership(c, &order) {
		return
	}

	var detail model.OrderDetail
	h.DB.Preload("Plugin").Preload("Writeoff").Preload("Writeoff.SystemUser").Preload("Domain").
		Where("order_id = ?", order.ID).First(&detail)

	var device model.OrderDeviceDetails
	h.DB.Where("order_id = ?", order.ID).First(&device)

	data := gin.H{
		"id":              order.ID,
		"order_no":        order.OrderNo,
		"out_order_no":    order.OutOrderNo,
		"order_status":    order.OrderStatus,
		"money":           order.Money,
		"tax":             order.Tax,
		"pay_datetime":    order.PayDatetime,
		"product_name":    order.ProductName,
		"compatible":      order.Compatible,
		"create_datetime": order.CreateDatetime,
	}

	if order.PayChannel != nil {
		data["channel_name"] = order.PayChannel.Name
		data["pay_channel_id"] = order.PayChannel.ID
	}
	if order.Merchant != nil && order.Merchant.SystemUser.ID > 0 {
		data["merchant_name"] = order.Merchant.SystemUser.Name
		data["merchant_id"] = order.Merchant.ID
	}

	// 订单详情
	if detail.ID > 0 {
		data["notify_url"] = detail.NotifyURL
		data["jump_url"] = detail.JumpURL
		data["notify_money"] = detail.NotifyMoney
		data["product_id"] = detail.ProductID
		data["plugin_type"] = detail.PluginType
		data["plugin_upstream"] = detail.PluginUpstream
		data["merchant_tax"] = detail.MerchantTax
		if detail.Plugin != nil {
			data["plugin_name"] = detail.Plugin.Name
		}
		if detail.Writeoff != nil && detail.Writeoff.SystemUser.ID > 0 {
			data["writeoff_name"] = detail.Writeoff.SystemUser.Name
			data["writeoff_id"] = detail.Writeoff.ID
		}
		if detail.Domain != nil {
			data["domain_url"] = detail.Domain.URL
		}
	}

	// 设备信息
	if device.ID > 0 {
		data["ip_address"] = device.IPAddress
		data["device_type"] = device.DeviceType
		data["address"] = device.Address
	}

	// 通知状态
	var notification model.MerchantNotification
	if err := h.DB.Where("order_id = ?", order.ID).First(&notification).Error; err == nil {
		data["notify_status"] = notification.Status
	}

	response.DetailResponse(c, data, "")
}

// checkOrderOwnership 检查当前用户是否有权操作该订单
func (h *OrderHandler) checkOrderOwnership(c *gin.Context, order *model.Order) bool {
	currentUser, _ := middleware.GetCurrentUser(c)
	if currentUser == nil {
		response.ErrorResponse(c, "未获取到用户信息", 4001)
		return false
	}
	switch currentUser.Role.Key {
	case model.RoleKeyAdmin, model.RoleKeyOperation:
		return true
	case model.RoleKeyTenant:
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err != nil {
			response.ErrorResponse(c, "租户信息不存在")
			return false
		}
		if order.MerchantID == nil {
			response.ErrorResponse(c, "无权操作该订单")
			return false
		}
		var merchant model.Merchant
		if err := h.DB.Where("id = ? AND parent_id = ?", *order.MerchantID, tenant.ID).First(&merchant).Error; err != nil {
			response.ErrorResponse(c, "无权操作该订单")
			return false
		}
		return true
	case model.RoleKeyMerchant:
		var merchant model.Merchant
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&merchant).Error; err != nil {
			response.ErrorResponse(c, "商户信息不存在")
			return false
		}
		if order.MerchantID == nil || *order.MerchantID != merchant.ID {
			response.ErrorResponse(c, "无权操作该订单")
			return false
		}
		return true
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err != nil {
			response.ErrorResponse(c, "核销信息不存在")
			return false
		}
		var detail model.OrderDetail
		if err := h.DB.Where("order_id = ? AND writeoff_id = ?", order.ID, writeoff.ID).First(&detail).Error; err != nil {
			response.ErrorResponse(c, "无权操作该订单")
			return false
		}
		return true
	default:
		response.ErrorResponse(c, "无权操作该订单")
		return false
	}
}

// Close 关闭订单
// POST /api/order/:id/close/
func (h *OrderHandler) Close(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var order model.Order
	if err := h.DB.Where("id = ? OR order_no = ?", id, id).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	if !h.checkOrderOwnership(c, &order) {
		return
	}

	if order.OrderStatus != model.OrderStatusWaitPay && order.OrderStatus != model.OrderStatusInProduction {
		response.ErrorResponse(c, "订单状态不允许关闭")
		return
	}

	if err := h.DB.Model(&order).Update("order_status", model.OrderStatusClosed).Error; err != nil {
		response.ErrorResponse(c, "关闭失败")
		return
	}

	response.DetailResponse(c, nil, "订单已关闭")
}

// Refund 退款
// POST /api/order/:id/refund/
func (h *OrderHandler) Refund(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.ErrorResponse(c, "参数错误")
		return
	}

	currentUser, _ := middleware.GetCurrentUser(c)

	var order model.Order
	if err := h.DB.Preload("Merchant").Preload("Merchant.Parent").
		Where("id = ? OR order_no = ?", id, id).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	if !h.checkOrderOwnership(c, &order) {
		return
	}

	if order.OrderStatus != model.OrderStatusSuccess && order.OrderStatus != model.OrderStatusSuccessPre {
		response.ErrorResponse(c, "订单状态不允许退款")
		return
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		// 更新订单状态
		if err := tx.Model(&order).Update("order_status", model.OrderStatusRefund).Error; err != nil {
			return err
		}

		// 退还租户手续费
		if order.Tax > 0 && order.Merchant != nil && order.Merchant.Parent != nil {
			tenantID := order.Merchant.Parent.ID
			var creatorID *uint
			if currentUser != nil {
				creatorID = &currentUser.ID
			}
			if err := h.CashFlowService.CreateTenantCashFlow(tx, tenantID, model.TenantCashFlowOrderRefunds,
				int64(order.Tax), order.PayChannelID, &order.ID, creatorID); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		response.ErrorResponse(c, "退款失败: "+err.Error())
		return
	}

	response.DetailResponse(c, nil, "退款成功")
}

// QueryLogs 查询订单日志
// GET /api/order/:id/logs/
func (h *OrderHandler) QueryLogs(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var order model.Order
	if err := h.DB.Where("id = ? OR order_no = ?", id, id).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	// 检查当前用户是否有权查看该订单日志
	if !h.checkOrderOwnership(c, &order) {
		return
	}

	// 获取签名日志
	var orderLogs []model.OrderLog
	h.DB.Where("out_order_no = ?", order.OutOrderNo).Order("create_datetime DESC").Find(&orderLogs)

	// 获取查单日志
	var queryLogs []model.QueryLog
	h.DB.Where("order_no = ?", order.OrderNo).Order("create_datetime DESC").Find(&queryLogs)

	// 获取通知记录
	var notifyHistories []model.MerchantNotificationHistory
	var notification model.MerchantNotification
	if err := h.DB.Where("order_id = ?", order.ID).First(&notification).Error; err == nil {
		h.DB.Where("notification_id = ?", notification.ID).Order("create_datetime DESC").Find(&notifyHistories)
	}

	response.DetailResponse(c, gin.H{
		"order_logs":   orderLogs,
		"query_logs":   queryLogs,
		"notify_logs":  notifyHistories,
	}, "")
}

// Notify 手动触发通知
// POST /api/order/:id/notify/
func (h *OrderHandler) Notify(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var order model.Order
	if err := h.DB.Where("id = ? OR order_no = ?", id, id).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	if !h.checkOrderOwnership(c, &order) {
		return
	}

	if order.OrderStatus != model.OrderStatusSuccess && order.OrderStatus != model.OrderStatusSuccessPre {
		response.ErrorResponse(c, "订单未支付成功，无法通知")
		return
	}

	var detail model.OrderDetail
	if err := h.DB.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		response.ErrorResponse(c, "订单详情不存在")
		return
	}

	payTime := ""
	if order.PayDatetime != nil {
		payTime = order.PayDatetime.Format("2006-01-02 15:04:05")
	}

	// 异步发送通知
	go h.NotificationFactory.StartMerchantNotify(order.OrderNo, detail.NotifyURL, detail.NotifyMoney, payTime, 3)

	response.DetailResponse(c, nil, "通知已发送")
}

// Statistics 订单统计
// GET /api/order/statistics/
func (h *OrderHandler) Statistics(c *gin.Context) {
	currentUser, _ := middleware.GetCurrentUser(c)

	startDate := c.DefaultQuery("start_date", time.Now().Format("2006-01-02"))
	endDate := c.DefaultQuery("end_date", time.Now().Format("2006-01-02"))

	query := h.DB.Model(&model.DayStatistics{}).
		Where("date >= ? AND date <= ?", startDate, endDate)

	// 根据角色返回不同维度统计
	if currentUser != nil {
		switch currentUser.Role.Key {
		case model.RoleKeyTenant:
			var tenant model.Tenant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&tenant).Error; err == nil {
				var stats []model.TenantDayStatistics
				h.DB.Where("tenant_id = ? AND date >= ? AND date <= ?", tenant.ID, startDate, endDate).
					Order("date DESC").Find(&stats)
				response.DetailResponse(c, stats, "")
				return
			}
		case model.RoleKeyMerchant:
			var merchant model.Merchant
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&merchant).Error; err == nil {
				var stats []model.MerchantDayStatistics
				h.DB.Where("merchant_id = ? AND date >= ? AND date <= ?", merchant.ID, startDate, endDate).
					Order("date DESC").Find(&stats)
				response.DetailResponse(c, stats, "")
				return
			}
		case model.RoleKeyWriteoff:
			var writeoff model.WriteOff
			if err := h.DB.Where("system_user_id = ?", currentUser.ID).First(&writeoff).Error; err == nil {
				var stats []model.WriteOffDayStatistics
				h.DB.Where("writeoff_id = ? AND date >= ? AND date <= ?", writeoff.ID, startDate, endDate).
					Order("date DESC").Find(&stats)
				response.DetailResponse(c, stats, "")
				return
			}
		}
	}

	// 管理员/运维看全局统计
	var stats []model.DayStatistics
	query.Order("date DESC").Find(&stats)
	response.DetailResponse(c, stats, "")
}
