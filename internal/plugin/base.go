package plugin

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gamewinner2019/FlowManPay/internal/model"
	"gorm.io/gorm"
)

// BasePlugin 基础插件实现，提供默认方法
// 对应 Django 的 BasePluginResponder
type BasePlugin struct {
	Props PluginProperties
}

// Properties 返回插件属性
func (b *BasePlugin) Properties() PluginProperties {
	return b.Props
}

// CreateOrder 默认创建订单（空实现）
func (b *BasePlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	return ErrorResult(7500, "插件未实现创建订单"), nil
}

// QueryOrder 默认查询订单（空实现）
func (b *BasePlugin) QueryOrder(db *gorm.DB, args QueryOrderArgs) (bool, error) {
	return false, nil
}

// CallbackSuccess 默认支付成功回调（空实现）
func (b *BasePlugin) CallbackSuccess(db *gorm.DB, args CallbackArgs) error {
	return nil
}

// CallbackSubmit 默认订单提交回调（空实现）
func (b *BasePlugin) CallbackSubmit(db *gorm.DB, args CallbackArgs) error {
	return nil
}

// CallbackTimeout 默认订单超时回调（空实现）
func (b *BasePlugin) CallbackTimeout(db *gorm.DB, args CallbackArgs) error {
	return nil
}

// CallbackRefund 默认退款回调（空实现）
func (b *BasePlugin) CallbackRefund(db *gorm.DB, args CallbackArgs) error {
	return nil
}

// CheckNotifySuccess 默认检查通知是否成功（返回false）
func (b *BasePlugin) CheckNotifySuccess(data map[string]interface{}) bool {
	return false
}

// GetWriteoffProduct 默认选择核销产品（不选择）
func (b *BasePlugin) GetWriteoffProduct(db *gorm.DB, args WriteoffProductArgs) *WriteoffProductResult {
	return &WriteoffProductResult{
		ProductID:  0,
		WriteoffID: 0,
		Money:      args.Money,
	}
}

// GetChannelExtraArgs 默认获取通道额外参数（空）
func (b *BasePlugin) GetChannelExtraArgs() []map[string]interface{} {
	return nil
}

// ===== 基础工具方法 =====

// UpdateOrderWait 更新订单状态为等待支付
func UpdateOrderWait(db *gorm.DB, orderNo string) {
	result := db.Model(&model.Order{}).
		Where("order_no = ? AND order_status = ?", orderNo, model.OrderStatusInProduction).
		Update("order_status", model.OrderStatusWaitPay)
	if result.Error != nil {
		log.Printf("更新订单状态失败: %v", result.Error)
	}
}

// SaveOrderDetail 更新订单详情的查询号和票据号
func SaveOrderDetail(db *gorm.DB, detailID int, queryNo string, ticketNo string, extra map[string]interface{}) {
	updates := map[string]interface{}{
		"query_no":  queryNo,
		"ticket_no": ticketNo,
	}
	if extra != nil {
		extraJSON, err := json.Marshal(extra)
		if err == nil {
			updates["extra"] = string(extraJSON)
		}
	}
	db.Model(&model.OrderDetail{}).Where("id = ?", detailID).Updates(updates)
}

// UpdateOrderCookie 更新订单详情的cookie_id
func UpdateOrderCookie(db *gorm.DB, detailID int, cookieID int) {
	db.Model(&model.OrderDetail{}).Where("id = ?", detailID).Update("cookie_id", cookieID)
}

// UpdateOrderProduct 更新订单详情的product_id
func UpdateOrderProduct(db *gorm.DB, detailID int, productID int) {
	db.Model(&model.OrderDetail{}).Where("id = ?", detailID).Update("product_id", productID)
}

// UpdateOrderRemarks 更新订单备注
func UpdateOrderRemarks(db *gorm.DB, orderNo string, remarks string) {
	db.Model(&model.Order{}).Where("order_no = ?", orderNo).Update("remarks", remarks)
}

// SavePayURL 保存支付URL到订单
func SavePayURL(db *gorm.DB, orderID string, payURL string) {
	db.Model(&model.Order{}).Where("id = ?", orderID).Update("pay_url", payURL)
}

