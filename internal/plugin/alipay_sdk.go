package plugin

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gamewinner2019/FlowManPay/internal/model"
	"gorm.io/gorm"
)

const (
	alipayGateway      = "https://openapi.alipay.com/gateway.do"
	alipayGatewaySandbox = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
)

// AlipaySDK 支付宝SDK封装
type AlipaySDK struct {
	AppID      string
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
	SignType   string // RSA2
	Gateway    string
	NotifyURL  string
	Proxies    map[string]string
	Debug      bool
}

// NewAlipaySDK 创建支付宝SDK实例
func NewAlipaySDK(appID string, privateKeyStr string, publicKeyStr string, signType int) (*AlipaySDK, error) {
	sdk := &AlipaySDK{
		AppID:    appID,
		SignType: "RSA2",
		Gateway:  alipayGateway,
	}

	// 解析私钥
	privateKey, err := parsePrivateKey(privateKeyStr)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %v", err)
	}
	sdk.PrivateKey = privateKey

	// 解析公钥
	if publicKeyStr != "" {
		publicKey, err := parsePublicKey(publicKeyStr)
		if err != nil {
			log.Printf("解析公钥失败(非致命): %v", err)
		} else {
			sdk.PublicKey = publicKey
		}
	}

	return sdk, nil
}

// NewAlipaySDKFromProduct 从产品配置创建SDK
func NewAlipaySDKFromProduct(db *gorm.DB, product *model.AlipayProduct) (*AlipaySDK, error) {
	var appID, privateKey, publicKey string
	var signType int

	// 子商户/服务商授权商户使用父商户的密钥
	if (product.AccountType == 0 || product.AccountType == 4 || product.AccountType == 6) && product.ParentID != nil {
		var parent model.AlipayProduct
		if err := db.First(&parent, *product.ParentID).Error; err != nil {
			return nil, fmt.Errorf("获取父产品失败: %v", err)
		}
		appID = parent.AppID
		privateKey = parent.PrivateKey
		publicKey = parent.PublicKey
		signType = parent.SignType
	} else if product.AccountType == 7 && product.ParentID != nil {
		var parent model.AlipayProduct
		if err := db.First(&parent, *product.ParentID).Error; err != nil {
			return nil, fmt.Errorf("获取父产品失败: %v", err)
		}
		if parent.ParentID != nil {
			var grandParent model.AlipayProduct
			if err := db.First(&grandParent, *parent.ParentID).Error; err != nil {
				return nil, fmt.Errorf("获取祖父产品失败: %v", err)
			}
			appID = grandParent.AppID
			privateKey = grandParent.PrivateKey
			publicKey = grandParent.PublicKey
			signType = grandParent.SignType
		}
	} else {
		appID = product.AppID
		privateKey = product.PrivateKey
		publicKey = product.PublicKey
		signType = product.SignType
	}

	sdk, err := NewAlipaySDK(appID, privateKey, publicKey, signType)
	if err != nil {
		return nil, err
	}

	// 设置代理
	if product.Proxy != "" {
		sdk.Proxies = map[string]string{
			"http":  product.Proxy,
			"https": product.Proxy,
		}
	}

	return sdk, nil
}

// TradePreCreate 统一收单交易创建（预下单/扫码支付）
func (s *AlipaySDK) TradePreCreate(subject, outTradeNo string, totalAmount float64, notifyURL string, productCode string, extras map[string]interface{}) (map[string]interface{}, error) {
	bizContent := map[string]interface{}{
		"subject":       subject,
		"out_trade_no":  outTradeNo,
		"total_amount":  fmt.Sprintf("%.2f", totalAmount),
		"product_code":  productCode,
	}
	if notifyURL != "" {
		bizContent["notify_url"] = notifyURL
	}
	for k, v := range extras {
		bizContent[k] = v
	}

	return s.execute("alipay.trade.precreate", bizContent)
}

// TradeWapPay 手机网站支付（返回签名后的参数字符串）
func (s *AlipaySDK) TradeWapPay(subject, outTradeNo string, totalAmount float64, notifyURL string, productCode string, extras map[string]interface{}) (string, error) {
	bizContent := map[string]interface{}{
		"subject":       subject,
		"out_trade_no":  outTradeNo,
		"total_amount":  fmt.Sprintf("%.2f", totalAmount),
		"product_code":  productCode,
	}
	for k, v := range extras {
		bizContent[k] = v
	}

	return s.buildRequestParams("alipay.trade.wap.pay", bizContent, notifyURL)
}

// TradePagePay 电脑网站支付（返回签名后的参数字符串）
func (s *AlipaySDK) TradePagePay(subject, outTradeNo string, totalAmount float64, notifyURL string, productCode string, extras map[string]interface{}) (string, error) {
	bizContent := map[string]interface{}{
		"subject":       subject,
		"out_trade_no":  outTradeNo,
		"total_amount":  fmt.Sprintf("%.2f", totalAmount),
		"product_code":  productCode,
	}
	for k, v := range extras {
		bizContent[k] = v
	}

	return s.buildRequestParams("alipay.trade.page.pay", bizContent, notifyURL)
}

