package plugin

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/gamewinner2019/FlowManPay/internal/model"
	"gorm.io/gorm"
)

// ===== 支付宝黄金UID (AlipayGold) =====

// AlipayGoldPlugin 支付宝黄金UID支付
// 对应 Django AlipayGoldPluginResponder
type AlipayGoldPlugin struct {
	AlipayFacePlugin
}

// NewAlipayGoldPlugin 创建支付宝黄金UID插件
func NewAlipayGoldPlugin() *AlipayGoldPlugin {
	p := &AlipayGoldPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_gold",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: false,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（黄金UID - 直接转账链接）
func (p *AlipayGoldPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return ErrorResult(7500, "产品不存在"), nil
	}

	UpdateOrderRemarks(db, args.OrderNo, product.Name)

	money := FormatMoney(args.Money)

	// 构建支付宝转账URL
	payURL := fmt.Sprintf(
		"https://ds.alipay.com/?from=pc&appId=20000116&actionType=toAccount&goBack=NO&amount=%s&userId=%s&memo=%s",
		money, product.UID, args.RawOrderNo,
	)

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// QueryOrder 查询订单（黄金UID - 通过账户流水查询）
func (p *AlipayGoldPlugin) QueryOrder(db *gorm.DB, args QueryOrderArgs) (bool, error) {
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

	// 通过账户流水查询
	outTime := 600
	if args.Actively {
		outTime = 1200
	}
	endTime := order.CreateDatetime.Time.Add(time.Duration(outTime) * time.Second)

	pageNo := 1
	errorCount := 0
	for i := 0; i < 200; i++ {
		result, err := sdk.BillAccountLogQuery(order.CreateDatetime.Time, endTime, pageNo)
		if err != nil {
			return false, nil
		}

		logID := AddQueryLogReq(db, args.OrderNo, "alipay.data.bill.accountlog.query",
			map[string]interface{}{"page_no": pageNo}, "GET", "", args.Remarks)

		code, _ := result["code"].(string)
		if code != "10000" {
			msg := fmt.Sprintf("查询订单失败,订单号:%s,错误信息:%v", args.OrderNo, result)
			AddQueryLogRes(db, logID, 200, msg)
			errorCount++
			if errorCount > 1 {
				break
			}
			continue
		}

		detailList, _ := result["detail_list"].([]interface{})
		for _, item := range detailList {
			de, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			direction, _ := de["direction"].(string)
			transType, _ := de["type"].(string)
			transMemo, _ := de["trans_memo"].(string)
			transAmount, _ := de["trans_amount"].(string)

			if direction != "收入" || transType != "转账" {
				continue
			}

			if transMemo == args.OrderNo && transAmount == FormatMoney(int(order.Money)) {
				resultJSON, _ := json.Marshal(de)
				AddQueryLogRes(db, logID, 200, string(resultJSON))
				return true, nil
			}
		}

		AddQueryLogRes(db, logID, 200, "暂未查询到")
		errorCount = 0

		// 分页检查
		totalSize := toInt(result["total_size"])
		pageSize := toInt(result["page_size"])
		if pageSize > 0 && (pageNo-1)*pageSize < totalSize {
			pageNo++
		} else {
			break
		}
	}

	return false, nil
}

// ===== 支付宝当面付QR (AlipayFaceQr) =====

// AlipayFaceQrPlugin 支付宝当面付QR支付
// 对应 Django AlipayFaceQrPluginResponder
type AlipayFaceQrPlugin struct {
	AlipayQrPlugin
}

// NewAlipayFaceQrPlugin 创建支付宝当面付QR插件
func NewAlipayFaceQrPlugin() *AlipayFaceQrPlugin {
	p := &AlipayFaceQrPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_face_qr",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（当面付QR - 返回纯QR码URL）
func (p *AlipayFaceQrPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
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

	result, err := sdk.TradePreCreate(subject, args.OrderNo, float64(args.Money)/100, notifyURL, "FACE_TO_FACE_PAYMENT", extras)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("预下单失败: %v", err)), nil
	}

	code, _ := result["code"].(string)
	if code == "10000" {
		qrCode, _ := result["qr_code"].(string)
		// 返回纯QR码URL（不跳转支付宝）
		UpdateOrderWait(db, args.OrderNo)
		return SuccessResult(qrCode), nil
	}

	subMsg, _ := result["sub_msg"].(string)
	return ErrorResult(7500, subMsg), nil
}

// ===== 支付宝名片UID (AlipayCardUid) =====

// AlipayCardUidPlugin 支付宝名片UID支付
// 对应 Django AlipayCardUidPluginResponder
type AlipayCardUidPlugin struct {
	AlipayGoldPlugin
}

