package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/plugin"
	"github.com/gamewinner2019/FlowManPay/internal/service"
)

// NotifyHandler 支付回调通知处理器
type NotifyHandler struct {
	DB                  *gorm.DB
	NotificationFactory *service.NotificationFactory
}

// NewNotifyHandler 创建通知处理器
func NewNotifyHandler(db *gorm.DB) *NotifyHandler {
	return &NotifyHandler{
		DB:                  db,
		NotificationFactory: service.NewNotificationFactory(db),
	}
}

// AlipayNotify 支付宝异步通知回调
// POST /api/pay/order/notify/:plugin_type/:product_id/
func (h *NotifyHandler) AlipayNotify(c *gin.Context) {
	pluginType := c.Param("plugin_type")
	productIDStr := c.Param("product_id")

	if !strings.HasPrefix(pluginType, "alipay_") {
		c.String(http.StatusBadRequest, "fail")
		return
	}

	productID, err := strconv.Atoi(productIDStr)
	if err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}

	// 解析POST表单数据（支付宝通知是application/x-www-form-urlencoded）
	if err := c.Request.ParseForm(); err != nil {
		log.Printf("[接收通知] 解析表单失败: %v", err)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	data := make(map[string]string)
	for k, v := range c.Request.PostForm {
		if len(v) > 0 {
			data[k] = v[0]
		}
	}

	logJSON, _ := json.MarshalIndent(data, "", "  ")
	log.Printf("[接收通知] %s/%d: %s", pluginType, productID, string(logJSON))

	// 获取支付宝产品并创建SDK验签
	var product model.AlipayProduct
	if err := h.DB.First(&product, productID).Error; err != nil {
		log.Printf("[接收通知] %s 产品%d不存在", pluginType, productID)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	alipaySDK, err := plugin.NewAlipaySDKFromProduct(h.DB, &product)
	if err != nil {
		log.Printf("[接收通知] %s 创建SDK失败: %v", pluginType, err)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	// 验证签名（必须先验签再做任何业务逻辑，防止信息泄露）
	if !alipaySDK.VerifyNotify(data) {
		log.Printf("[接收通知] %s 验签失败", pluginType)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	orderNo := data["out_trade_no"]
	payTime := data["gmt_payment"]
	totalAmount := data["total_amount"]
	ticketNo := data["trade_no"]

	// 查找订单
	var order model.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		log.Printf("[接收通知] %s 订单不存在, 订单号: %s", pluginType, orderNo)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	// 验证金额（将支付宝返回的元字符串解析为分进行整数比较，避免浮点精度问题）
	notifyAmountCents, err := parseYuanToCents(totalAmount)
	if err != nil || notifyAmountCents != order.Money {
		log.Printf("[接收通知] %s 订单金额不一致(订单:%d分,通知:%s元), 订单号: %s", pluginType, order.Money, totalAmount, orderNo)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	// 记录查单日志
	plugin.AddQueryLogReq(h.DB, orderNo,
		fmt.Sprintf("api/pay/order/notify/%s/%d/", pluginType, productID),
		data, "POST", order.OutOrderNo, "")

	// 更新票据号
	h.DB.Model(&model.OrderDetail{}).
		Where("order_id = ?", order.ID).
		Update("ticket_no", ticketNo)

	// 检查插件是否确认通知成功
	responder := plugin.GetByKey(pluginType)
	if responder != nil {
		dataMap := make(map[string]interface{})
		for k, v := range data {
			dataMap[k] = v
		}
		if responder.CheckNotifySuccess(dataMap) {
			// 通知成功，触发订单完成流程
			go h.successOrder(orderNo, payTime, pluginType)
			c.String(http.StatusOK, "success")
		} else {
			// 通知状态非成功（如WAIT_BUYER_PAY），返回fail让支付宝重试
			log.Printf("[接收通知] %s 通知状态非成功, 订单号: %s", pluginType, orderNo)
			c.String(http.StatusOK, "fail")
		}
	} else {
		// 没有对应插件，默认走成功流程
		go h.successOrder(orderNo, payTime, pluginType)
		c.String(http.StatusOK, "success")
	}
}

// successOrder 订单完成流程
func (h *NotifyHandler) successOrder(orderNo string, payTimeStr string, pluginType string) {
	// 查询订单（只处理生产中或等待支付状态）
	var order model.Order
	if err := h.DB.Where("order_no = ? AND order_status IN ?", orderNo,
		[]model.OrderStatus{model.OrderStatusInProduction, model.OrderStatusWaitPay}).
		First(&order).Error; err != nil {
		log.Printf("[通知成功] 订单已完成或不存在, 订单号: %s", orderNo)
		// 可能是已经是 SUCCESS_PRE 状态，尝试触发商户通知
		var existingOrder model.Order
		if err := h.DB.Where("order_no = ? AND order_status = ?", orderNo, model.OrderStatusSuccessPre).
			First(&existingOrder).Error; err == nil {
			h.triggerMerchantNotify(orderNo)
		}
		return
	}

	// 解析支付时间
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

	orderBefore := int(order.OrderStatus)

	// 原子更新订单状态为 SUCCESS_PRE（防止并发重复处理）
	result := h.DB.Model(&model.Order{}).Where("order_no = ? AND order_status IN ?", orderNo,
		[]model.OrderStatus{model.OrderStatusInProduction, model.OrderStatusWaitPay}).
		Updates(map[string]interface{}{
			"order_status": model.OrderStatusSuccessPre,
			"pay_datetime": payTime,
		})
	if result.RowsAffected == 0 {
		log.Printf("[通知成功] 订单已被其他进程处理, 订单号: %s", orderNo)
		h.triggerMerchantNotify(orderNo)
		return
	}

	log.Printf("[通知成功] 订单完成, 订单号: %s", orderNo)

	// 获取订单详情
	var detail model.OrderDetail
	if err := h.DB.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		log.Printf("[通知成功] 订单详情不存在, 订单号: %s", orderNo)
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
			log.Printf("[通知成功] 插件回调失败: %v", err)
		}
	}

	// 触发商户通知
	h.triggerMerchantNotify(orderNo)
}

// triggerMerchantNotify 触发商户通知
func (h *NotifyHandler) triggerMerchantNotify(orderNo string) {
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

// parseYuanToCents 将元字符串解析为分（整数），避免浮点精度问题
// 例如: "9.99" -> 999, "0.01" -> 1, "100.00" -> 10000
func parseYuanToCents(yuan string) (int, error) {
	yuan = strings.TrimSpace(yuan)
	if yuan == "" {
		return 0, fmt.Errorf("空金额")
	}
	parts := strings.SplitN(yuan, ".", 2)
	negative := strings.HasPrefix(parts[0], "-")
	intPart, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	cents := intPart * 100
	if len(parts) == 2 {
		decStr := parts[1]
		// 补齐到2位
		for len(decStr) < 2 {
			decStr += "0"
		}
		// 只取前2位
		decStr = decStr[:2]
		dec, err := strconv.Atoi(decStr)
		if err != nil {
			return 0, err
		}
		if negative {
			cents -= dec
		} else {
			cents += dec
		}
	}
	return cents, nil
}

// NotifyTest 通知测试接口
// POST/GET /api/pay/order/notify/test/
func (h *NotifyHandler) NotifyTest(c *gin.Context) {
	c.String(http.StatusOK, "success")
}

// SuccessOrderByQuery 查询后订单完成（由插件查单成功后调用）
func SuccessOrderByQuery(db *gorm.DB, orderNo string) {
	var order model.Order
	if err := db.Where("order_no = ? AND order_status NOT IN ?", orderNo,
		[]model.OrderStatus{model.OrderStatusSuccessPre, model.OrderStatusSuccess}).
		First(&order).Error; err != nil {
		log.Printf("[查询完成] 订单已经完成, 订单号: %s", orderNo)
		// 已经是 SUCCESS_PRE，尝试触发商户通知
		var existingOrder model.Order
		if err := db.Where("order_no = ? AND order_status = ?", orderNo, model.OrderStatusSuccessPre).
			First(&existingOrder).Error; err == nil {
			triggerMerchantNotifyStatic(db, orderNo)
		}
		return
	}

	orderBefore := int(order.OrderStatus)
	now := time.Now()

	// 原子更新订单状态（防止并发重复处理）
	result := db.Model(&model.Order{}).Where("order_no = ? AND order_status NOT IN ?", orderNo,
		[]model.OrderStatus{model.OrderStatusSuccessPre, model.OrderStatusSuccess}).
		Updates(map[string]interface{}{
			"order_status": model.OrderStatusSuccessPre,
			"pay_datetime": now,
		})
	if result.RowsAffected == 0 {
		log.Printf("[查询完成] 订单已被其他进程处理, 订单号: %s", orderNo)
		triggerMerchantNotifyStatic(db, orderNo)
		return
	}

	log.Printf("[查询完成] 订单完成, 订单号: %s", orderNo)

	// 获取订单详情
	var detail model.OrderDetail
	if err := db.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		return
	}

	// 触发插件成功回调
	if detail.PluginType != "" {
		if responder := plugin.GetByKey(detail.PluginType); responder != nil {
			args := plugin.CallbackArgs{
				PluginType:  detail.PluginType,
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
			if err := responder.CallbackSuccess(db, args); err != nil {
				log.Printf("[查询完成] 插件回调失败: %v", err)
			}
		}
	}

	// 触发商户通知
	triggerMerchantNotifyStatic(db, orderNo)
}

// triggerMerchantNotifyStatic 静态版本的触发商户通知
func triggerMerchantNotifyStatic(db *gorm.DB, orderNo string) {
	var order model.Order
	if err := db.Preload("Merchant").Preload("Merchant.SystemUser").
		Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		return
	}

	var detail model.OrderDetail
	if err := db.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		return
	}

	payTime := ""
	if order.PayDatetime != nil {
		payTime = order.PayDatetime.Format("2006-01-02 15:04:05")
	}

	factory := service.NewNotificationFactory(db)
	go factory.StartMerchantNotify(orderNo, detail.NotifyURL, detail.NotifyMoney, payTime, 5)
}
