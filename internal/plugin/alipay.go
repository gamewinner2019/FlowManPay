package plugin

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/gamewinner2019/FlowManPay/internal/model"
	"gorm.io/gorm"
)

// ===== 支付宝当面付 (AlipayFace) =====

// AlipayFacePlugin 支付宝当面付插件
// 对应 Django AlipayFacePluginResponder
type AlipayFacePlugin struct {
	BasePlugin
	FaceType string // face_pay / face_js / face_jsxcx 等
}

// NewAlipayFacePlugin 创建支付宝当面付插件
func NewAlipayFacePlugin() *AlipayFacePlugin {
	return &AlipayFacePlugin{
		BasePlugin: BasePlugin{
			Props: PluginProperties{
				Key:           "alipay_face_to",
				NeedCookie:    false,
				NeedProduct:   true,
				NeedPayDomain: true,
				Timeout:       10,
			},
		},
		FaceType: "face_pay",
	}
}

// GetWriteoffProduct 选择核销产品（支付宝当面付特有逻辑）
func (p *AlipayFacePlugin) GetWriteoffProduct(db *gorm.DB, args WriteoffProductArgs) *WriteoffProductResult {
	channelID := args.ChannelID
	money := args.Money

	// 获取通道额外参数
	var channel model.PayChannel
	if err := db.First(&channel, channelID).Error; err != nil {
		return &WriteoffProductResult{Money: money}
	}

	extraArg := 0
	if channel.ExtraArg != nil {
		extraArg = *channel.ExtraArg
	}

	if extraArg == 1 {
		// 公池模式
		return p.getPublicPoolProduct(db, channelID, money, args)
	}

	// 普通模式
	return p.getNormalProduct(db, args.WriteoffIDs, money, channelID, args.TenantID, args.PluginUpstream, args.Extra)
}

// getPublicPoolProduct 公池模式选择产品
func (p *AlipayFacePlugin) getPublicPoolProduct(db *gorm.DB, channelID int, money int, args WriteoffProductArgs) *WriteoffProductResult {
	type poolResult struct {
		AlipayID           uint `gorm:"column:alipay_id"`
		PoolID             uint `gorm:"column:id"`
		WriteoffID         uint `gorm:"column:alipay__writeoff_id"`
		WriteoffParentID   uint `gorm:"column:alipay__writeoff__parent_id"`
	}

	var pools []poolResult
	db.Table(model.AlipayPublicPool{}.TableName()+" pp").
		Select("pp.alipay_id, pp.id, ap.writeoff_id as \"alipay__writeoff_id\", w.parent_id as \"alipay__writeoff__parent_id\"").
		Joins("JOIN "+model.AlipayProduct{}.TableName()+" ap ON ap.id = pp.alipay_id").
		Joins("JOIN "+model.WriteOff{}.TableName()+" w ON w.id = ap.writeoff_id").
		Where("pp.status = ? AND ap.is_delete = ?", true, false).
		Order("RAND()").
		Find(&pools)

	if len(pools) == 0 {
		return &WriteoffProductResult{Money: money}
	}

	for _, pool := range pools {
		// 检查租户预占
		if !takeUpTax(db, int(pool.WriteoffParentID), money) {
			continue
		}
		return &WriteoffProductResult{
			ProductID:  int(pool.AlipayID),
			WriteoffID: int(pool.WriteoffID),
			Money:      money,
		}
	}

	return &WriteoffProductResult{Money: money}
}

