package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/sign"
	"github.com/gamewinner2019/FlowManPay/internal/plugin"
	"github.com/gamewinner2019/FlowManPay/internal/service"
)

// OrderAPIHandler 订单公开API处理器（无需JWT认证，通过签名/缓存认证）
type OrderAPIHandler struct {
	DB                  *gorm.DB
	RDB                 *redis.Client
	NotificationFactory *service.NotificationFactory
}

// NewOrderAPIHandler 创建订单API处理器
func NewOrderAPIHandler(db *gorm.DB, rdb *redis.Client) *OrderAPIHandler {
	return &OrderAPIHandler{
		DB:                  db,
		RDB:                 rdb,
		NotificationFactory: service.NewNotificationFactory(db),
	}
}

// ===== start_order 收银台启动订单 =====

// StartOrder 收银台启动订单
// POST/GET /api/pay/order/start/
// 对应 Django 的 start_order
func (h *OrderAPIHandler) StartOrder(c *gin.Context) {
	ctx := c.Request.Context()

	var rawOrderNo string
	var device int
	var fingerprint string

	if c.Request.Method == "GET" {
		rawOrderNo = c.Query("order_no")
		device, _ = strconv.Atoi(c.Query("device_type"))
		fingerprint = c.Query("device_fingerprint")
	} else {
		var req struct {
			OrderNo           string `json:"order_no" form:"order_no"`
			DeviceType        int    `json:"device_type" form:"device_type"`
			DeviceFingerprint string `json:"device_fingerprint" form:"device_fingerprint"`
		}
		if err := c.ShouldBind(&req); err == nil {
			rawOrderNo = req.OrderNo
			device = req.DeviceType
			fingerprint = req.DeviceFingerprint
		} else {
			rawOrderNo = c.PostForm("order_no")
			device, _ = strconv.Atoi(c.PostForm("device_type"))
			fingerprint = c.PostForm("device_fingerprint")
		}
	}

	ua := c.GetHeader("User-Agent")
	_ = ua // 用于后续插件调用

	if rawOrderNo == "" {
		response.ErrorResponse(c, "订单号错误")
		return
	}

	// 从Redis获取真实订单号（rawOrderNo -> orderNo 的映射）
	orderNo := h.RDB.Get(ctx, rawOrderNo).Val()
	if orderNo == "" {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	// 查询订单
	var order model.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	// 检查订单状态（只有生成中和等待支付可以继续）
	if order.OrderStatus != model.OrderStatusInProduction && order.OrderStatus != model.OrderStatusWaitPay {
		response.ErrorResponse(c, fmt.Sprintf("订单%s", order.OrderStatus.Label()))
		return
	}

	// 检查Redis中的超时和支付时间
	canPayTimeKey := fmt.Sprintf("%s-pay-time", orderNo)
	canPayTime := h.RDB.Get(ctx, canPayTimeKey).Val()
	if canPayTime == "" {
		response.ErrorResponse(c, "已超过订单支付时间")
		return
	}

	outTime := h.RDB.Get(ctx, fmt.Sprintf("%s-timeout", orderNo)).Val()
	createTime := h.RDB.Get(ctx, fmt.Sprintf("%s-create-time", orderNo)).Val()
	if outTime == "" || createTime == "" {
		response.ErrorResponse(c, "订单已关闭")
		return
	}

	ip := getClientIPFromRequest(c)

	// 构建设备数据（返回给前端用于倒计时等）
	deviceData := gin.H{
		"out_time":    h.RDB.Get(ctx, canPayTimeKey).Val(),
		"create_time": createTime,
	}

	// 检查设备记录
	var devCount int64
	h.DB.Model(&model.OrderDeviceDetails{}).Where("order_id = ?", order.ID).Count(&devCount)
	if devCount == 0 {
		// 获取插件支持的设备类型
		supportDevice := model.SupportDeviceAndroidIOSPC // 默认全部支持
		if order.PayChannelID != nil {
			var channel model.PayChannel
			if err := h.DB.Preload("Plugin").First(&channel, *order.PayChannelID).Error; err == nil && channel.Plugin != nil {
				supportDevice = channel.Plugin.SupportDevice
			}
		}

		// 检查设备类型是否支持
		deviceSupported := false
		switch device {
		case 1: // Android
			deviceSupported = supportDevice.SupportsAndroid()
		case 2: // iOS
			deviceSupported = supportDevice.SupportsIOS()
		case 4: // PC
			deviceSupported = supportDevice.SupportsPC()
		}

		if deviceSupported {
			// 创建设备记录
			deviceDetail := model.OrderDeviceDetails{
				DeviceType:        model.DeviceType(device),
				DeviceFingerprint: fingerprint,
				IPAddress:         ip,
				OrderID:           order.ID,
			}
			result := h.DB.Where("order_id = ?", order.ID).FirstOrCreate(&deviceDetail)
			if result.RowsAffected > 0 {
				// 第一次创建，更新IP定位并触发设备提交通知
				go h.updateDeviceLocation(order.ID, ip)
				go h.deviceSubmitNotify(device, orderNo)
			}
		} else {
			deviceName := "安卓"
			if device == 2 {
				deviceName = "IOS"
			} else if device == 4 {
				deviceName = "PC"
			}
			h.DB.Model(&model.Order{}).Where("id = ?", order.ID).
				Update("remarks", fmt.Sprintf("[%s]设备类型不支持", deviceName))
			response.ErrorResponse(c, "设备类型不支持")
			return
		}
	}

	// 检查缓存中的订单创建结果
	orderCacheKey := fmt.Sprintf("%s-res", orderNo)
	orderCacheStr := h.RDB.Get(ctx, orderCacheKey).Val()

	pendingResult := map[string]interface{}{
		"code": float64(999),
		"msg":  "订单正在生成",
		"data": nil,
	}

	if orderCacheStr != "" {
		var res map[string]interface{}
		if err := json.Unmarshal([]byte(orderCacheStr), &res); err == nil {
			code, _ := res["code"].(float64)

			if code == 0 {
				// 订单已成功生成，返回支付数据
				changeZKey := fmt.Sprintf("%s-change_z", orderNo)
				if h.RDB.Get(ctx, changeZKey).Val() == "" {
					plugin.PluginQueryOrder(h.DB, h.RDB, orderNo)
					h.RDB.Set(ctx, changeZKey, "1", 300*time.Second)
				}
				if dataMap, ok := res["data"].(map[string]interface{}); ok {
					for k, v := range dataMap {
						deviceData[k] = v
					}
				}
				response.DetailResponse(c, deviceData, "获取成功")
				return
			} else if code == 999 {
				// 正在生成中
				h.errorResponseWithData(c, "订单正在生成", 999, deviceData)
				return
			} else {
				// 之前失败了，重新尝试创建
				pendingJSON, _ := json.Marshal(pendingResult)
				h.RDB.Set(ctx, orderCacheKey, string(pendingJSON), 600*time.Second)
				go plugin.PluginCreateOrder(h.DB, h.RDB, orderNo, rawOrderNo, ip, ua)
				msg, _ := res["msg"].(string)
				respCode := int(code)
				h.errorResponseWithData(c, msg, respCode, deviceData)
				return
			}
		}
	} else if order.OrderStatus == model.OrderStatusInProduction {
		// 订单处于生成中状态，检查缓存是否刚被设置
		if h.RDB.Get(ctx, orderCacheKey).Val() != "" {
			h.errorResponseWithData(c, "订单正在生成", 999, deviceData)
			return
		}
		// 生成订单
		pendingJSON, _ := json.Marshal(pendingResult)
		h.RDB.Set(ctx, orderCacheKey, string(pendingJSON), 600*time.Second)
		go plugin.PluginCreateOrder(h.DB, h.RDB, orderNo, rawOrderNo, ip, ua)
		h.errorResponseWithData(c, "订单正在生成", 999, deviceData)
		return
	}

	h.errorResponseWithData(c, "订单生成失败", 400, deviceData)
}

// ===== check_order 收银台轮询检查 =====

// CheckOrder 检查订单状态（收银台轮询）
// POST /api/pay/order/:raw_order_no/check/
// 对应 Django 的 check_order
func (h *OrderAPIHandler) CheckOrder(c *gin.Context) {
	rawOrderNo := c.Param("raw_order_no")
	ctx := c.Request.Context()

	orderNo := h.RDB.Get(ctx, rawOrderNo).Val()
	if orderNo == "" {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	var order model.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	switch order.OrderStatus {
	case model.OrderStatusSuccess, model.OrderStatusSuccessPre:
		// 订单已付款
		jumpURL := "https://www.baidu.com/"
		var detail model.OrderDetail
		if err := h.DB.Where("order_id = ?", order.ID).First(&detail).Error; err == nil && detail.JumpURL != "" {
			jumpURL = detail.JumpURL
		}
		h.errorResponseWithData(c, "订单已付款", 1002, gin.H{"jump_url": jumpURL})
	case model.OrderStatusClosed:
		response.ErrorResponseWithCode(c, 1007, "订单已关闭")
	case model.OrderStatusRefund:
		response.ErrorResponseWithCode(c, 1005, "订单已退款")
	default:
		response.ErrorResponseWithCode(c, 1000, "订单待支付")
	}
}

// ===== query_order 商户查询接口 =====

// QueryOrder 商户查询订单（带签名验证）
// GET/POST /api/pay/order/query_order/
// 对应 Django 的 query_order
func (h *OrderAPIHandler) QueryOrder(c *gin.Context) {
	var outOrderNo, orderNo string
	var merchantID int
	signData := make(map[string]string)

	if c.Request.Method == "POST" {
		// POST: 从JSON body或form获取参数
		var req map[string]interface{}
		if err := c.ShouldBindJSON(&req); err == nil {
			outOrderNo, _ = req["mchOrderNo"].(string)
			if mid, ok := req["mchId"].(float64); ok {
				merchantID = int(mid)
			}
			orderNo, _ = req["payOrderId"].(string)
			for k, v := range req {
				signData[k] = fmt.Sprintf("%v", v)
			}
		} else {
			// fallback to form
			outOrderNo = c.PostForm("mchOrderNo")
			merchantID, _ = strconv.Atoi(c.PostForm("mchId"))
			orderNo = c.PostForm("payOrderId")
			for k, v := range c.Request.PostForm {
				if len(v) > 0 {
					signData[k] = v[0]
				}
			}
		}
	} else {
		// GET: 从query params获取
		outOrderNo = c.Query("mchOrderNo")
		merchantID, _ = strconv.Atoi(c.Query("mchId"))
		orderNo = c.Query("payOrderId")
		for k := range c.Request.URL.Query() {
			signData[k] = c.Query(k)
		}
	}

	if merchantID == 0 {
		response.ErrorResponseWithCode(c, 7301, "商户不存在")
		return
	}

	// 查找商户
	type merchantInfo struct {
		ID                   uint
		SystemUserKey        string
		ParentID             uint
		SystemUserStatus     bool
		ParentSystemUserStatus bool
	}
	var mi merchantInfo
	err := h.DB.Table(model.Merchant{}.TableName()+" m").
		Select("m.id, su.`key` as system_user_key, m.parent_id, su.status as system_user_status, psu.status as parent_system_user_status").
		Joins("JOIN "+model.Users{}.TableName()+" su ON su.id = m.system_user_id").
		Joins("JOIN "+model.Tenant{}.TableName()+" t ON t.id = m.parent_id").
		Joins("JOIN "+model.Users{}.TableName()+" psu ON psu.id = t.system_user_id").
		Where("m.id = ? AND su.is_active = ?", merchantID, true).
		First(&mi).Error

	if err != nil {
		response.ErrorResponseWithCode(c, 7301, "商户不存在")
		return
	}

	// 验证签名
	reqSign := signData["sign"]
	delete(signData, "sign")

	// 修正 mchId 为 int 字符串（避免 float64 的 "1.0" 问题）
	signData["mchId"] = strconv.Itoa(merchantID)

	signRaw, encrypted, signErr := sign.GetSign(signData, mi.SystemUserKey, []string{"mchId"}, []string{"mchOrderNo", "payOrderId"}, 0)
	if signErr != nil {
		response.ErrorResponseWithCode(c, 7303, signErr.Error())
		return
	}
	_ = signRaw
	if encrypted != reqSign {
		response.ErrorResponseWithCode(c, 7304, "签名错误")
		return
	}

	// 查询订单
	var order model.Order
	if orderNo != "" {
		err = h.DB.Where("order_no = ?", orderNo).First(&order).Error
	} else if outOrderNo != "" {
		err = h.DB.Where("out_order_no = ?", outOrderNo).First(&order).Error
	} else {
		response.ErrorResponse(c, "订单号不能为空")
		return
	}
	if err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	// 获取订单详情
	var detail model.OrderDetail
	h.DB.Where("order_id = ?", order.ID).First(&detail)

	resData := map[string]string{
		"mchOrderNo": order.OutOrderNo,
		"payOrderId": order.OrderNo,
		"amount":     strconv.Itoa(detail.NotifyMoney),
	}
	msg := "查询成功"

	// 设置状态码
	resData["status"] = strconv.Itoa(getNotificationStatus(int(order.OrderStatus)))
	if (order.OrderStatus == model.OrderStatusSuccessPre || order.OrderStatus == model.OrderStatusSuccess) && order.PayDatetime != nil {
		resData["payTime"] = order.PayDatetime.Format("2006-01-02 15:04:05")
	}

	// 生成响应签名
	_, resSign := sign.ToSign(resData, mi.SystemUserKey)
	resData["sign"] = resSign

	response.DetailResponse(c, resData, msg)
}

// ===== notifies 微信回调 =====

// WechatNotify 微信支付回调通知
// POST /api/pay/order/notify/wechat_:suffix/:product_id/
// 对应 Django 的 notifies 中 wechat_ 分支
func (h *OrderAPIHandler) WechatNotify(c *gin.Context) {
	pluginType := c.Param("plugin_type")
	productIDStr := c.Param("product_id")

	if !strings.HasPrefix(pluginType, "wechat_") {
		c.String(http.StatusBadRequest, "fail")
		return
	}

	// 读取请求体
	bodyBytes, err := c.GetRawData()
	if err != nil {
		log.Printf("[接收通知] 微信回调读取body失败: %v", err)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	var data map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		log.Printf("[接收通知] 微信回调解析JSON失败: %v", err)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	logJSON, _ := json.MarshalIndent(data, "", "  ")
	log.Printf("[接收通知] %s/%s: %s", pluginType, productIDStr, string(logJSON))

	// 微信回调数据解析
	// 微信V3通知结构: resource -> out_trade_no, transaction_id, success_time, amount.total
	resource, ok := data["resource"].(map[string]interface{})
	if !ok {
		log.Printf("[接收通知] %s 缺少resource字段", pluginType)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	orderNo, _ := resource["out_trade_no"].(string)
	ticketNo, _ := resource["transaction_id"].(string)
	successTime, _ := resource["success_time"].(string)
	// 微信金额在 amount.total 中，单位为分
	var totalAmountCents int
	if amountMap, ok := resource["amount"].(map[string]interface{}); ok {
		if total, ok := amountMap["total"].(float64); ok {
			totalAmountCents = int(total)
		}
	}

	// 处理支付时间格式（移除时区标记）
	payTime := successTime
	payTime = strings.Replace(payTime, "+08:00", "", 1)
	payTime = strings.Replace(payTime, "T", " ", 1)

	// 格式化金额为元（用于比较）
	totalAmount := plugin.FormatMoney(totalAmountCents)

	// 查找订单
	var order model.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		log.Printf("[接收通知] %s 订单不存在, 订单号: %s", pluginType, orderNo)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	// 验证金额
	orderMoney := plugin.FormatMoney(order.Money)
	if orderMoney != totalAmount {
		log.Printf("[接收通知] %s 订单金额不一致(%s,%s), 订单号: %s", pluginType, orderMoney, totalAmount, orderNo)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	// 记录查单日志
	plugin.AddQueryLogReq(h.DB, orderNo,
		fmt.Sprintf("api/pay/order/notify/%s/%s/", pluginType, productIDStr),
		data, "POST", order.OutOrderNo, "")

	// 更新票据号
	h.DB.Model(&model.OrderDetail{}).
		Where("order_id = ?", order.ID).
		Update("ticket_no", ticketNo)

	// 检查插件是否确认通知成功
	responder := plugin.GetByKey(pluginType)
	if responder != nil {
		if responder.CheckNotifySuccess(data) {
			go h.successNotify(orderNo, ticketNo, payTime, pluginType)
			c.String(http.StatusOK, "success")
		} else {
			log.Printf("[接收通知] %s 通知状态非成功, 订单号: %s", pluginType, orderNo)
			c.String(http.StatusBadRequest, "fail")
		}
	} else {
		// 没有对应插件，拒绝通知（防止伪造回调）
		log.Printf("[接收通知] %s 未找到对应插件, 订单号: %s", pluginType, orderNo)
		c.String(http.StatusBadRequest, "fail")
		return
	}
}

// ===== retry_notify_all 重试全部通知 =====

// RetryNotifyAll 重试全部待通知订单
// POST /api/pay/order/retry/notify/all/
// 对应 Django 的 retry_notify_all
func (h *OrderAPIHandler) RetryNotifyAll(c *gin.Context) {
	// 需要认证，从context获取用户
	user, ok := middleware.GetCurrentUser(c)
	if !ok || user == nil {
		response.ErrorResponse(c, "未认证", 4001)
		return
	}

	// 仅 admin/operation/tenant 角色可操作
	if user.Role.Key != model.RoleKeyAdmin && user.Role.Key != model.RoleKeyOperation && user.Role.Key != model.RoleKeyTenant {
		response.ErrorResponse(c, "无权操作", 4003)
		return
	}

	var tenantID *uint
	if user.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err == nil {
			tenantID = &tenant.ID
		}
	}

	go h.retryAllNotify(tenantID)
	response.DetailResponse(c, nil, "成功")
}

// ===== reorder 补单 =====

// Reorder 补单（手动触发订单成功）
// POST /api/order/:id/reorder/
// 对应 Django 的 reorder
func (h *OrderAPIHandler) Reorder(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var order model.Order
	if err := h.DB.Where("id = ?", id).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	// 订单归属权检查（复用 OrderHandler 的权限逻辑）
	if !h.checkOrderOwnership(c, &order) {
		return
	}

	go h.reorderSuccessOrder(order.ID)
	response.DetailResponse(c, nil, "补单任务提交成功")
}

// ===== 辅助方法 =====

// errorResponseWithData 带数据的错误响应
func (h *OrderAPIHandler) errorResponseWithData(c *gin.Context, msg string, code int, data interface{}) {
	c.JSON(http.StatusOK, response.Response{
		Code:    code,
		Data:    data,
		Msg:     msg,
		Success: false,
	})
}

// getClientIPFromRequest 获取客户端IP
func getClientIPFromRequest(c *gin.Context) string {
	ip := c.GetHeader("X-Forwarded-For")
	if ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	ip = c.GetHeader("X-Real-IP")
	if ip != "" {
		return ip
	}
	return c.ClientIP()
}

// updateDeviceLocation 更新设备定位信息（异步）
func (h *OrderAPIHandler) updateDeviceLocation(orderID string, ip string) {
	// 简单的IP定位实现（实际生产中需要接入IP定位服务）
	h.DB.Model(&model.OrderDeviceDetails{}).Where("order_id = ?", orderID).
		Updates(map[string]interface{}{
			"address": "未知",
			"pid":     -1,
			"cid":     -1,
		})
}

// deviceSubmitNotify 设备提交通知（异步）
func (h *OrderAPIHandler) deviceSubmitNotify(device int, orderNo string) {
	ctx := context.Background()
	createArgsStr := h.RDB.Get(ctx, fmt.Sprintf("%s-create", orderNo)).Val()
	if createArgsStr == "" {
		return
	}

	// 触发设备 hook
	var order model.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		return
	}
	var deviceDetail model.OrderDeviceDetails
	if err := h.DB.Where("order_id = ?", order.ID).First(&deviceDetail).Error; err != nil {
		return
	}
	h.DB.Preload("Merchant").Preload("Merchant.Parent").Where("id = ?", order.ID).First(&order)
	service.GetHookRegistry().TriggerDevice(h.DB, &order, &deviceDetail)
}

// successNotify 微信通知成功后的处理流程（使用实际支付时间）
func (h *OrderAPIHandler) successNotify(orderNo string, ticketNo string, payTimeStr string, pluginType string) {
	// 更新票据号（通过订单关联）
	var order model.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		log.Printf("[微信通知] 订单不存在: %s", orderNo)
		return
	}
	h.DB.Model(&model.OrderDetail{}).
		Where("order_id = ?", order.ID).
		Update("ticket_no", ticketNo)

	// 解析实际支付时间（与 NotifyHandler.successOrder 保持一致）
	var payTime time.Time
	if payTimeStr != "" {
		parsed, err := time.ParseInLocation("2006-01-02 15:04:05", payTimeStr, time.Local)
		if err == nil {
			payTime = parsed
		} else {
			payTime = time.Now()
		}
	} else {
		payTime = time.Now()
	}

		// 查询订单（只处理生成中和等待支付状态，与支付宝路径保持一致）
		if err := h.DB.Where("order_no = ? AND order_status IN ?", orderNo,
			[]model.OrderStatus{model.OrderStatusInProduction, model.OrderStatusWaitPay}).
			First(&order).Error; err != nil {
			log.Printf("[微信通知] 订单状态不允许更新, 订单号: %s", orderNo)
		// 已经是 SUCCESS_PRE 状态，尝试触发商户通知
		var existingOrder model.Order
		if err := h.DB.Where("order_no = ? AND order_status = ?", orderNo, model.OrderStatusSuccessPre).
			First(&existingOrder).Error; err == nil {
			h.triggerMerchantNotify(orderNo)
		}
		return
	}

	orderBefore := int(order.OrderStatus)

		// 原子更新订单状态为 SUCCESS_PRE，使用实际支付时间（只允许从生成中/等待支付状态更新）
		result := h.DB.Model(&model.Order{}).Where("order_no = ? AND order_status IN ?", orderNo,
			[]model.OrderStatus{model.OrderStatusInProduction, model.OrderStatusWaitPay}).
		Updates(map[string]interface{}{
			"order_status": model.OrderStatusSuccessPre,
			"pay_datetime": payTime,
		})
	if result.RowsAffected == 0 {
		log.Printf("[微信通知] 订单已被其他进程处理, 订单号: %s", orderNo)
		h.triggerMerchantNotify(orderNo)
		return
	}

	log.Printf("[微信通知] 订单完成, 订单号: %s", orderNo)

	// 获取订单详情
	var detail model.OrderDetail
	if err := h.DB.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		log.Printf("[微信通知] 订单详情不存在, 订单号: %s", orderNo)
		return
	}

	// 触发插件成功回调
	responder := plugin.GetByKey(pluginType)
	if responder != nil {
		args := plugin.CallbackArgs{
			PluginType:  pluginType,
			OrderNo:     orderNo,
			OutOrderNo:  order.OutOrderNo,
			Money:       order.Money,
			OrderBefore: orderBefore,
			OrderAfter:  int(model.OrderStatusSuccessPre),
		}
		if detail.PluginID != nil {
			args.PluginID = int(*detail.PluginID)
		}
		if detail.ProductID != "" {
			args.ProductID, _ = strconv.Atoi(detail.ProductID)
		}
		if order.PayChannelID != nil {
			args.ChannelID = int(*order.PayChannelID)
		}
		if err := responder.CallbackSuccess(h.DB, args); err != nil {
			log.Printf("[微信通知] 插件回调失败: %v", err)
		}
	}

	// 触发成功 hooks（手续费扣减、统计更新等）
	h.DB.Preload("Merchant").Preload("Merchant.Parent").Where("id = ?", order.ID).First(&order)
	service.GetHookRegistry().TriggerSuccess(h.DB, &order, &detail)

	// 触发商户通知
	h.triggerMerchantNotify(orderNo)
}

// retryAllNotify 重试全部待通知订单
func (h *OrderAPIHandler) retryAllNotify(tenantID *uint) {
	now := time.Now().Add(-5 * time.Minute)
	beforeDay := now.Add(-24 * time.Hour)

	query := h.DB.Model(&model.Order{}).
		Where("order_status = ? AND pay_channel_id IS NOT NULL AND create_datetime >= ? AND create_datetime <= ?",
			model.OrderStatusSuccessPre, beforeDay, now)

	if tenantID != nil {
		query = query.Joins("JOIN "+model.Merchant{}.TableName()+" m ON m.id = merchant_id").
			Where("m.parent_id = ?", *tenantID)
	}

	var orders []model.Order
	query.Find(&orders)

	count := 0
	for _, order := range orders {
		var detail model.OrderDetail
		if err := h.DB.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
			continue
		}

		payTime := ""
		if order.PayDatetime != nil {
			payTime = order.PayDatetime.Format("2006-01-02 15:04:05")
		}

		h.NotificationFactory.StartMerchantNotify(order.OrderNo, detail.NotifyURL, detail.NotifyMoney, payTime, 4)
		count++
		if count%100 == 0 {
			time.Sleep(5 * time.Second)
		}
	}

	log.Printf("[重试通知] 共重试 %d 笔订单", count)
}

