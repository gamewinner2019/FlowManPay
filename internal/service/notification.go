package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/sign"
)

// ===== 通知状态辅助函数 =====

// GetNotificationStatus 获取通知状态文本(本系统)
func GetNotificationStatus(status model.OrderStatus) string {
	switch status {
	case model.OrderStatusSuccess, model.OrderStatusSuccessPre:
		return "1"
	default:
		return "0"
	}
}

// GetYiPayNotificationStatus 获取通知状态文本(易支付)
func GetYiPayNotificationStatus(status model.OrderStatus) string {
	switch status {
	case model.OrderStatusSuccess, model.OrderStatusSuccessPre:
		return "TRADE_SUCCESS"
	default:
		return "TRADE_WAIT"
	}
}

// FormatMoney 格式化金额(分→元，保留两位小数)
func FormatMoney(money int) string {
	yuan := float64(money) / 100.0
	return fmt.Sprintf("%.2f", yuan)
}

// ===== NotificationSender 通知发送接口 =====

// NotificationSender 商户通知发送接口
type NotificationSender interface {
	// Notify 发送通知
	Notify() error
	// NotifyCount 获取最大通知次数
	NotifyCount() int
}

// ===== BaseNotificationSender 基础通知发送者 =====

// baseNotificationSender 基础通知发送者
type baseNotificationSender struct {
	DB             *gorm.DB
	MerchantKey    string
	OrderNo        string
	NotifyMoney    int
	NotifyURL      string
	NotificationID uint
	InitData       map[string]string
	MaxNotifyCount int
	NotifyTimeout  time.Duration
	NotifyMethod   string
}

// NotifyCount 获取最大通知次数
func (s *baseNotificationSender) NotifyCount() int {
	return s.MaxNotifyCount
}

// createHistory 创建通知历史记录
func (s *baseNotificationSender) createHistory(url string, reqData map[string]string) (*model.MerchantNotificationHistory, error) {
	bodyJSON, _ := json.MarshalIndent(reqData, "", "    ")
	history := &model.MerchantNotificationHistory{
		NotificationID: s.NotificationID,
		URL:            url,
		RequestMethod:  s.NotifyMethod,
		RequestBody:    string(bodyJSON),
	}
	if err := s.DB.Create(history).Error; err != nil {
		return nil, err
	}
	return history, nil
}

// updateNotification 更新通知状态
func (s *baseNotificationSender) updateNotification(status model.NotifyStatus) {
	s.DB.Model(&model.MerchantNotification{}).
		Where("id = ?", s.NotificationID).
		Update("status", status)
}

// updateHistoryResponse 更新历史记录的响应
func (s *baseNotificationSender) updateHistoryResponse(historyID uint, statusCode int, result string) {
	s.DB.Model(&model.MerchantNotificationHistory{}).
		Where("id = ?", historyID).
		Updates(map[string]interface{}{
			"response_code": statusCode,
			"json_result":   result,
		})
}

// updateReOrderRemarks 更新重新支付记录备注
func (s *baseNotificationSender) updateReOrderRemarks(orderNo string, remarks string) {
	s.DB.Model(&model.ReOrder{}).
		Joins("JOIN "+model.Order{}.TableName()+" o ON o.id = "+model.ReOrder{}.TableName()+".order_id").
		Where("o.order_no = ?", orderNo).
		Update("remarks", remarks)
}

// UpdateSuccessOrderStatus 更新订单状态为成功(已通知)
func UpdateSuccessOrderStatus(db *gorm.DB, orderNo string, status model.OrderStatus) {
	db.Model(&model.Order{}).
		Where("order_no = ? AND order_status = ?", orderNo, model.OrderStatusSuccessPre).
		Update("order_status", status)
}

// ===== GoogleNotificationSender 本系统通知发送者 =====

// GoogleNotificationSender 本系统通知发送者
type GoogleNotificationSender struct {
	baseNotificationSender
}

// NewGoogleNotificationSender 创建本系统通知发送者
func NewGoogleNotificationSender(db *gorm.DB, order *model.Order, merchantKey string,
	notifyURL string, notificationID uint, notifyMoney int, payTime string) *GoogleNotificationSender {

	initData := map[string]string{
		"payOrderId": order.OrderNo,
		"mchOrderNo": order.OutOrderNo,
		"status":     GetNotificationStatus(order.OrderStatus),
		"amount":     fmt.Sprintf("%d", notifyMoney),
	}
	if order.MerchantID != nil {
		initData["mchId"] = fmt.Sprintf("%d", *order.MerchantID)
	}
	if order.PayChannelID != nil {
		initData["channelId"] = fmt.Sprintf("%d", *order.PayChannelID)
	}
	if payTime != "" {
		initData["payTime"] = payTime
	}

	return &GoogleNotificationSender{
		baseNotificationSender: baseNotificationSender{
			DB:             db,
			MerchantKey:    merchantKey,
			OrderNo:        order.OrderNo,
			NotifyMoney:    notifyMoney,
			NotifyURL:      notifyURL,
			NotificationID: notificationID,
			InitData:       initData,
			MaxNotifyCount: 5,
			NotifyTimeout:  20 * time.Second,
			NotifyMethod:   "POST",
		},
	}
}

