package handler

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
	"github.com/gamewinner2019/FlowManPay/internal/plugin"
)

// CashierHandler 收银台模板处理器
type CashierHandler struct {
	DB        *gorm.DB
	RDB       *redis.Client
	templates *template.Template
}

// NewCashierHandler 创建收银台处理器
func NewCashierHandler(db *gorm.DB, rdb *redis.Client, templateDir string) *CashierHandler {
	tmpl, err := template.ParseGlob(templateDir + "/*.html")
	if err != nil {
		log.Fatalf("加载收银台模板失败: %v", err)
	}
	return &CashierHandler{
		DB:        db,
		RDB:       rdb,
		templates: tmpl,
	}
}

// templateData 模板渲染数据
type templateData struct {
	OrderNo   string
	Title     string
	Money     string
	URL       string
	Error     string
	CheckURL  string
	StartURL  string
	AuthKey   string
	SDK       string
	UID       string
	Remark    string
	TradeNo   string
	Name      string
	FirstName string
	Tm        int64
}

// renderTemplate 渲染模板
func (h *CashierHandler) renderTemplate(c *gin.Context, name string, data interface{}) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(c.Writer, name, data); err != nil {
		log.Printf("渲染模板 %s 失败: %v", name, err)
		c.String(http.StatusInternalServerError, "页面渲染失败")
	}
}

// getClientIP 获取客户端IP
func (h *CashierHandler) getClientIP(c *gin.Context) string {
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

// getDeviceType 根据 User-Agent 判断设备类型
func (h *CashierHandler) getDeviceType(c *gin.Context) int {
	ua := c.GetHeader("User-Agent")
	if strings.Contains(ua, "Android") {
		return 1 // Android
	}
	if strings.Contains(ua, "Mac OS") {
		return 2 // iOS
	}
	return 4 // PC
}

// getAuthURL 添加认证参数到URL
func (h *CashierHandler) getAuthURL(rawURL string, authKey string) string {
	if authKey == "" {
		return rawURL
	}
	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
	}
	return rawURL + separator + "auth_key=" + authKey
}

