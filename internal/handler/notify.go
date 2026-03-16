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

	// 验证签名
	flag := alipaySDK.VerifyNotify(data)

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

	// 验证金额
	orderMoney := fmt.Sprintf("%.2f", float64(order.Money)/100)
	if orderMoney != totalAmount {
		log.Printf("[接收通知] %s 订单金额不一致(%s,%s), 订单号: %s", pluginType, orderMoney, totalAmount, orderNo)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	if flag {
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
			}
		} else {
			// 没有对应插件，默认走成功流程
			go h.successOrder(orderNo, payTime, pluginType)
		}

		c.String(http.StatusOK, "success")
	} else {
		log.Printf("[接收通知] %s 验签失败, 订单号: %s", pluginType, orderNo)
		c.String(http.StatusBadRequest, "fail")
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

	// 更新订单状态为 SUCCESS_PRE（待通知商户）
	h.DB.Model(&order).Updates(map[string]interface{}{
		"order_status": model.OrderStatusSuccessPre,
		"pay_datetime": payTime,
	})

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

	db.Model(&order).Updates(map[string]interface{}{
		"order_status": model.OrderStatusSuccessPre,
		"pay_datetime": now,
	})

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