// TradeAppPay APP支付（返回签名后的参数字符串）
func (s *AlipaySDK) TradeAppPay(subject, outTradeNo string, totalAmount float64, notifyURL string, extras map[string]interface{}) (string, error) {
	bizContent := map[string]interface{}{
		"subject":       subject,
		"out_trade_no":  outTradeNo,
		"total_amount":  fmt.Sprintf("%.2f", totalAmount),
	}
	for k, v := range extras {
		bizContent[k] = v
	}

	return s.buildRequestParams("alipay.trade.app.pay", bizContent, notifyURL)
}

// TradeQuery 交易查询
func (s *AlipaySDK) TradeQuery(outTradeNo string) (map[string]interface{}, error) {
	bizContent := map[string]interface{}{
		"out_trade_no": outTradeNo,
	}
	return s.execute("alipay.trade.query", bizContent)
}

// BillAccountLogQuery 账户流水查询
func (s *AlipaySDK) BillAccountLogQuery(startTime, endTime time.Time, pageNo int) (map[string]interface{}, error) {
	bizContent := map[string]interface{}{
		"start_time": startTime.Format("2006-01-02 15:04:05"),
		"end_time":   endTime.Format("2006-01-02 15:04:05"),
		"page_no":    pageNo,
		"page_size":  20,
	}
	return s.execute("alipay.data.bill.accountlog.query", bizContent)
}

// FundAuthOrderAppFreeze 资金预授权冻结
func (s *AlipaySDK) FundAuthOrderAppFreeze(outOrderNo, outRequestNo, orderTitle string, amount float64, productCode string, notifyURL string) (string, error) {
	bizContent := map[string]interface{}{
		"out_order_no":   outOrderNo,
		"out_request_no": outRequestNo,
		"order_title":    orderTitle,
		"amount":         fmt.Sprintf("%.2f", amount),
		"product_code":   productCode,
	}
	return s.buildRequestParams("alipay.fund.auth.order.app.freeze", bizContent, notifyURL)
}

// FundTransCommonQuery 转账查询
func (s *AlipaySDK) FundTransCommonQuery(outBizNo string) (map[string]interface{}, error) {
	bizContent := map[string]interface{}{
		"out_biz_no": outBizNo,
	}
	return s.execute("alipay.fund.trans.common.query", bizContent)
}

// OauthToken 获取授权令牌
func (s *AlipaySDK) OauthToken(authCode string) (map[string]interface{}, error) {
	params := map[string]string{
		"app_id":     s.AppID,
		"method":     "alipay.system.oauth.token",
		"charset":    "utf-8",
		"sign_type":  "RSA2",
		"timestamp":  time.Now().Format("2006-01-02 15:04:05"),
		"version":    "1.0",
		"grant_type": "authorization_code",
		"code":       authCode,
	}

	sign, err := s.signParams(params)
	if err != nil {
		return nil, err
	}
	params["sign"] = sign

	return s.doPost(params)
}

// execute 执行API调用
func (s *AlipaySDK) execute(method string, bizContent map[string]interface{}) (map[string]interface{}, error) {
	bizContentJSON, err := json.Marshal(bizContent)
	if err != nil {
		return nil, fmt.Errorf("序列化biz_content失败: %v", err)
	}

	params := map[string]string{
		"app_id":      s.AppID,
		"method":      method,
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"biz_content": string(bizContentJSON),
	}

	sign, err := s.signParams(params)
	if err != nil {
		return nil, fmt.Errorf("签名失败: %v", err)
	}
	params["sign"] = sign

	result, err := s.doPost(params)
	if err != nil {
		return nil, err
	}

	// 提取实际响应（alipay_xxx_response）
	responseKey := strings.ReplaceAll(method, ".", "_") + "_response"
	if resp, ok := result[responseKey]; ok {
		if respMap, ok := resp.(map[string]interface{}); ok {
			return respMap, nil
		}
	}

	return result, nil
}

// buildRequestParams 构建请求参数（用于页面跳转类API）
func (s *AlipaySDK) buildRequestParams(method string, bizContent map[string]interface{}, notifyURL string) (string, error) {
	bizContentJSON, err := json.Marshal(bizContent)
	if err != nil {
		return "", fmt.Errorf("序列化biz_content失败: %v", err)
	}

	params := map[string]string{
		"app_id":      s.AppID,
		"method":      method,
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"biz_content": string(bizContentJSON),
	}
	if notifyURL != "" {
		params["notify_url"] = notifyURL
	}

	sign, err := s.signParams(params)
	if err != nil {
		return "", fmt.Errorf("签名失败: %v", err)
	}
	params["sign"] = sign

	// 编码为URL参数
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	return values.Encode(), nil
}