// GetPluginConfigValue 获取插件配置值
func GetPluginConfigValue(db *gorm.DB, pluginID uint, key string) string {
	var config model.PayPluginConfig
	if err := db.Where("parent_id = ? AND `key` = ?", pluginID, key).First(&config).Error; err != nil {
		return ""
	}
	if config.Value == nil {
		return ""
	}
	return *config.Value
}

// GetPluginConfigMap 获取插件配置为map
func GetPluginConfigMap(db *gorm.DB, pluginID uint, key string) map[string]string {
	val := GetPluginConfigValue(db, pluginID, key)
	if val == "" {
		return nil
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		return nil
	}
	return result
}

// GetPluginSubject 获取插件订单标题
func GetPluginSubject(db *gorm.DB, pluginID uint, money int, orderNo string, outOrderNo string) string {
	val := GetPluginConfigValue(db, pluginID, "subject")
	if val == "" {
		return fmt.Sprintf("订单%s", orderNo)
	}
	// 去掉引号
	val = strings.Trim(val, "\"")
	// 替换模板变量
	r := strings.NewReplacer(
		"{money}", fmt.Sprintf("%.2f", float64(money)/100),
		"{order_no}", orderNo,
		"{out_order_no}", outOrderNo,
	)
	return r.Replace(val)
}

// GetSystemConfigFromCache 从数据库获取系统配置
func GetSystemConfigFromCache(db *gorm.DB, key string) string {
	var config model.SystemConfig
	if err := db.Where("`key` = ?", key).First(&config).Error; err != nil {
		return ""
	}
	if config.Value == nil {
		return ""
	}
	val := strings.Trim(*config.Value, "\"")
	return val
}

// AddQueryLogReq 添加查询日志请求记录
func AddQueryLogReq(db *gorm.DB, orderNo string, url string, body interface{}, method string, outOrderNo string, remarks string) uint {
	bodyJSON, _ := json.Marshal(body)
	logEntry := model.QueryLog{
		OrderNo:    orderNo,
		OutOrderNo: outOrderNo,
		URL:        url,
		ReqBody:    string(bodyJSON),
		Method:     method,
		Remarks:    remarks,
	}
	db.Create(&logEntry)
	return logEntry.ID
}

// AddQueryLogRes 添加查询日志响应记录
func AddQueryLogRes(db *gorm.DB, logID uint, statusCode int, responseBody string) {
	if logID == 0 {
		return
	}
	db.Model(&model.QueryLog{}).Where("id = ?", logID).Updates(map[string]interface{}{
		"status_code": statusCode,
		"res_body":    responseBody,
	})
}

// BaseCreateOrder 基础创建订单（调用第三方API）
func BaseCreateOrder(db *gorm.DB, pluginID uint, orderNo string, money int, cookieID int,
	outOrderNo string, url string, timeout int, extraParams map[string]interface{}) (map[string]interface{}, error) {

	// 获取请求体模板
	bodyTemplate := GetPluginConfigValue(db, pluginID, "create_request_body")
	if bodyTemplate == "" {
		return nil, fmt.Errorf("插件配置缺少 create_request_body")
	}

	// 替换模板变量
	r := strings.NewReplacer(
		"{money}", fmt.Sprintf("%.2f", float64(money)/100),
		"{money_raw}", fmt.Sprintf("%d", money),
		"{order_no}", orderNo,
	)
	bodyStr := r.Replace(bodyTemplate)

	// 解析JSON
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(bodyStr), &body); err != nil {
		return nil, fmt.Errorf("解析请求体失败: %v", err)
	}

	// 获取URL
	if url == "" {
		url = GetPluginConfigValue(db, pluginID, "create_order_url")
		url = strings.Trim(url, "\"")
	}
	if url == "" {
		return nil, fmt.Errorf("支付插件出错: 缺少URL")
	}

	// 获取请求方法
	method := GetPluginConfigValue(db, pluginID, "create_request_method")
	method = strings.Trim(method, "\"")
	if method == "" {
		method = "POST"
	}

	// 记录请求日志
	logID := AddQueryLogReq(db, orderNo, url, body, method, outOrderNo, "")

	// 发送请求
	if timeout <= 0 {
		timeout = 10
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}

	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest(strings.ToUpper(method), url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		AddQueryLogRes(db, logID, resp.StatusCode, "解析响应失败")
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	resultJSON, _ := json.Marshal(result)
	AddQueryLogRes(db, logID, resp.StatusCode, string(resultJSON))

	if resp.StatusCode == 500 {
		return nil, nil
	}

	return result, nil
}