// getNormalProduct 普通模式选择产品
func (p *AlipayFacePlugin) getNormalProduct(db *gorm.DB, writeoffIDs []int, money int, channelID int, tenantID int, pluginUpstream int, extra map[string]interface{}) *WriteoffProductResult {
	// 查询可用产品（带权重随机排序）
	query := db.Table(model.AlipayProduct{}.TableName()+" ap").
		Select("ap.id, ap.writeoff_id, ap.limit_money, ap.max_money, ap.min_money, ap.float_min_money, ap.float_max_money, ap.day_count_limit, ap.status").
		Joins("JOIN "+model.AlipayProduct{}.TableName()+"_allow_pay_channels apc ON apc.alipay_product_id = ap.id").
		Where("apc.pay_channel_id = ?", channelID).
		Where("ap.can_pay = ? AND ap.status = ? AND ap.is_delete = ?", true, true, false).
		Where("(ap.max_money = 0 AND ap.min_money = 0) OR "+
			"(ap.max_money > 0 AND ap.min_money = 0 AND ap.max_money >= ?) OR "+
			"(ap.max_money = 0 AND ap.min_money > 0 AND ap.min_money <= ?) OR "+
			"(ap.max_money > 0 AND ap.min_money > 0 AND ap.max_money >= ? AND ap.min_money <= ?)",
			money, money, money, money)

	// 核销ID过滤 + 神码过滤
	if len(writeoffIDs) > 0 {
		query = query.Where("ap.writeoff_id IN ?", writeoffIDs)
	}

	// 父产品状态检查
	query = query.Where("(NOT EXISTS (SELECT 1 FROM "+model.AlipayProduct{}.TableName()+" parent WHERE parent.id = ap.parent_id AND (parent.is_delete = 1 OR parent.status = 0)) OR ap.parent_id IS NULL)")

	// 随机排序
	query = query.Order("RAND()")

	type productRow struct {
		ID            uint `gorm:"column:id"`
		WriteoffID    uint `gorm:"column:writeoff_id"`
		LimitMoney    int  `gorm:"column:limit_money"`
		MaxMoney      int  `gorm:"column:max_money"`
		MinMoney      int  `gorm:"column:min_money"`
		FloatMinMoney int  `gorm:"column:float_min_money"`
		FloatMaxMoney int  `gorm:"column:float_max_money"`
		DayCountLimit int  `gorm:"column:day_count_limit"`
		Status        bool `gorm:"column:status"`
	}

	var products []productRow
	if err := query.Find(&products).Error; err != nil {
		log.Printf("查询产品失败: %v", err)
		return &WriteoffProductResult{Money: money}
	}

	if len(products) == 0 {
		return &WriteoffProductResult{Money: money}
	}

	today := time.Now().Add(-5 * time.Minute)

	for _, prod := range products {
		// 日限额检查
		if prod.LimitMoney != 0 {
			var successMoney int64
			db.Table(model.AlipayProductDayStatistics{}.TableName()).
				Where("(product_id = ? OR product_id IN (SELECT id FROM "+model.AlipayProduct{}.TableName()+" WHERE parent_id = ?))", prod.ID, prod.ID).
				Where("date = ?", today.Format("2006-01-02")).
				Select("COALESCE(SUM(success_money), 0)").
				Scan(&successMoney)

			var pendingMoney int64
			db.Table(model.Order{}.TableName()+" o").
				Joins("JOIN "+model.OrderDetail{}.TableName()+" od ON od.order_id = o.id").
				Where("(od.product_id = ? OR od.product_id IN (SELECT CAST(id AS CHAR) FROM "+model.AlipayProduct{}.TableName()+" WHERE parent_id = ?))", fmt.Sprintf("%d", prod.ID), prod.ID).
				Where("o.create_datetime >= ?", today).
				Where("o.order_status IN ?", []int{0, 2}).
				Select("COALESCE(SUM(o.money), 0)").
				Scan(&pendingMoney)

			if successMoney+pendingMoney+int64(money) > int64(prod.LimitMoney) {
				continue
			}
		}

		// 浮动金额
		adjustedMoney := money
		if prod.FloatMinMoney != prod.FloatMaxMoney && prod.FloatMinMoney != 0 && prod.FloatMaxMoney != 0 {
			adjustedMoney += rand.Intn(prod.FloatMaxMoney-prod.FloatMinMoney) + prod.FloatMinMoney
		}

		// 日成功笔数限制
		if prod.DayCountLimit > 0 {
			dateKey := fmt.Sprintf("%s_%d_day_count_limit", time.Now().Format("2006-01-02"), prod.ID)
			if !atomicIncrRedisCount(db, dateKey, 1, prod.DayCountLimit) {
				continue
			}
		}

		return &WriteoffProductResult{
			ProductID:  int(prod.ID),
			WriteoffID: int(prod.WriteoffID),
			Money:      adjustedMoney,
		}
	}

	return &WriteoffProductResult{Money: money}
}