// signParams RSA2签名
func (s *AlipaySDK) signParams(params map[string]string) (string, error) {
	// 排序键
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构建待签名字符串
	var pairs []string
	for _, k := range keys {
		if params[k] != "" {
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, params[k]))
		}
	}
	signStr := strings.Join(pairs, "&")

	// RSA2 签名 (SHA256WithRSA)
	hash := crypto.SHA256
	h := hash.New()
	h.Write([]byte(signStr))
	hashed := h.Sum(nil)

	signature, err := rsa.SignPKCS1v15(rand.Reader, s.PrivateKey, hash, hashed)
	if err != nil {
		return "", fmt.Errorf("RSA签名失败: %v", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

// doPost 发送POST请求
func (s *AlipaySDK) doPost(params map[string]string) (map[string]interface{}, error) {
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(s.Gateway, values)
	if err != nil {
		return nil, fmt.Errorf("请求支付宝失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v, body: %s", err, string(body))
	}

	return result, nil
}

// VerifyNotify 验证支付宝异步通知签名
func (s *AlipaySDK) VerifyNotify(params map[string]string) bool {
	if s.PublicKey == nil {
		return false
	}

	sign := params["sign"]
	signType := params["sign_type"]
	if sign == "" {
		return false
	}

	// 复制map，避免修改调用方的原始数据
	filtered := make(map[string]string, len(params))
	for k, v := range params {
		if k != "sign" && k != "sign_type" {
			filtered[k] = v
		}
	}

	// 排序
	keys := make([]string, 0, len(filtered))
	for k := range filtered {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var pairs []string
	for _, k := range keys {
		if filtered[k] != "" {
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, filtered[k]))
		}
	}
	signStr := strings.Join(pairs, "&")
	_ = signType // signType已在过滤时排除，无需还原

	// 解码签名
	signBytes, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return false
	}

	// SHA256WithRSA验证
	hash := crypto.SHA256
	h := hash.New()
	h.Write([]byte(signStr))
	hashed := h.Sum(nil)

	return rsa.VerifyPKCS1v15(s.PublicKey, hash, hashed, signBytes) == nil
}

// ===== 直付通二级商户 API =====

// IndirectImageUpload 直付通图片上传
func (s *AlipaySDK) IndirectImageUpload(imageType, fileName string, content []byte) (map[string]interface{}, error) {
	bizContent := map[string]interface{}{
		"image_type":    imageType,
		"image_content": base64.StdEncoding.EncodeToString(content),
		"image_name":    fileName,
	}
	return s.execute("ant.merchant.expand.indirect.image.upload", bizContent)
}

// IndirectZftCreate 直付通进件创建
func (s *AlipaySDK) IndirectZftCreate(reqData map[string]interface{}) (map[string]interface{}, error) {
	return s.execute("ant.merchant.expand.indirect.zft.create", reqData)
}

// IndirectZftOrderQuery 直付通进件查询
func (s *AlipaySDK) IndirectZftOrderQuery(orderID, externalID string) (map[string]interface{}, error) {
	bizContent := map[string]interface{}{}
	if orderID != "" {
		bizContent["order_id"] = orderID
	}
	if externalID != "" {
		bizContent["external_id"] = externalID
	}
	return s.execute("ant.merchant.expand.indirect.zftorder.query", bizContent)
}

// ===== 密钥解析工具 =====

// cutKey 将连续密钥字符串按64字符分行
func cutKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, "\n", "")
	key = strings.ReplaceAll(key, "\r", "")
	key = strings.ReplaceAll(key, " ", "")

	var result strings.Builder
	for i := 0; i < len(key); i += 64 {
		end := i + 64
		if end > len(key) {
			end = len(key)
		}
		result.WriteString(key[i:end])
		result.WriteString("\n")
	}
	return result.String()
}

// parsePrivateKey 解析RSA私钥
func parsePrivateKey(keyStr string) (*rsa.PrivateKey, error) {
	keyStr = strings.TrimSpace(keyStr)

	// 如果不包含PEM头，添加之
	if !strings.Contains(keyStr, "-----BEGIN") {
		keyStr = fmt.Sprintf("-----BEGIN RSA PRIVATE KEY-----\n%s-----END RSA PRIVATE KEY-----", cutKey(keyStr))
	}

	block, _ := pem.Decode([]byte(keyStr))
	if block == nil {
		return nil, fmt.Errorf("PEM解码失败")
	}

	// 尝试PKCS1
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err == nil {
		return key, nil
	}

	// 尝试PKCS8
	pkcs8Key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败(PKCS1和PKCS8均失败)")
	}
	rsaKey, ok := pkcs8Key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("密钥不是RSA类型")
	}
	return rsaKey, nil
}

// parsePublicKey 解析RSA公钥
func parsePublicKey(keyStr string) (*rsa.PublicKey, error) {
	keyStr = strings.TrimSpace(keyStr)

	if !strings.Contains(keyStr, "-----BEGIN") {
		keyStr = fmt.Sprintf("-----BEGIN PUBLIC KEY-----\n%s-----END PUBLIC KEY-----", cutKey(keyStr))
	}

	block, _ := pem.Decode([]byte(keyStr))
	if block == nil {
		return nil, fmt.Errorf("PEM解码失败")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析公钥失败: %v", err)
	}

	rsaKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("公钥不是RSA类型")
	}
	return rsaKey, nil
}