// BaseQueryOrder 基础查询订单（调用第三方API）
func BaseQueryOrder(db *gorm.DB, pluginID uint, orderNo string, money int, queryNo string,
	cookieID int, remarks string, url string, timeout int) (map[string]interface{}, error) {

	// 获取请求体模板
	bodyTemplate := GetPluginConfigValue(db, pluginID, "query_request_body")
	if bodyTemplate == "" {
		return nil, fmt.Errorf("插件配置缺少 query_request_body")
	}

	// 替换模板变量
	r := strings.NewReplacer(
		"{money}", fmt.Sprintf("%.2f", float64(money)/100),
		"{money_raw}", fmt.Sprintf("%d", money),
		"{ticket_no}", queryNo,
	)
	bodyStr := r.Replace(bodyTemplate)

	var body map[string]interface{}
	if err := json.Unmarshal([]byte(bodyStr), &body); err != nil {
		return nil, fmt.Errorf("解析请求体失败: %v", err)
	}

	if url == "" {
		url = GetPluginConfigValue(db, pluginID, "query_order_url")
		url = strings.Trim(url, "\"")
	}
	if url == "" {
		return nil, fmt.Errorf("支付插件出错: 缺少URL")
	}

	method := GetPluginConfigValue(db, pluginID, "query_request_method")
	method = strings.Trim(method, "\"")
	if method == "" {
		method = "POST"
	}

	logID := AddQueryLogReq(db, orderNo, url, body, method, "", remarks)

	if timeout <= 0 {
		timeout = 10
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}

	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest(strings.ToUpper(method), url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		AddQueryLogRes(db, logID, resp.StatusCode, "解析响应失败")
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	resultJSON, _ := json.Marshal(result)
	AddQueryLogRes(db, logID, resp.StatusCode, string(resultJSON))

	return result, nil
}

// ChoiceProduct 选择产品（带预占位逻辑）
func ChoiceProduct(db *gorm.DB, pluginKey string, results []map[string]interface{},
	money int, outOrderNo string) (int, int, int) {
	if len(results) == 0 {
		return 0, 0, money
	}
	for _, item := range results {
		id := toInt(item["id"])
		limitMoney := toInt(item["limit_money"])
		totalMoney := toInt(item["total_money"])
		writeoffID := toInt(item["writeoff_id"])

		if limitMoney != 0 && totalMoney+money > limitMoney {
			continue
		}

		return id, writeoffID, money
	}
	return 0, 0, money
}

// GetWriteoffIDs 获取可用核销ID列表
func GetWriteoffIDs(db *gorm.DB, tenantID int, money int, channelID int) []int {
	var ids []int
	db.Model(&model.WriteoffPayChannel{}).
		Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = "+model.WriteoffPayChannel{}.TableName()+".writeoff_id").
		Joins("JOIN "+model.Users{}.TableName()+" u ON u.id = w.system_user_id").
		Where(model.WriteoffPayChannel{}.TableName()+".pay_channel_id = ?", channelID).
		Where(model.WriteoffPayChannel{}.TableName()+".status = ?", true).
		Where("u.is_active = ? AND u.status = ?", true, true).
		Where("w.balance IS NULL OR w.balance >= ?", money).
		Pluck("writeoff_id", &ids)
	return ids
}

// GetTenantCookie 获取租户cookie
func GetTenantCookie(db *gorm.DB, pluginID uint, tenantID int) *int {
	var cookieIDs []int
	db.Model(&model.TenantCookie{}).
		Where("plugin_id = ? AND status = ? AND tenant_id = ?", pluginID, true, tenantID).
		Pluck("id", &cookieIDs)
	if len(cookieIDs) == 0 {
		return nil
	}
	id := cookieIDs[rand.Intn(len(cookieIDs))]
	return &id
}

// toInt 安全转换为int
func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case uint:
		return int(val)
	case float64:
		return int(val)
	case nil:
		return 0
	default:
		return 0
	}
}