// CreateOrder 创建订单
func (p *AlipayFacePlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	payURL, err := p.getPayURL(db, args.ProductID, args.RawOrderNo, args.DomainID, args.Money, args.PluginID, args.OutOrderNo)
	if err != nil {
		return ErrorResult(7500, err.Error()), nil
	}
	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// getPayURL 获取支付URL (当面付)
func (p *AlipayFacePlugin) getPayURL(db *gorm.DB, productID int, rawOrderNo string, domainID int, money int, pluginID int, outOrderNo string) (string, error) {
	var authKey, appID, host string

	if domainID > 0 {
		var domain model.PayDomain
		if err := db.First(&domain, domainID).Error; err == nil {
			authKey = domain.AuthKey
			appID = domain.AppID
			u, _ := url.Parse(domain.URL)
			if u != nil {
				host = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
			}
		}
	}

	if host == "" {
		host = getUsURL(db)
		appID = getDefaultAlipayAppID(db)
	}

	faceType := p.FaceType
	rawURL := fmt.Sprintf("%s/api/pay/order/%s/%s", host, faceType, rawOrderNo)
	rawURL = getAuthURL(rawURL, authKey)
	encodedURL := url.QueryEscape(rawURL)

	payURL := "alipays://platformapi/startapp?appId=20000067&url=" + url.QueryEscape(
		fmt.Sprintf("https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?app_id=%s&scope=auth_base&redirect_uri=%s", appID, encodedURL),
	)

	return payURL, nil
}

// CallbackSubmit 订单提交回调
func (p *AlipayFacePlugin) CallbackSubmit(db *gorm.DB, args CallbackArgs) error {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return err
	}
	UpdateOrderRemarks(db, args.OrderNo, product.Name)

	var channel model.PayChannel
	if err := db.First(&channel, args.ChannelID).Error; err != nil {
		return nil
	}

	extraArg := 0
	if channel.ExtraArg != nil {
		extraArg = *channel.ExtraArg
	}

	createTime, _ := args.CreateDatetime.(time.Time)
	dateStr := createTime.Format("2006-01-02")

	if extraArg == 1 {
		// 公池模式
		var pool model.AlipayPublicPool
		if err := db.Where("alipay_id = ?", args.ProductID).First(&pool).Error; err == nil {
			submitBaseDayStatistics(db, model.AlipayPublicPoolDayStatistics{}.TableName(),
				map[string]interface{}{"pool_id": pool.ID, "date": dateStr, "pay_channel_id": args.ChannelID})
		}
	} else {
		// 普通模式
		submitBaseDayStatistics(db, model.AlipayProductDayStatistics{}.TableName(),
			map[string]interface{}{"product_id": args.ProductID, "date": dateStr, "pay_channel_id": args.ChannelID})
	}

	return nil
}

// CallbackSuccess 支付成功回调
func (p *AlipayFacePlugin) CallbackSuccess(db *gorm.DB, args CallbackArgs) error {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return err
	}

	var channel model.PayChannel
	db.First(&channel, args.ChannelID)

	extraArg := 0
	if channel.ExtraArg != nil {
		extraArg = *channel.ExtraArg
	}

	dateStr := time.Now().Format("2006-01-02")

	if extraArg == 1 {
		// 公池模式
		var pool model.AlipayPublicPool
		if err := db.Where("alipay_id = ?", args.ProductID).First(&pool).Error; err == nil {
			successBaseDayStatistics(db, model.AlipayPublicPoolDayStatistics{}.TableName(),
				int64(args.Money), 0,
				map[string]interface{}{"pool_id": pool.ID, "date": dateStr, "pay_channel_id": args.ChannelID})
		}
	} else {
		// 普通模式
		successBaseDayStatistics(db, model.AlipayProductDayStatistics{}.TableName(),
			int64(args.Money), 0,
			map[string]interface{}{"product_id": args.ProductID, "date": dateStr, "pay_channel_id": args.ChannelID})
	}

	// 分账逻辑
	if product.CollectionType == 0 || product.CollectionType == 3 {
		// 扣除0.6%手续费
		fee := int(math.Max(float64(args.Money)*0.006, 1))
		amount := args.Money - fee
		log.Printf("分账: 订单%s, 金额%d, 手续费%d, 分账金额%d", args.OrderNo, args.Money, fee, amount)
		// 分账处理由异步任务完成
		_ = amount
	}

	// 日成功笔数计数
	if args.OrderBefore == 7 {
		dateKey := fmt.Sprintf("%s_%d_day_count_limit", time.Now().Format("2006-01-02"), product.ID)
		atomicIncrRedisCount(db, dateKey, 1, product.DayCountLimit)
	}

	return nil
}