// NewAlipayCardUidPlugin 创建支付宝名片UID插件
func NewAlipayCardUidPlugin() *AlipayCardUidPlugin {
	p := &AlipayCardUidPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_card_uid",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: false,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（名片UID）
func (p *AlipayCardUidPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return ErrorResult(7500, "产品不存在"), nil
	}

	money := float64(args.Money) / 100

	// 构建直接转账URL
	transferURL := fmt.Sprintf(
		"https://ds.alipay.com/?from=pc&appId=20000116&actionType=toAccount&goBack=NO&amount=%.2f&userId=%s&memo=%s",
		money, product.UID, args.RawOrderNo,
	)

	UpdateOrderRemarks(db, args.OrderNo, product.Name)

	// 包装为支付宝扫码URL
	payURL := "alipays://platformapi/startapp?appId=10000007&qrcode=" + url.QueryEscape(transferURL)

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// ===== 支付宝确认单UID (AlipayConfirmUid) =====

// AlipayConfirmUidPlugin 支付宝确认单UID支付
// 对应 Django AlipayConfirmUidPluginResponder
type AlipayConfirmUidPlugin struct {
	AlipayGoldPlugin
}

// NewAlipayConfirmUidPlugin 创建支付宝确认单UID插件
func NewAlipayConfirmUidPlugin() *AlipayConfirmUidPlugin {
	p := &AlipayConfirmUidPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_confirm",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: false,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（确认单UID）
func (p *AlipayConfirmUidPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return ErrorResult(7500, "产品不存在"), nil
	}

	money := float64(args.Money) / 100

	// 构建确认转账数据
	data := map[string]interface{}{
		"productCode": "TRANSFER_TO_ALIPAY_ACCOUNT",
		"bizScene":    "YUEBAO",
		"outBizNo":    "",
		"transAmount": fmt.Sprintf("%.2f", money),
		"remark":      args.RawOrderNo,
		"businessParams": map[string]interface{}{
			"returnUrl": "alipays://platformapi/startApp?appId=20000218&bizScenario=transoutXtrans",
		},
		"payeeInfo": map[string]interface{}{
			"identity":     product.UID,
			"identityType": "ALIPAY_USER_ID",
		},
	}

	dataJSON, _ := json.Marshal(data)
	encodedData := url.QueryEscape(string(dataJSON))

	transferURL := "https://render.alipay.com/p/yuyan/180020010001206672/rent-index.html?formData=" + encodedData

	UpdateOrderRemarks(db, args.OrderNo, product.Name)

	payURL := "alipays://platformapi/startapp?appId=10000007&qrcode=" + url.QueryEscape(transferURL)

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// ===== 支付宝预授权 (AlipayPre) =====

// AlipayPrePlugin 支付宝预授权支付
// 对应 Django AlipayPrePluginResponder
type AlipayPrePlugin struct {
	AlipayFacePlugin
}

// NewAlipayPrePlugin 创建支付宝预授权插件
func NewAlipayPrePlugin() *AlipayPrePlugin {
	p := &AlipayPrePlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_ysq",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// CreateOrder 创建订单（预授权）
func (p *AlipayPrePlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var domain model.PayDomain
	if err := db.First(&domain, args.DomainID).Error; err != nil {
		return ErrorResult(7500, "域名不存在"), nil
	}

	authKey := domain.AuthKey
	u, _ := url.Parse(domain.URL)
	host := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return ErrorResult(7500, "产品不存在"), nil
	}

	subject := p.getPreSubject(db, &product, args.PluginID, args.Money, args.OrderNo, args.OutOrderNo)
	amount := float64(args.Money) / 100

	sdk, err := NewAlipaySDKFromProduct(db, &product)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("SDK初始化失败: %v", err)), nil
	}

	notifyHost := GetSystemConfigFromCache(db, "alipay.notify_domain")
	notifyURL := fmt.Sprintf("%s/api/business/alipay/notify/pre/%d/%s/", notifyHost, args.ProductID, args.RawOrderNo)

	_, err = sdk.FundAuthOrderAppFreeze(args.RawOrderNo, args.RawOrderNo, subject, amount, "PREAUTH_PAY", notifyURL)
	if err != nil {
		return ErrorResult(7500, fmt.Sprintf("预授权冻结失败: %v", err)), nil
	}

	redirectURL := fmt.Sprintf("%s/api/alipay/app/%s/", host, args.RawOrderNo)
	redirectURL = getAuthURL(redirectURL, authKey)

	payURL := "alipays://platformapi/startapp?appId=20000067&url=" + url.QueryEscape(redirectURL)

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

func (p *AlipayPrePlugin) getPreSubject(db *gorm.DB, product *model.AlipayProduct, pluginID int, money int, orderNo string, outOrderNo string) string {
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

// ===== 支付宝JS小程序 (AlipayJsXcx) =====

// AlipayJsXcxPlugin 支付宝JS小程序支付
// 对应 Django AlipayJsXcxPluginResponder
type AlipayJsXcxPlugin struct {
	AlipayFacePlugin
}

// NewAlipayJsXcxPlugin 创建支付宝JS小程序插件
func NewAlipayJsXcxPlugin() *AlipayJsXcxPlugin {
	p := &AlipayJsXcxPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_jsxcx",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_jsxcx"
	return p
}

// CreateOrder 创建订单（JS小程序 - 使用OAuth跳转）
func (p *AlipayJsXcxPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	payURL, err := p.getPayURLJsXcx(db, args.ProductID, args.RawOrderNo, args.DomainID)
	if err != nil {
		return ErrorResult(7500, err.Error()), nil
	}
	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

func (p *AlipayJsXcxPlugin) getPayURLJsXcx(db *gorm.DB, productID int, rawOrderNo string, domainID int) (string, error) {
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

	payURL := "alipays://platformapi/startapp?appId=20000067&url=" + url.QueryEscape(
		fmt.Sprintf("https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?app_id=%s&scope=auth_base&redirect_uri=%s", appID, encodedURL),
	)

	return payURL, nil
}

// ===== 支付宝C2C红包 (AlipayC2cHongBao) =====

// AlipayC2cHongBaoPlugin 支付宝C2C红包支付
// 对应 Django AlipayC2cHongBaoPluginResponder
type AlipayC2cHongBaoPlugin struct {
	AlipayFacePlugin
}

// NewAlipayC2cHongBaoPlugin 创建支付宝C2C红包插件
func NewAlipayC2cHongBaoPlugin() *AlipayC2cHongBaoPlugin {
	p := &AlipayC2cHongBaoPlugin{}
	p.Props = PluginProperties{
		Key:           "alipay_c2c",
		NeedCookie:    false,
		NeedProduct:   true,
		NeedPayDomain: true,
		Timeout:       10,
	}
	p.FaceType = "face_pay"
	return p
}

// GetWriteoffProduct 选择核销产品（C2C红包需要证书签名类型产品）
func (p *AlipayC2cHongBaoPlugin) GetWriteoffProduct(db *gorm.DB, args WriteoffProductArgs) *WriteoffProductResult {
	// C2C红包需要sign_type=1的产品
	args.Extra = map[string]interface{}{
		"sign_type": 1,
	}
	return p.AlipayFacePlugin.GetWriteoffProduct(db, args)
}

// CreateOrder 创建订单（C2C红包 - OAuth跳转）
func (p *AlipayC2cHongBaoPlugin) CreateOrder(db *gorm.DB, args CreateOrderArgs) (*CreateOrderResult, error) {
	var domain model.PayDomain
	if err := db.First(&domain, args.DomainID).Error; err != nil {
		return ErrorResult(7500, "域名不存在"), nil
	}

	authKey := domain.AuthKey
	appID := domain.AppID
	u, _ := url.Parse(domain.URL)
	host := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	rawURL := fmt.Sprintf("%s/api/pay/order/alipay_c2c/%s", host, args.RawOrderNo)
	rawURL = getAuthURL(rawURL, authKey)
	encodedURL := url.QueryEscape(rawURL)

	payURL := "alipays://platformapi/startapp?appId=20000067&url=" + url.QueryEscape(
		fmt.Sprintf("https://openauth.alipay.com/oauth2/publicAppAuthorize.htm?app_id=%s&scope=auth_base&redirect_uri=%s", appID, encodedURL),
	)

	UpdateOrderWait(db, args.OrderNo)
	return SuccessResult(payURL), nil
}

// QueryOrder 查询订单（C2C红包 - 通过转账查询API）
func (p *AlipayC2cHongBaoPlugin) QueryOrder(db *gorm.DB, args QueryOrderArgs) (bool, error) {
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

	result, err := sdk.FundTransCommonQuery(args.OrderNo)
	if err != nil {
		return false, nil
	}

	logID := AddQueryLogReq(db, args.OrderNo, "alipay.fund.trans.common.query", nil, "POST", "", args.Remarks)
	resultJSON, _ := json.Marshal(result)
	AddQueryLogRes(db, logID, 200, string(resultJSON))

	code, _ := result["code"].(string)
	if code == "10000" {
		status, _ := result["status"].(string)
		if status == "SUCCESS" {
			ticketNo, _ := result["order_id"].(string)
			if ticketNo != "" {
				db.Model(&model.OrderDetail{}).Where("order_id = ?", order.ID).Update("ticket_no", ticketNo)
			}
			return true, nil
		}
	}

	return false, nil
}

// CallbackSuccess C2C红包成功回调（需要执行C2C转账）
func (p *AlipayC2cHongBaoPlugin) CallbackSuccess(db *gorm.DB, args CallbackArgs) error {
	var product model.AlipayProduct
	if err := db.First(&product, args.ProductID).Error; err != nil {
		return err
	}

	dateStr := time.Now().Format("2006-01-02")
	successBaseDayStatistics(db, model.AlipayProductDayStatistics{}.TableName(),
		int64(args.Money), 0,
		map[string]interface{}{"product_id": args.ProductID, "date": dateStr, "pay_channel_id": args.ChannelID})

	// C2C转账由异步任务处理
	log.Printf("C2C红包成功: 订单%s, 金额%d, 产品%d(%s), 需要执行C2C转账",
		args.OrderNo, args.Money, product.ID, product.Name)

	return nil
}