// FormatMoney 金额分转元（保留2位小数字符串）
func FormatMoney(money int) string {
	return fmt.Sprintf("%.2f", float64(money)/100)
}

// PluginCreateOrder 通过插件创建订单（收银台调用）
// 对应 Django 的 plugin_create_order(order_no, raw_order_no, ip, ua)
func PluginCreateOrder(db *gorm.DB, rdb interface{}, orderNo string, rawOrderNo string, ip string, ua string) map[string]interface{} {
	defaultRes := map[string]interface{}{
		"code": float64(400),
		"msg":  "订单生成失败",
		"data": nil,
	}

	// 查询订单
	var order model.Order
	if err := db.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		log.Printf("PluginCreateOrder: 订单 %s 不存在", orderNo)
		return defaultRes
	}

	// 获取订单详情
	var detail model.OrderDetail
	if err := db.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		log.Printf("PluginCreateOrder: 订单详情 %s 不存在", orderNo)
		return defaultRes
	}

	pluginType := detail.PluginType
	if pluginType == "" {
		log.Printf("PluginCreateOrder: 订单 %s 缺少 plugin_type", orderNo)
		return defaultRes
	}

	responder := GetByKey(pluginType)
	if responder == nil {
		log.Printf("PluginCreateOrder: 未找到 %s 插件", pluginType)
		return defaultRes
	}

	args := CreateOrderArgs{
		RawOrderNo: rawOrderNo,
		OrderNo:    orderNo,
		IP:         ip,
		Money:      order.Money,
	}

	if detail.PluginID != nil {
		args.PluginID = int(*detail.PluginID)
	}
	if detail.ProductID != "" {
		args.ProductID = toInt(detail.ProductID)
	}
	if detail.WriteoffID != nil {
		var writeoff model.WriteOff
		if err := db.First(&writeoff, *detail.WriteoffID).Error; err == nil {
			args.TenantID = int(writeoff.ParentID)
		}
	}
	if detail.DomainID != nil {
		args.DomainID = int(*detail.DomainID)
	}
	if detail.CookieID != "" {
		args.CookieID = toInt(detail.CookieID)
	}

	result, err := responder.CreateOrder(db, args)
	if err != nil {
		log.Printf("PluginCreateOrder: 插件创建订单失败: %v", err)
		return map[string]interface{}{
			"code": float64(400),
			"msg":  fmt.Sprintf("订单生成失败:%v", err),
			"data": nil,
		}
	}

	if result == nil {
		return defaultRes
	}

	res := map[string]interface{}{
		"code": float64(result.Code),
		"msg":  result.Msg,
		"data": result.Data,
	}

	return res
}

// PluginQueryOrder 通过插件查询订单（收银台调用）
// 对应 Django 的 plugin_query_order(order_no)
func PluginQueryOrder(db *gorm.DB, rdb interface{}, orderNo string) {
	var order model.Order
	if err := db.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		log.Printf("PluginQueryOrder: 订单 %s 不存在", orderNo)
		return
	}

	var detail model.OrderDetail
	if err := db.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		log.Printf("PluginQueryOrder: 订单详情 %s 不存在", orderNo)
		return
	}

	pluginType := detail.PluginType
	if pluginType == "" {
		return
	}

	responder := GetByKey(pluginType)
	if responder == nil {
		log.Printf("PluginQueryOrder: 未找到 %s 插件", pluginType)
		return
	}

	// 异步调用查询（对应 Django 的 apply_async）
	go func() {
		args := QueryOrderArgs{
			OrderNo:       orderNo,
			QueryInterval: 5,
			Actively:      false,
		}
		if _, err := responder.QueryOrder(db, args); err != nil {
			log.Printf("PluginQueryOrder: 查询订单 %s 失败: %v", orderNo, err)
		}
	}()
}