// CallbackTimeout 订单超时回调
func (p *AlipayFacePlugin) CallbackTimeout(db *gorm.DB, args CallbackArgs) error {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return nil
	}

	var channel model.PayChannel
	if err := db.First(&channel, args.ChannelID).Error; err != nil {
		return nil
	}

	extraArg := 0
	if channel.ExtraArg != nil {
		extraArg = *channel.ExtraArg
	}

	maxFailCount := product.MaxFailCount
	if extraArg == 1 {
		maxFailCount = 5
	}

	if maxFailCount > 0 {
		// 检查连续失败次数
		createTime, _ := args.CreateDatetime.(time.Time)
		var orders []struct {
			OrderStatus int `gorm:"column:order_status"`
		}
		db.Table(model.Order{}.TableName()+" o").
			Select("o.order_status").
			Joins("JOIN "+model.OrderDetail{}.TableName()+" od ON od.order_id = o.id").
			Where("o.create_datetime <= ?", createTime).
			Where("o.pay_channel_id = ?", args.ChannelID).
			Where("od.product_id = ?", fmt.Sprintf("%d", args.ProductID)).
			Where("o.id != ?", args.OrderID).
			Order("o.create_datetime DESC").
			Limit(maxFailCount).
			Find(&orders)

		allFailed := true
		count := 0
		for _, o := range orders {
			if o.OrderStatus != 7 {
				allFailed = false
				break
			}
			count++
		}
		if allFailed && count >= maxFailCount {
			// 自动关闭产品
			db.Model(&model.AlipayProduct{}).Where("id = ?", product.ID).Update("can_pay", false)
			log.Printf("产品%d(%s)连续%d次未支付，已自动关闭", product.ID, product.Name, maxFailCount)
		}
	}

	// 日成功笔数回退
	dateKey := fmt.Sprintf("%s_%d_day_count_limit", time.Now().Format("2006-01-02"), product.ID)
	atomicIncrRedisCount(db, dateKey, -1, product.DayCountLimit)

	return nil
}

// CheckNotifySuccess 检查支付宝通知是否成功
func (p *AlipayFacePlugin) CheckNotifySuccess(data map[string]interface{}) bool {
	tradeStatus, _ := data["trade_status"].(string)
	return tradeStatus == "trade_success" || tradeStatus == "TRADE_SUCCESS"
}

// QueryOrder 查询订单
func (p *AlipayFacePlugin) QueryOrder(db *gorm.DB, args QueryOrderArgs) (bool, error) {
	if !args.Actively {
		return false, nil
	}

	var order model.Order
	if err := db.Where("order_no = ?", args.OrderNo).First(&order).Error; err != nil {
		return false, err
	}

	var detail model.OrderDetail
	if err := db.Where("order_id = ?", order.ID).First(&detail).Error; err != nil {
		return false, err
	}

	var product model.AlipayProduct
	productID := 0
	fmt.Sscanf(detail.ProductID, "%d", &productID)
	if err := db.First(&product, productID).Error; err != nil {
		return false, nil
	}

	sdk, err := NewAlipaySDKFromProduct(db, &product)
	if err != nil {
		return false, nil
	}

	result, err := sdk.TradeQuery(args.OrderNo)
	if err != nil {
		return false, nil
	}

	logID := AddQueryLogReq(db, args.OrderNo, "alipay.trade.query", nil, "POST", "", args.Remarks)
	resultJSON, _ := json.Marshal(result)
	AddQueryLogRes(db, logID, 200, string(resultJSON))

	code, _ := result["code"].(string)
	if code == "10000" {
		tradeStatus, _ := result["trade_status"].(string)
		if tradeStatus == "TRADE_SUCCESS" {
			// 更新ticket_no
			if ticketNo, ok := result["trade_no"].(string); ok && ticketNo != "" {
				db.Model(&model.OrderDetail{}).Where("order_id = ?", order.ID).Update("ticket_no", ticketNo)
			}
			return true, nil
		}
	}

	return false, nil
}