// getAuthKey 生成 auth_key 参数
func (h *CashierHandler) getAuthKey(rawURL string, authKey string) string {
	if authKey == "" {
		return ""
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce1 := fmt.Sprintf("%04d", time.Now().UnixNano()%10000)
	nonce2 := fmt.Sprintf("%04d", (time.Now().UnixNano()/10000)%10000)
	raw := fmt.Sprintf("%s-%s-%s-%s-%s", rawURL, ts, nonce1, nonce2, authKey)
	hash := fmt.Sprintf("%x", md5.Sum([]byte(raw)))
	return fmt.Sprintf("%s-%s-%s-%s", ts, nonce1, nonce2, hash)
}

// getPluginSubject 获取插件标题
func (h *CashierHandler) getPluginSubject(pluginID *uint, money int, orderNo string) string {
	if pluginID == nil {
		return "充值"
	}
	var pluginConfig model.PayPlugin
	if err := h.DB.First(&pluginConfig, *pluginID).Error; err != nil {
		return "充值"
	}
	return "充值"
}

// ========== templates_new 核心收银台渲染 ==========

// TemplatesNew 核心收银台模板渲染逻辑
// 对应 Django 的 templates_new 方法
func (h *CashierHandler) TemplatesNew(c *gin.Context, templateName string, extraSDKKey string) {
	orderNo := c.Param("order_no")
	moneyStr := c.Param("money")

	money, _ := strconv.Atoi(moneyStr)
	moneyFloat := float64(money) / 100.0

	data := templateData{
		OrderNo: orderNo,
		Title:   "充值",
		Money:   fmt.Sprintf("%.2f", moneyFloat),
		URL:     "",
		Error:   "",
	}

	ip := h.getClientIP(c)
	device := h.getDeviceType(c)

	if orderNo == "" {
		data.Error = "订单号错误"
		h.renderTemplate(c, templateName, data)
		return
	}

	// 查询订单
	var order model.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		data.Error = "订单不存在"
		h.renderTemplate(c, templateName, data)
		return
	}

	if order.OrderStatus != 0 && order.OrderStatus != 2 {
		data.Error = "订单非支付状态"
		h.renderTemplate(c, templateName, data)
		return
	}

	ctx := c.Request.Context()

	// IP 封禁检查
	if ip != "" && ip != "unknown" {
		// 加载订单详情
		var detail model.OrderDetail
		if err := h.DB.Where("order_id = ?", order.ID).First(&detail).Error; err == nil {
			if detail.WriteoffID != nil {
				var writeoff model.WriteOff
				if err := h.DB.First(&writeoff, *detail.WriteoffID).Error; err == nil {
						var banCount int64
						h.DB.Model(&model.BanIp{}).Where(
						"(tenant_id IS NULL OR tenant_id = ?) AND ip_address = ?",
						writeoff.ParentID, ip,
					).Count(&banCount)
					if banCount > 0 {
						c.Redirect(http.StatusFound, "https://www.baidu.com")
						return
					}
				}
			}
		}
	}

	// 检查缓存中的超时和创建时间
	outTime := h.RDB.Get(ctx, fmt.Sprintf("%s-timeout", orderNo)).Val()
	createTime := h.RDB.Get(ctx, fmt.Sprintf("%s-create-time", orderNo)).Val()
	canPayTime := h.RDB.Get(ctx, fmt.Sprintf("%s-pay-time", orderNo)).Val()

	if canPayTime == "" {
		data.Error = "已超过订单支付时间"
		h.renderTemplate(c, templateName, data)
		return
	}
	if outTime == "" || createTime == "" {
		data.Error = "订单已关闭"
		h.renderTemplate(c, templateName, data)
		return
	}

	// 检查设备是否已记录
	var devCount int64
	h.DB.Model(&model.OrderDeviceDetails{}).Where("order_id = ?", order.ID).Count(&devCount)
	if devCount == 0 {
		// 检查设备类型兼容
		var detail model.OrderDetail
		if err := h.DB.Where("order_id = ?", order.ID).Preload("Plugin").First(&detail).Error; err == nil && detail.Plugin != nil {
			supportDevice := detail.Plugin.SupportDevice
			deviceSupported := false
			switch device {
			case 1: // Android
				deviceSupported = supportDevice == 0 || supportDevice == 1 || supportDevice == 3 || supportDevice == 5 || supportDevice == 7
			case 2: // iOS
				deviceSupported = supportDevice == 0 || supportDevice == 2 || supportDevice == 3 || supportDevice == 6 || supportDevice == 7
			case 4: // PC
				deviceSupported = supportDevice == 0 || supportDevice == 4 || supportDevice == 5 || supportDevice == 6 || supportDevice == 7
			}

			if deviceSupported {
				// 创建设备记录
				deviceDetail := model.OrderDeviceDetails{
					DeviceType: model.DeviceType(device),
					IPAddress:  ip,
					OrderID:    order.ID,
				}
				h.DB.Create(&deviceDetail)
			} else {
				data.Error = "设备类型不支持"
			}
		}
	}

	// 如果没有错误，获取支付链接
	if data.Error == "" {
		// 获取创建参数
		createArgsStr := h.RDB.Get(ctx, fmt.Sprintf("%s-create", orderNo)).Val()
		if createArgsStr != "" {
			var createArgs map[string]interface{}
			if err := json.Unmarshal([]byte(createArgsStr), &createArgs); err == nil {
				if m, ok := createArgs["money"]; ok {
					if mf, ok := m.(float64); ok {
						data.Money = fmt.Sprintf("%.2f", mf/100.0)
					}
				}
				if pid, ok := createArgs["plugin_id"]; ok {
					if pidF, ok := pid.(float64); ok {
						pidU := uint(pidF)
						data.Title = h.getPluginSubject(&pidU, money, orderNo)
					}
				}
			}
		}

		// 尝试从缓存获取结果
		resStr := h.RDB.Get(ctx, fmt.Sprintf("%s-res", orderNo)).Val()
		var res map[string]interface{}
		needCreate := true
		if resStr != "" {
			if err := json.Unmarshal([]byte(resStr), &res); err == nil {
				if code, ok := res["code"].(float64); ok && code == 0 {
					needCreate = false
				}
			}
		}

		if needCreate {
			// 调用插件创建订单
			res = plugin.PluginCreateOrder(h.DB, h.RDB, orderNo, orderNo, ip, "")
		}

		if res != nil {
			if code, ok := res["code"].(float64); ok {
				if code == 0 {
					// 成功，提取支付URL
					if dataMap, ok := res["data"].(map[string]interface{}); ok {
						if payURL, ok := dataMap["pay_url"].(string); ok {
							data.URL = payURL
						}
					}

					// 获取 domain 构建 check_url
					var detail model.OrderDetail
					if err := h.DB.Where("order_id = ?", order.ID).First(&detail).Error; err == nil && detail.DomainID != nil {
						var domain model.PayDomain
						if err := h.DB.First(&domain, *detail.DomainID).Error; err == nil {
							checkPath := fmt.Sprintf("/api/pay/order/%s/check/", orderNo)
							data.CheckURL = domain.URL + h.getAuthURL(checkPath, domain.AuthKey)
						}
					}

					// 设置查询标记
					changeZ := h.RDB.Get(ctx, fmt.Sprintf("%s-change_z", orderNo)).Val()
					if changeZ == "" {
						plugin.PluginQueryOrder(h.DB, h.RDB, orderNo)
						h.RDB.Set(ctx, fmt.Sprintf("%s-change_z", orderNo), "1", 300*time.Second)
					}
				} else if code == 400 {
					if msg, ok := res["msg"].(string); ok {
						data.Error = msg
					}
				}
			}
		}
	}

	// 如果有额外SDK数据
	if extraSDKKey != "" {
		extraData := h.RDB.Get(ctx, extraSDKKey).Val()
		if extraData != "" {
			var extra map[string]interface{}
			if err := json.Unmarshal([]byte(extraData), &extra); err == nil {
				if uid, ok := extra["uid"].(string); ok {
					data.UID = uid
				}
				if remark, ok := extra["remark"].(string); ok {
					data.Remark = remark
				}
			}
		}
	}

	h.renderTemplate(c, templateName, data)
}