// reorderSuccessOrder 补单成功流程
func (h *OrderAPIHandler) reorderSuccessOrder(orderID string) {
	var order model.Order
	if err := h.DB.Where("id = ? AND order_status != ?", orderID, model.OrderStatusSuccess).
		First(&order).Error; err != nil {
		log.Printf("[补单] 订单不存在或已完成: %s", orderID)
		return
	}

	orderBefore := int(order.OrderStatus)

	// 更新状态
	now := time.Now()
	updates := map[string]interface{}{
		"order_status": model.OrderStatusSuccessPre,
	}
	if order.PayDatetime == nil {
		updates["pay_datetime"] = now
	}

	result := h.DB.Model(&model.Order{}).Where("id = ? AND order_status != ?", orderID, model.OrderStatusSuccess).
		Updates(updates)
	if result.RowsAffected == 0 {
		return
	}

	log.Printf("[补单] 补单完成, 订单号: %s", order.OrderNo)

	// 记录补单记录
	h.DB.Create(&model.ReOrder{
		OrderID: order.ID,
	})

	// 获取订单详情
	var detail model.OrderDetail
	if err := h.DB.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		return
	}

	if orderBefore == int(model.OrderStatusSuccessPre) {
		// 已经是成功预状态，只触发商户通知
		h.triggerMerchantNotify(order.OrderNo)
	} else {
		// 触发成功 hooks
		h.DB.Preload("Merchant").Preload("Merchant.Parent").Where("id = ?", order.ID).First(&order)
		service.GetHookRegistry().TriggerSuccess(h.DB, &order, &detail)
		h.triggerMerchantNotify(order.OrderNo)
	}
}