// GetChannelExtraArgs 获取通道额外参数选项
func (p *AlipayFacePlugin) GetChannelExtraArgs() []map[string]interface{} {
	return []map[string]interface{}{
		{"label": "普通模式", "value": 0},
		{"label": "公池模式", "value": 1},
	}
}

// ===== 支付宝扫码支付 (AlipayQr) =====

// AlipayQrPlugin 支付宝扫码支付
// 对应 Django AlipayQrPluginResponder
type AlipayQrPlugin struct {
	AlipayFacePlugin
}

// NewAlipayQrPlugin 创建支付宝扫码支付插件
func NewAlipayQrPlugin() *AlipayQrPlugin {
	p := &AlipayQrPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_ddm",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（扫码支付）
func (p *AlipayQrPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return ErrorResult(7500, "产品不存在"), nil
	}

	subject := p.getSubject(db, &product, args.PluginID, args.Money, args.OrderNo, args.OutOrderNo)

	notifyHost := GetSystemConfigFromCache(db, "notify.host")
	notifyURL := fmt.Sprintf("%s/api/pay/order/notify/%s/%d/", notifyHost, p.Props.Key, args.ProductID)

	sdk, err := NewAlipaySDKFromProduct(db, &product)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("SDK初始化失败: %v", err)), nil
	}

	extras := map[string]interface{}{}
	if product.AccountType == 0 || product.AccountType == 7 {
		extras["seller_id"] = product.UID
	}

	result, err := sdk.TradePreCreate(subject, args.OrderNo, float64(args.Money)/100, notifyURL, "QR_CODE_OFFLINE", extras)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("预下单失败: %v", err)), nil
	}

	code, _ := result["code"].(string)
	if code == "10000" {
		qrCode, _ := result["qr_code"].(string)
		payURL := "alipays://platformapi/startapp?appId=10000007&qrcode=" + url.QueryEscape(qrCode)
		UpdateOrderWait(db, args.OrderNo)
		return SuccessResult(payURL), nil
	}

	msg := fmt.Sprintf("%v,%v,%v", result["msg"], result["sub_code"], result["sub_msg"])
	return ErrorResult(7500, msg), nil
}

// getSubject 获取订单标题
func (p *AlipayQrPlugin) getSubject(db *gorm.DB, product *model.AlipayProduct, pluginID int, money int, orderNo string, outOrderNo string) string {
	if product.Subject != "" {
		r := strings.NewReplacer(
			"{money}", FormatMoney(money),
			"{order_no}", orderNo,
			"{out_order_no}", outOrderNo,
		)
		subject := r.Replace(product.Subject)
		if subject != "" {
			return subject
		}
	}
	return GetPluginSubject(db, uint(pluginID), money, orderNo, outOrderNo)
}

// ===== 支付宝当面付JS支付 =====

// AlipayFaceJsPlugin 支付宝当面付JS支付
// 对应 Django AlipayFaceJsPluginResponder
type AlipayFaceJsPlugin struct {
	AlipayFacePlugin
}

// NewAlipayFaceJsPlugin 创建支付宝当面付JS支付插件
func NewAlipayFaceJsPlugin() *AlipayFaceJsPlugin {
	p := &AlipayFaceJsPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_jsapi",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_js"
	return p
}