// buildReqData 构建请求数据
func (s *GoogleNotificationSender) buildReqData() map[string]string {
	data := make(map[string]string)
	for k, v := range s.InitData {
		data[k] = v
	}
	data["notifyTime"] = time.Now().Format("2006-01-02 15:04:05")
	_, resSign := sign.ToSign(data, s.MerchantKey)
	data["sign"] = resSign
	return data
}

// Notify 发送通知
func (s *GoogleNotificationSender) Notify() error {
	// 检查订单是否已完成
	var count int64
	s.DB.Model(&model.Order{}).Where("order_no = ? AND order_status = ?", s.OrderNo, model.OrderStatusSuccess).Count(&count)
	if count > 0 {
		log.Printf("%s | 订单已经完成,取消重复通知", s.OrderNo)
		return nil
	}

	reqData := s.buildReqData()
	history, err := s.createHistory(s.NotifyURL, reqData)
	if err != nil {
		return err
	}

	// 发送POST请求(JSON body)
	bodyJSON, _ := json.Marshal(reqData)
	client := &http.Client{Timeout: s.NotifyTimeout}
	req, err := http.NewRequest("POST", s.NotifyURL, strings.NewReader(string(bodyJSON)))
	if err != nil {
		s.updateNotification(model.NotifyStatusFailed)
		s.updateReOrderRemarks(s.OrderNo, fmt.Sprintf("[Error]%v", err))
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "close")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:91.0) Gecko/20100101 Firefox/91.0")

	resp, err := client.Do(req)
	if err != nil {
		s.updateNotification(model.NotifyStatusFailed)
		s.updateReOrderRemarks(s.OrderNo, fmt.Sprintf("[Error]%v", err))
		return err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)

	s.updateHistoryResponse(history.ID, resp.StatusCode, bodyStr)

	if bodyStr == "success" {
		s.updateNotification(model.NotifyStatusSuccess)
		UpdateSuccessOrderStatus(s.DB, s.OrderNo, model.OrderStatusSuccess)
		s.updateReOrderRemarks(s.OrderNo, "[200]success")
	} else {
		s.updateNotification(model.NotifyStatusFailed)
		s.updateReOrderRemarks(s.OrderNo, fmt.Sprintf("[%d]%s", resp.StatusCode, bodyStr))
	}

	return nil
}

// ===== YiPayNotificationSender 易支付通知发送者 =====

// YiPayNotificationSender 易支付通知发送者
type YiPayNotificationSender struct {
	baseNotificationSender
}

// NewYiPayNotificationSender 创建易支付通知发送者
func NewYiPayNotificationSender(db *gorm.DB, order *model.Order, merchantKey string,
	merchantName string, notifyURL string, notificationID uint, notifyMoney int, payTime string) *YiPayNotificationSender {

	initData := map[string]string{
		"trade_no":     order.OrderNo,
		"out_trade_no": order.OutOrderNo,
		"name":         merchantName,
		"trade_status": GetYiPayNotificationStatus(order.OrderStatus),
		"money":        FormatMoney(notifyMoney),
		"sign_type":    "MD5",
	}
	if order.MerchantID != nil {
		initData["pid"] = fmt.Sprintf("%d", *order.MerchantID)
	}
	if order.PayChannelID != nil {
		initData["type"] = fmt.Sprintf("%d", *order.PayChannelID)
	}

	return &YiPayNotificationSender{
		baseNotificationSender: baseNotificationSender{
			DB:             db,
			MerchantKey:    merchantKey,
			OrderNo:        order.OrderNo,
			NotifyMoney:    notifyMoney,
			NotifyURL:      notifyURL,
			NotificationID: notificationID,
			InitData:       initData,
			MaxNotifyCount: 5,
			NotifyTimeout:  20 * time.Second,
			NotifyMethod:   "GET",
		},
	}
}

// buildReqData 构建请求数据
func (s *YiPayNotificationSender) buildReqData() map[string]string {
	data := make(map[string]string)
	for k, v := range s.InitData {
		data[k] = v
	}
	_, resSign := sign.YiSign(data, s.MerchantKey)
	data["sign"] = resSign
	return data
}