// triggerMerchantNotify 触发商户通知
func (h *OrderAPIHandler) triggerMerchantNotify(orderNo string) {
	var order model.Order
	if err := h.DB.Preload("Merchant").Preload("Merchant.SystemUser").
		Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		return
	}

	var detail model.OrderDetail
	if err := h.DB.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		return
	}

	payTime := ""
	if order.PayDatetime != nil {
		payTime = order.PayDatetime.Format("2006-01-02 15:04:05")
	}

	go h.NotificationFactory.StartMerchantNotify(orderNo, detail.NotifyURL, detail.NotifyMoney, payTime, 5)
}

// checkOrderOwnership 检查当前用户是否有权操作该订单
// 复用 OrderHandler.checkOrderOwnership 的逻辑
func (h *OrderAPIHandler) checkOrderOwnership(c *gin.Context, order *model.Order) bool {
	currentUser, ok := middleware.GetCurrentUser(c)
	if !ok || currentUser == nil {
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

// getNotificationStatus 将内部订单状态映射为对外通知状态码
// 对应 Django 的 get_notification_status
func getNotificationStatus(orderStatus int) int {
	switch orderStatus {
	case 0: // 生成中
		return 1001
	case 1: // 出码失败
		return 1006
	case 2: // 等待支付
		return 1002
	case 3: // 支付失败
		return 1006
	case 4: // 支付成功，通知已返回
		return 1004
	case 5: // 已退款
		return 1008
	case 6: // 支付成功，通知未返回
		return 1005
	case 7: // 已关闭
		return 1007
	default:
		return 1001
	}
}