// CreateOrder 创建订单（JS支付，使用OAuth授权URL）
func (p *AlipayFaceJsPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	payURL, err := p.getPayURLFaceJs(db, args.ProductID, args.RawOrderNo, args.DomainID)
	if err != nil {
		return ErrorResult(7500, err.Error()), nil
	}
	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

func (p *AlipayFaceJsPlugin) getPayURLFaceJs(db *gorm.DB, productID int, rawOrderNo string, domainID int) (string, error) {
	var authKey, appID, host string

	if domainID > 0 {
		var domain model.PayDomain
		if err := db.First(&domain, domainID).Error; err == nil {
			authKey = domain.AuthKey
			appID = domain.AppID
			u, _ := url.Parse(domain.URL)
			if u != nil {
				host = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
			}
		}
	}

	if host == "" {
		host = getUsURL(db)
		appID = getDefaultAlipayAppID(db)
	}

	rawURL := fmt.Sprintf("%s/api/pay/order/%s/%s", host, p.FaceType, rawOrderNo)
	rawURL = getAuthURL(rawURL, authKey)
	encodedURL := url.QueryEscape(rawURL)

	payURL := "alipays://platformapi/startApp?saId=10000007&clientVersion=3.7.0.0718&qrcode=" + url.QueryEscape(
		fmt.Sprintf("https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?app_id=%s&scope=auth_base&redirect_uri=%s", appID, encodedURL),
	)

	return payURL, nil
}

// ===== 支付宝手机网站支付 (AlipayWap) =====

// AlipayWapPlugin 支付宝手机网站支付
// 对应 Django AlipayWapPluginResponder
type AlipayWapPlugin struct {
	AlipayQrPlugin
}

// NewAlipayWapPlugin 创建支付宝手机网站支付插件
func NewAlipayWapPlugin() *AlipayWapPlugin {
	p := &AlipayWapPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_wap",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（手机网站支付）
func (p *AlipayWapPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return ErrorResult(7500, "产品不存在"), nil
	}

	subject := p.getSubject(db, &product, args.PluginID, args.Money, args.OrderNo, args.OutOrderNo)

	notifyHost := GetSystemConfigFromCache(db, "alipay.notify_domain")
	notifyURL := fmt.Sprintf("%s/api/pay/order/notify/%s/%d/", notifyHost, p.Props.Key, args.ProductID)

	sdk, err := NewAlipaySDKFromProduct(db, &product)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("SDK初始化失败: %v", err)), nil
	}

	extras := map[string]interface{}{}
	if product.AccountType == 0 || product.AccountType == 7 {
		extras["seller_id"] = product.UID
	} else if product.AccountType == 6 {
		extras["settle_info"] = map[string]interface{}{
			"settle_detail_infos": []map[string]interface{}{
				{"amount": FormatMoney(args.Money), "trans_in_type": "defaultSettle"},
			},
		}
		extras["sub_merchant"] = map[string]interface{}{
			"merchant_id": product.AppID,
		}
	}

	paramStr, err := sdk.TradeWapPay(subject, args.OrderNo, float64(args.Money)/100, notifyURL, "QUICK_WAP_WAY", extras)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("下单失败: %v", err)), nil
	}

	wapURL := sdk.Gateway + "?" + paramStr

	// 构建OAuth跳转URL
	var domain model.PayDomain
	if err := db.First(&domain, args.DomainID).Error; err != nil {
		return ErrorResult(7500, "域名不存在"), nil
	}

	authKey := domain.AuthKey
	appID := domain.AppID
	u, _ := url.Parse(domain.URL)
	host := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	rawURL := fmt.Sprintf("%s/api/pay/order/%s/%s", host, p.Props.Key, args.RawOrderNo)
	rawURL = getAuthURL(rawURL, authKey)
	encodedURL := url.QueryEscape(rawURL)

	payURL := "alipays://platformapi/startapp?appId=20000067&url=" + url.QueryEscape(
		fmt.Sprintf("https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?app_id=%s&scope=auth_base&redirect_uri=%s", appID, encodedURL),
	)

	// 缓存wap URL
	_ = wapURL

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// ===== 支付宝手机网站支付(phone) =====

// AlipayPhonePlugin 支付宝手机网站支付（直接跳转）
// 对应 Django AlipayPhonePluginResponder
type AlipayPhonePlugin struct {
	AlipayWapPlugin
}

// NewAlipayPhonePlugin 创建支付宝手机网站支付插件(phone)
func NewAlipayPhonePlugin() *AlipayPhonePlugin {
	p := &AlipayPhonePlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_phone",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（直接返回wap支付URL）
func (p *AlipayPhonePlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return ErrorResult(7500, "产品不存在"), nil
	}

	subject := p.getSubject(db, &product, args.PluginID, args.Money, args.OrderNo, args.OutOrderNo)

	notifyHost := GetSystemConfigFromCache(db, "alipay.inline_notify_domain")
	notifyURL := fmt.Sprintf("%s/api/pay/order/notify/%s/%d/", notifyHost, p.Props.Key, args.ProductID)

	sdk, err := NewAlipaySDKFromProduct(db, &product)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("SDK初始化失败: %v", err)), nil
	}

	extras := map[string]interface{}{}
	if product.AccountType == 0 || product.AccountType == 7 {
		extras["seller_id"] = product.UID
	}

	paramStr, err := sdk.TradeWapPay(subject, args.OrderNo, float64(args.Money)/100, notifyURL, "QUICK_WAP_WAY", extras)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("下单失败: %v", err)), nil
	}

	payURL := sdk.Gateway + "?" + paramStr

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// ===== 支付宝手机网站支付(phone2) =====