// ========== 各模板的快捷方法 ==========

// AlipayNew 支付宝标准收银台
// GET /view/:order_no/:money/alipay/
func (h *CashierHandler) AlipayNew(c *gin.Context) {
	h.TemplatesNew(c, "alipay.html", "")
}

// AlipayCopy 支付宝复制收银台
// GET /view/:order_no/:money/alipay/copy/
func (h *CashierHandler) AlipayCopy(c *gin.Context) {
	h.TemplatesNew(c, "alipay_copy.html", "")
}

// AlipayTs 支付宝TS收银台
// GET /view/:order_no/:money/alipay/ts/
func (h *CashierHandler) AlipayTs(c *gin.Context) {
	h.TemplatesNew(c, "alipay_ts.html", "")
}

// WechatNew 微信收银台
// GET /view/:order_no/:money/wechat/
func (h *CashierHandler) WechatNew(c *gin.Context) {
	h.TemplatesNew(c, "wechat.html", "")
}

// AlipayHgNew 支付宝黄金收银台
// GET /api/view/hg/:order_no/:money/alipay/
func (h *CashierHandler) AlipayHgNew(c *gin.Context) {
	h.TemplatesNew(c, "alipay_hg.html", "")
}

// AlipayUID 支付宝UID收银台
// GET /view/:order_no/:money/alipay/uid/
func (h *CashierHandler) AlipayUID(c *gin.Context) {
	orderNo := c.Param("order_no")
	moneyStr := c.Param("money")

	money, _ := strconv.Atoi(moneyStr)
	moneyFloat := float64(money) / 100.0

	ctx := c.Request.Context()
	extraSDK := fmt.Sprintf("%s_card_uid_sdk", orderNo)
	sdkStr := h.RDB.Get(ctx, extraSDK).Val()

	data := templateData{
		OrderNo: orderNo,
		Money:   fmt.Sprintf("%.2f", moneyFloat),
		Tm:      time.Now().UnixMilli() + 600000, // 10分钟倒计时(毫秒)
	}

	if sdkStr != "" {
		var sdkData map[string]interface{}
		if err := json.Unmarshal([]byte(sdkStr), &sdkData); err == nil {
			if name, ok := sdkData["name"].(string); ok {
				data.Name = name
				if len(name) > 0 {
					data.FirstName = string([]rune(name)[:1])
				}
			}
			if uid, ok := sdkData["uid"].(string); ok {
				data.UID = uid
			}
			if remark, ok := sdkData["remark"].(string); ok {
				data.Remark = remark
			}
			if tm, ok := sdkData["tm"].(float64); ok {
				data.Tm = int64(tm)
			}
		}
	}

	h.renderTemplate(c, "alipay_uid.html", data)
}