// Notify 发送通知
func (s *YiPayNotificationSender) Notify() error {
	// 检查订单是否已完成
	var count int64
	s.DB.Model(&model.Order{}).Where("order_no = ? AND order_status = ?", s.OrderNo, model.OrderStatusSuccess).Count(&count)
	if count > 0 {
		log.Printf("%s | 订单已经完成,取消重复通知", s.OrderNo)
		return nil
	}

	reqData := s.buildReqData()
	history, err := s.createHistory(s.NotifyURL, reqData)
	if err != nil {
		return err
	}

	// 构建GET请求URL
	client := &http.Client{Timeout: s.NotifyTimeout}
	req, err := http.NewRequest("GET", s.NotifyURL, nil)
	if err != nil {
		s.updateNotification(model.NotifyStatusFailed)
		s.updateReOrderRemarks(s.OrderNo, fmt.Sprintf("[Error]%v", err))
		return err
	}
	q := req.URL.Query()
	for k, v := range reqData {
		q.Add(k, v)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Connection", "close")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:91.0) Gecko/20100101 Firefox/91.0")

	resp, err := client.Do(req)
	if err != nil {
		s.updateNotification(model.NotifyStatusFailed)
		s.updateReOrderRemarks(s.OrderNo, fmt.Sprintf("[Error]%v", err))
		return err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)

	s.updateHistoryResponse(history.ID, resp.StatusCode, bodyStr)

	if bodyStr == "success" {
		s.updateNotification(model.NotifyStatusSuccess)
		UpdateSuccessOrderStatus(s.DB, s.OrderNo, model.OrderStatusSuccess)
		s.updateReOrderRemarks(s.OrderNo, "[200]success")
	} else {
		s.updateNotification(model.NotifyStatusFailed)
		s.updateReOrderRemarks(s.OrderNo, fmt.Sprintf("[%d]%s", resp.StatusCode, bodyStr))
	}

	return nil
}

// ===== NotificationFactory 通知工厂 =====

// NotificationFactory 通知工厂
type NotificationFactory struct {
	DB *gorm.DB
}

// NewNotificationFactory 创建通知工厂
func NewNotificationFactory(db *gorm.DB) *NotificationFactory {
	return &NotificationFactory{DB: db}
}

// GetSender 获取通知发送者
func (f *NotificationFactory) GetSender(orderNo string, notifyURL string, notifyMoney int, payTime string) (NotificationSender, error) {
	var order model.Order
	if err := f.DB.Preload("Merchant").Preload("Merchant.SystemUser").Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		return nil, fmt.Errorf("订单%s不存在", orderNo)
	}

	// 获取或创建通知记录
	var notification model.MerchantNotification
	err := f.DB.Where("order_id = ?", order.ID).First(&notification).Error
	if err != nil {
		notification = model.MerchantNotification{
			OrderID: order.ID,
			Status:  model.NotifyStatusPending,
		}
		if err := f.DB.Create(&notification).Error; err != nil {
			return nil, err
		}
	}

	if order.Merchant == nil {
		log.Printf("订单%s没有商户,取消通知", orderNo)
		return nil, nil
	}

	merchantKey := order.Merchant.SystemUser.Key
	merchantName := order.Merchant.SystemUser.Name

	if order.Compatible == model.OrderCompatibleYiPay {
		return NewYiPayNotificationSender(f.DB, &order, merchantKey, merchantName,
			notifyURL, notification.ID, notifyMoney, payTime), nil
	}
	return NewGoogleNotificationSender(f.DB, &order, merchantKey,
		notifyURL, notification.ID, notifyMoney, payTime), nil
}

// StartMerchantNotify 启动商户通知(同步版本，异步任务需在调用方处理)
func (f *NotificationFactory) StartMerchantNotify(orderNo string, notifyURL string, notifyMoney int, payTime string, maxRetry int) {
	for i := 0; i < maxRetry; i++ {
		sender, err := f.GetSender(orderNo, notifyURL, notifyMoney, payTime)
		if err != nil {
			log.Printf("获取通知发送者失败: %v", err)
			time.Sleep(60 * time.Second)
			continue
		}
		if sender == nil {
			return
		}

		// 检查订单是否已完成
		var count int64
		f.DB.Model(&model.Order{}).Where("order_no = ? AND order_status = ?", orderNo, model.OrderStatusSuccess).Count(&count)
		if count > 0 {
			log.Printf("%s | 订单已经完成,取消重复通知", orderNo)
			return
		}

		if err := sender.Notify(); err != nil {
			log.Printf("通知失败: %v", err)
		}

		// 通知成功后检查订单状态
		f.DB.Model(&model.Order{}).Where("order_no = ? AND order_status = ?", orderNo, model.OrderStatusSuccess).Count(&count)
		if count > 0 {
			return
		}

		time.Sleep(60 * time.Second)
	}
}