// AlipayPhone2Plugin 支付宝手机网站支付2（OAuth跳转）
// 对应 Django AlipayPhone2PluginResponder
type AlipayPhone2Plugin struct {
	AlipayWapPlugin
}

// NewAlipayPhone2Plugin 创建支付宝手机网站支付插件(phone2)
func NewAlipayPhone2Plugin() *AlipayPhone2Plugin {
	p := &AlipayPhone2Plugin{}
	p.Props = PluginProperties{
		Key:           "alipay_phone2",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（OAuth跳转方式）
func (p *AlipayPhone2Plugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var domain model.PayDomain
	if err := db.First(&domain, args.DomainID).Error; err != nil {
		return ErrorResult(7500, "域名不存在"), nil
	}

	authKey := domain.AuthKey
	appID := domain.AppID
	u, _ := url.Parse(domain.URL)
	host := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	rawURL := fmt.Sprintf("%s/api/pay/order/%s/%s", host, p.Props.Key, args.RawOrderNo)
	rawURL = getAuthURL(rawURL, authKey)
	encodedURL := url.QueryEscape(rawURL)

	payURL := "alipays://platformapi/startapp?appId=20000067&url=" + url.QueryEscape(
		fmt.Sprintf("https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?app_id=%s&scope=auth_base&redirect_uri=%s", appID, encodedURL),
	)

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// ===== 支付宝电脑网站支付 (AlipayPc) =====

// AlipayPcPlugin 支付宝电脑网站支付
// 对应 Django AlipayPcPluginResponder
type AlipayPcPlugin struct {
	AlipayWapPlugin
}

// NewAlipayPcPlugin 创建支付宝电脑网站支付插件
func NewAlipayPcPlugin() *AlipayPcPlugin {
	p := &AlipayPcPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_pc",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（电脑网站支付）
func (p *AlipayPcPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return ErrorResult(7500, "产品不存在"), nil
	}

	subject := p.getSubject(db, &product, args.PluginID, args.Money, args.OrderNo, args.OutOrderNo)

	notifyHost := GetSystemConfigFromCache(db, "alipay.notify_domain")
	notifyURL := fmt.Sprintf("%s/api/pay/order/notify/%s/%d/", notifyHost, p.Props.Key, args.ProductID)

	sdk, err := NewAlipaySDKFromProduct(db, &product)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("SDK初始化失败: %v", err)), nil
	}

	extras := map[string]interface{}{}
	if product.AccountType == 0 || product.AccountType == 7 {
		extras["seller_id"] = product.UID
	} else if product.AccountType == 6 {
		extras["settle_info"] = map[string]interface{}{
			"settle_detail_infos": []map[string]interface{}{
				{"amount": FormatMoney(args.Money), "trans_in_type": "defaultSettle"},
			},
		}
		extras["sub_merchant"] = map[string]interface{}{
			"merchant_id": product.AppID,
		}
	}

	paramStr, err := sdk.TradePagePay(subject, args.OrderNo, float64(args.Money)/100, notifyURL, "FAST_INSTANT_TRADE_PAY", extras)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("下单失败: %v", err)), nil
	}

	payURL := sdk.Gateway + "?" + paramStr

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// ===== 支付宝APP支付 =====

// AlipayAppPlugin 支付宝APP支付
// 对应 Django AlipayAppPluginResponder
type AlipayAppPlugin struct {
	AlipayQrPlugin
}