// AlipayQr 支付宝二维码收银台
// GET /view/:order_no/:money/alipay/qr/
func (h *CashierHandler) AlipayQr(c *gin.Context) {
	h.TemplatesNew(c, "alipay_qr.html", "")
}

// AlipayWithQr 支付宝APP二维码收银台
// GET /view/:order_no/:money/alipay/wqr/
func (h *CashierHandler) AlipayWithQr(c *gin.Context) {
	h.TemplatesNew(c, "alipay_app_qr.html", "")
}

// ========== AlipayApp SDK渲染 ==========

// AlipayApp 支付宝APP支付页面
// GET /api/alipay/app/:order_no/
func (h *CashierHandler) AlipayApp(c *gin.Context) {
	orderNo := c.Param("order_no")
	ctx := c.Request.Context()

	sdk := h.RDB.Get(ctx, fmt.Sprintf("%s_app_sdk", orderNo)).Val()
	if sdk == "" {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	// html/template 会自动对 <script> 内的内容做 JS 上下文转义，无需手动处理引号
	h.renderTemplate(c, "alipay_app.html", templateData{SDK: sdk})
}

// ========== AlipayHg 黄金UID页面 ==========

// AlipayHg 支付宝黄金UID
// GET /api/alipay/gold/hg/:order_no/
func (h *CashierHandler) AlipayHg(c *gin.Context) {
	orderNo := c.Param("order_no")
	ctx := c.Request.Context()

	sdkStr := h.RDB.Get(ctx, fmt.Sprintf("%s_gold_hg", orderNo)).Val()
	if sdkStr == "" {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	// 记录查询日志
	h.DB.Create(&model.QueryLog{
		URL:        fmt.Sprintf("api/alipay/gold/hg/%s/", orderNo),
		ReqBody:    sdkStr,
		OrderNo:    orderNo,
		Method:     "POST",
		StatusCode: 200,
		ResBody:    "空",
	})

	// SDK数据包含 uid/money/remark
	var sdkData map[string]interface{}
	if err := json.Unmarshal([]byte(sdkStr), &sdkData); err != nil {
		response.ErrorResponse(c, "SDK数据解析失败")
		return
	}

	data := templateData{}
	if uid, ok := sdkData["uid"].(string); ok {
		data.UID = uid
	}
	if money, ok := sdkData["money"].(string); ok {
		data.Money = money
	}
	if remark, ok := sdkData["remark"].(string); ok {
		data.Remark = remark
	}

	h.renderTemplate(c, "cc.html", data)
}

// ========== YunshuPay 运输支付 ==========

// YunshuPay 运输支付页面
// GET /view/yunshu/:order_no/:trade_no/alipay/
func (h *CashierHandler) YunshuPay(c *gin.Context) {
	orderNo := c.Param("order_no")
	tradeNo := c.Param("trade_no")

	var order model.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	moneyFloat := float64(order.Money) / 100.0
	h.renderTemplate(c, "yunshu_pay.html", templateData{
		TradeNo: tradeNo,
		Money:   fmt.Sprintf("%.2f", moneyFloat),
	})
}

// ========== Loading 加载页面 ==========

// Loading 加载中页面
// GET /view/:order_no/loading/
func (h *CashierHandler) Loading(c *gin.Context) {
	orderNo := c.Param("order_no")
	ctx := c.Request.Context()

	sdkStr := h.RDB.Get(ctx, fmt.Sprintf("%s_loading", orderNo)).Val()
	if sdkStr == "" {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	var sdkData map[string]interface{}
	if err := json.Unmarshal([]byte(sdkStr), &sdkData); err != nil {
		response.ErrorResponse(c, "数据解析失败")
		return
	}

	data := templateData{}
	if startURL, ok := sdkData["start_url"].(string); ok {
		data.StartURL = startURL
	}

	h.renderTemplate(c, "loading.html", data)
}

// ========== OtherPay 通用其他支付收银台 ==========

// otherPay 通用其他支付收银台渲染
func (h *CashierHandler) otherPay(c *gin.Context, htmlName string) {
	orderNo := c.Param("order_no")
	moneyStr := c.Param("money")

	money, _ := strconv.Atoi(moneyStr)
	moneyFloat := float64(money) / 100.0

	// 先查询订单获取ID
	var order model.Order
	if err := h.DB.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		response.ErrorResponse(c, "订单不存在")
		return
	}

	// 获取订单详情中的域名
	var detail model.OrderDetail
	if err := h.DB.Where("order_id = ?", order.ID).Preload("Domain").First(&detail).Error; err != nil {
		response.ErrorResponse(c, "订单详情不存在")
		return
	}

	if detail.Domain == nil {
		response.ErrorResponse(c, "域名配置缺失")
		return
	}

	authKey := detail.Domain.AuthKey
	parsedURL, err := url.Parse(detail.Domain.URL)
	if err != nil {
		response.ErrorResponse(c, "域名解析失败")
		return
	}
	host := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	startURL := fmt.Sprintf("%s/api/pay/order/start/", host)
	checkPath := fmt.Sprintf("/api/pay/order/%s/check/", orderNo)
	checkURL := h.getAuthURL(fmt.Sprintf("%s%s", host, checkPath), authKey)

	data := templateData{
		OrderNo:  orderNo,
		Money:    fmt.Sprintf("%.2f", moneyFloat),
		StartURL: startURL,
		AuthKey:  h.getAuthKey(startURL, authKey),
		CheckURL: checkURL,
	}

	h.renderTemplate(c, htmlName, data)
}

// OtherAlipay 其他支付宝收银台
// GET /view/other/:order_no/:money/alipay/
func (h *CashierHandler) OtherAlipay(c *gin.Context) {
	h.otherPay(c, "other_alipay.html")
}

// OtherAlipayAuto 自动跳转支付宝收银台
// GET /view/other/:order_no/:money/alipay/auto/
func (h *CashierHandler) OtherAlipayAuto(c *gin.Context) {
	h.otherPay(c, "other_alipay_auto.html")
}

// OtherAlipayGold 黄金支付宝收银台
// GET /view/other/:order_no/:money/alipay/gold/
func (h *CashierHandler) OtherAlipayGold(c *gin.Context) {
	h.otherPay(c, "other_alipay_gold.html")
}

// OtherWechat 微信收银台
// GET /view/other/:order_no/:money/wechat/
func (h *CashierHandler) OtherWechat(c *gin.Context) {
	h.otherPay(c, "other_wechat.html")
}

// OtherWechat2 微信V2收银台
// GET /view/other/:order_no/:money/wechat/v2/
func (h *CashierHandler) OtherWechat2(c *gin.Context) {
	h.otherPay(c, "other_wechat2.html")
}

// OtherWechat2NoDevice 无指纹微信收银台
// GET /view/other/:order_no/:money/wechat/v3/
func (h *CashierHandler) OtherWechat2NoDevice(c *gin.Context) {
	h.otherPay(c, "other_wechat2_no_device.html")
}

// OtherWechatMix 混合微信收银台
// GET /view/other/:order_no/:money/wechat/mix/
func (h *CashierHandler) OtherWechatMix(c *gin.Context) {
	h.otherPay(c, "other_wechat_mix.html")
}

// OtherWechatQr 微信扫码收银台
// GET /view/other/:order_no/:money/wechat/qr/
func (h *CashierHandler) OtherWechatQr(c *gin.Context) {
	h.otherPay(c, "other_wechat_qr.html")
}

// OtherWechatQrAuto 微信自动扫码收银台
// GET /view/other/:order_no/:money/wechat/qr/v2
func (h *CashierHandler) OtherWechatQrAuto(c *gin.Context) {
	h.otherPay(c, "other_wechat_qr_auto.html")
}

// OtherPaypal PayPal收银台
// GET /view/other/:order_no/:money/paypal/
func (h *CashierHandler) OtherPaypal(c *gin.Context) {
	h.otherPay(c, "paypal.html")
}

// OtherTaobao 淘宝收银台
// GET /view/other/:order_no/:money/taobao/
func (h *CashierHandler) OtherTaobao(c *gin.Context) {
	h.otherPay(c, "taobao.html")
}

// OtherUnpay 未支付页面
// GET /view/other/:order_no/:money/unpay/
func (h *CashierHandler) OtherUnpay(c *gin.Context) {
	h.otherPay(c, "unpay.html")
}

// ========== MerchantPay 商户收款页面 ==========

// MerchantPayChannel 商户收款通道信息
type MerchantPayChannel struct {
	First       bool   `json:"first"`
	ChannelID   uint   `json:"channel_id"`
	ChannelName string `json:"channel_name"`
}

// merchantPayData 商户收款页面数据
type merchantPayData struct {
	MerchantName string
	MerchantID   string
	Channels     []MerchantPayChannel
}

// MerchantPay 商户收款页面
// GET /view/:merchant_id/MerchantPay/
func (h *CashierHandler) MerchantPay(c *gin.Context) {
	merchantID := c.Param("merchant_id")

	// 查询商户名称
	var user model.Users
	err := h.DB.Joins("JOIN "+model.TablePrefix+"system_merchant m ON m.system_user_id = "+model.TablePrefix+"system_users.id").
		Where("m.id = ?", merchantID).
		First(&user).Error
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	// 查询商户启用的支付通道
	type channelRow struct {
		PayChannelID   uint   `gorm:"column:pay_channel_id"`
		PayChannelName string `gorm:"column:pay_channel_name"`
	}
	var rows []channelRow
	h.DB.Table(model.MerchantPayChannel{}.TableName()+" mpc").
		Select("mpc.pay_channel_id, pc.name as pay_channel_name").
		Joins("JOIN "+model.PayChannel{}.TableName()+" pc ON pc.id = mpc.pay_channel_id").
		Where("mpc.merchant_id = ? AND mpc.status = ?", merchantID, true).
		Order("mpc.pay_channel_id").
		Find(&rows)

	channels := make([]MerchantPayChannel, len(rows))
	for i, row := range rows {
		channels[i] = MerchantPayChannel{
			First:       i == 0,
			ChannelID:   row.PayChannelID,
			ChannelName: row.PayChannelName,
		}
	}

	data := merchantPayData{
		MerchantName: user.Name,
		MerchantID:   merchantID,
		Channels:     channels,
	}

	h.renderTemplate(c, "MerchantPay.html", data)
}