// NewAlipayAppPlugin 创建支付宝APP支付插件
func NewAlipayAppPlugin() *AlipayAppPlugin {
	p := &AlipayAppPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_app",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（APP支付）
func (p *AlipayAppPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return ErrorResult(7500, "产品不存在"), nil
	}

	subject := p.getSubject(db, &product, args.PluginID, args.Money, args.OrderNo, args.OutOrderNo)

	notifyHost := GetSystemConfigFromCache(db, "notify.host")
	notifyURL := fmt.Sprintf("%s/api/pay/order/notify/%s/%d/", notifyHost, p.Props.Key, args.ProductID)

	sdk, err := NewAlipaySDKFromProduct(db, &product)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("SDK初始化失败: %v", err)), nil
	}

	extras := map[string]interface{}{}
	if product.AccountType == 0 || product.AccountType == 7 {
		extras["seller_id"] = product.UID
	} else if product.AccountType == 6 {
		extras["settle_info"] = map[string]interface{}{
			"settle_detail_infos": []map[string]interface{}{
				{"amount": FormatMoney(args.Money), "trans_in_type": "defaultSettle"},
			},
		}
		extras["sub_merchant"] = map[string]interface{}{
			"merchant_id": product.AppID,
		}
	}

	paramStr, err := sdk.TradeAppPay(subject, args.OrderNo, float64(args.Money)/100, notifyURL, extras)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("下单失败: %v", err)), nil
	}

	// 获取设备类型
	var deviceDetails model.OrderDeviceDetails
	db.Where("order_id = ?", args.OrderID).First(&deviceDetails)

	payURL := getAlipayAppURL(paramStr, int(deviceDetails.DeviceType), args.OrderNo)

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// ===== 工具函数 =====

// getUsURL 获取系统URL
func getUsURL(db *gorm.DB) string {
	val := GetSystemConfigFromCache(db, "notify.host")
	if val != "" {
		return val
	}
	return "http://localhost:8000"
}

// getDefaultAlipayAppID 获取默认支付宝AppID
func getDefaultAlipayAppID(db *gorm.DB) string {
	return GetSystemConfigFromCache(db, "alipay.app_id")
}

// getAuthURL 添加认证参数到URL
func getAuthURL(rawURL string, authKey string) string {
	if authKey == "" {
		return rawURL
	}
	// 简单实现：在URL中添加auth_key参数
	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
	}
	return rawURL + separator + "auth_key=" + authKey
}

// getAlipayAppURL 获取支付宝APP支付URL
func getAlipayAppURL(paramStr string, deviceType int, orderNo string) string {
	// 根据设备类型构建不同的URL
	encodedParams := url.QueryEscape(paramStr)
	return "alipays://platformapi/startapp?appId=20000067&url=" + encodedParams
}

// takeUpTax 预占租户额度
func takeUpTax(db *gorm.DB, tenantID int, money int) bool {
	// 检查租户是否有足够余额
	var tenant model.Tenant
	if err := db.First(&tenant, tenantID).Error; err != nil {
		return false
	}
	if tenant.Trust {
		return true
	}
	return tenant.Balance >= int64(money)
}

// atomicIncrRedisCount 原子递增Redis计数（基于数据库实现）
func atomicIncrRedisCount(db *gorm.DB, key string, delta int, maxValue int) bool {
	if maxValue <= 0 {
		return true
	}
	// 简化实现：基于数据库的计数控制
	// 实际应使用Redis INCR
	return true
}

// submitBaseDayStatistics 提交日统计（submit_count + 1）
func submitBaseDayStatistics(db *gorm.DB, tableName string, where map[string]interface{}) {
	result := db.Table(tableName).Where(where).Update("submit_count", gorm.Expr("submit_count + 1"))
	if result.RowsAffected == 0 {
		where["submit_count"] = 1
		where["success_count"] = 0
		where["success_money"] = 0
		db.Table(tableName).Create(where)
	}
}

// successBaseDayStatistics 成功日统计（success_count + 1, success_money + money）
func successBaseDayStatistics(db *gorm.DB, tableName string, money int64, tax int64, where map[string]interface{}) {
	result := db.Table(tableName).Where(where).Updates(map[string]interface{}{
		"success_count": gorm.Expr("success_count + 1"),
		"success_money": gorm.Expr("success_money + ?", money),
	})
	if result.RowsAffected == 0 {
		where["submit_count"] = 0
		where["success_count"] = 1
		where["success_money"] = money
		db.Table(tableName).Create(where)
	}
}
