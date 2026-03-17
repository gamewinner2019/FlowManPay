package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gamewinner2019/FlowManPay/internal/config"
	"github.com/gamewinner2019/FlowManPay/internal/handler"
	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/database"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/rds"
	"github.com/gamewinner2019/FlowManPay/internal/plugin"
	"github.com/gamewinner2019/FlowManPay/internal/service"
)

func main() {
	// 加载配置
	if _, err := config.Load("config.yaml"); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	cfg := config.Get()

	// 初始化数据库
	db := database.Init()

	// 初始化Redis
	rdb := rds.Init()

	// 初始化服务
	authService := service.NewAuthService(db, rdb)

	// 初始化Phase1 Handler
	authHandler := handler.NewAuthHandler(db, rdb)
	userHandler := handler.NewUserHandler(db)
	tenantHandler := handler.NewTenantHandler(db, authService)
	merchantHandler := handler.NewMerchantHandler(db, authService)
	writeoffHandler := handler.NewWriteOffHandler(db, authService)

	// 初始化支付插件
	plugin.InitAll()

	// 初始化Phase2 Handler
	orderHandler := handler.NewOrderHandler(db)
	notifyHandler := handler.NewNotifyHandler(db)
	payTypeHandler := handler.NewPayTypeHandler(db)
	payPluginHandler := handler.NewPayPluginHandler(db)
	payChannelHandler := handler.NewPayChannelHandler(db)
	payChannelTaxHandler := handler.NewPayChannelTaxHandler(db)
	payDomainHandler := handler.NewPayDomainHandler(db)
	merchantPayChannelHandler := handler.NewMerchantPayChannelHandler(db)
	banHandler := handler.NewBanHandler(db)
	rechargeHandler := handler.NewRechargeHandler(db)

	// 初始化Phase4 Handler
	dataAnalysisHandler := handler.NewDataAnalysisHandler(db)
	splitHandler := handler.NewSplitHandler(db)

	// 初始化原生管理-支付宝 Handler
	alipayNativeHandler := handler.NewAlipayNativeHandler(db)

	// 初始化业务模块 Handler
	businessHandler := handler.NewBusinessHandler(db, rdb)

	// 初始化 PayPluginConfig Handler
	payPluginConfigHandler := handler.NewPayPluginConfigHandler(db)

	// 初始化 支付宝子请求 Handler
	alipaySubRequestHandler := handler.NewAlipaySubRequestHandler(db, rdb)

	// 初始化收银台 Handler
	cashierHandler := handler.NewCashierHandler(db, rdb, "templates")

	// 初始化订单API Handler（收银台启动/检查/商户查询/微信回调）
	orderAPIHandler := handler.NewOrderAPIHandler(db, rdb)

	// 注册内置 Hook
	service.RegisterBuiltinHooks(db)

	// 启动定时任务
	jobsSvc := service.NewJobsService(db)
	go jobsSvc.Start()

	// 初始化 Jobs Handler
	jobsHandler := handler.NewJobsHandler(db, jobsSvc)

	// 创建Gin引擎
	r := gin.Default()

	// 全局中间件
	r.Use(gin.Recovery())

	// ===== 公开接口（无需认证） =====
	api := r.Group("/api")
	{
		// 初始化配置
		api.GET("/init/settings/", authHandler.InitSettings)
		// 验证码
		api.GET("/captcha/", authHandler.Captcha)
		// 登录
		api.POST("/token/", authHandler.Login)
		// 无验证码登录
		api.POST("/login/", authHandler.LoginNoCaptcha)
		// 刷新Token
		api.POST("/token/refresh/", authHandler.RefreshToken)

		// 支付下单接口（公开，商户通过签名认证）
		api.POST("/pay/create/", orderHandler.CreateOrder)

		// 支付回调通知接口（公开，由第三方支付平台回调）
		// 统一分发：alipay_ 前缀走 AlipayNotify，wechat_ 前缀走 WechatNotify
		api.POST("/pay/order/notify/:plugin_type/:product_id/", func(c *gin.Context) {
			pluginType := c.Param("plugin_type")
			if strings.HasPrefix(pluginType, "wechat_") {
				orderAPIHandler.WechatNotify(c)
			} else {
				notifyHandler.AlipayNotify(c)
			}
		})
		api.GET("/pay/order/notify/test/", notifyHandler.NotifyTest)

		// 收银台订单启动/检查（公开，通过Redis缓存认证）
		api.GET("/pay/order/start/", orderAPIHandler.StartOrder)
		api.POST("/pay/order/start/", orderAPIHandler.StartOrder)
		api.POST("/pay/order/:raw_order_no/check/", orderAPIHandler.CheckOrder)

		// 商户查询接口（公开，通过签名认证）
		api.GET("/pay/order/query_order/", orderAPIHandler.QueryOrder)
		api.POST("/pay/order/query_order/", orderAPIHandler.QueryOrder)

		// Telegram Bot 回调（公开，由外部 Telegram Bot 调用）
		api.POST("/business/tenant_yufu/bot/telegram/", businessHandler.TenantYufuBotTelegram)

		// 支付宝直付通进件回调通知（公开，由支付宝服务器回调）
		api.GET("/alipay/sub/request/indirect/notify/", alipaySubRequestHandler.GetIndirectNotify)

		// 支付宝SDK页面（公开）
		api.GET("/view/hg/:order_no/:money/alipay/", cashierHandler.AlipayHgNew)
		api.GET("/alipay/app/:order_no/", cashierHandler.AlipayApp)
		api.GET("/alipay/gold/hg/:order_no/", cashierHandler.AlipayHg)
	}

	// ===== 收银台模板路由（公开，无需认证） =====
	// 注意: Gin的radix树路由不允许同一层级同时存在参数节点和静态节点
	// 因此将 loading/yunshu/merchant 放在带静态前缀的独立路由中，
	// 避免与 /:order_no/:money/... 冲突
	view := r.Group("/view")
	{
		// 标准收银台（第二段均为参数 :money）
		view.GET("/:order_no/:money/alipay/", cashierHandler.AlipayNew)
		view.GET("/:order_no/:money/alipay/copy/", cashierHandler.AlipayCopy)
		view.GET("/:order_no/:money/alipay/ts/", cashierHandler.AlipayTs)
		view.GET("/:order_no/:money/wechat/", cashierHandler.WechatNew)
		view.GET("/:order_no/:money/alipay/uid/", cashierHandler.AlipayUID)
		view.GET("/:order_no/:money/alipay/qr/", cashierHandler.AlipayQr)
		view.GET("/:order_no/:money/alipay/wqr/", cashierHandler.AlipayWithQr)

		// Loading 页面（使用静态前缀 /loading/ 避免与 :money 冲突）
		view.GET("/loading/:order_no/", cashierHandler.Loading)

		// 运输支付（yunshu 是静态前缀，不冲突）
		view.GET("/yunshu/:order_no/:trade_no/alipay/", cashierHandler.YunshuPay)

		// 商户收款页面（使用静态前缀 /merchant/ 避免与 :order_no 冲突）
		view.GET("/merchant/:merchant_id/pay/", cashierHandler.MerchantPay)

		// Other 系列收银台（other 是静态前缀，不冲突）
		view.GET("/other/:order_no/:money/alipay/", cashierHandler.OtherAlipay)
		view.GET("/other/:order_no/:money/alipay/auto/", cashierHandler.OtherAlipayAuto)
		view.GET("/other/:order_no/:money/alipay/gold/", cashierHandler.OtherAlipayGold)
		view.GET("/other/:order_no/:money/wechat/", cashierHandler.OtherWechat)
		view.GET("/other/:order_no/:money/wechat/v2/", cashierHandler.OtherWechat2)
		view.GET("/other/:order_no/:money/wechat/v3/", cashierHandler.OtherWechat2NoDevice)
		view.GET("/other/:order_no/:money/wechat/mix/", cashierHandler.OtherWechatMix)
		view.GET("/other/:order_no/:money/wechat/qr/", cashierHandler.OtherWechatQr)
		view.GET("/other/:order_no/:money/wechat/qr/v2", cashierHandler.OtherWechatQrAuto)
		view.GET("/other/:order_no/:money/paypal/", cashierHandler.OtherPaypal)
		view.GET("/other/:order_no/:money/taobao/", cashierHandler.OtherTaobao)
		view.GET("/other/:order_no/:money/unpay/", cashierHandler.OtherUnpay)
	}

	// ===== 需要认证的接口 =====
	auth := api.Group("")
	auth.Use(middleware.JWTAuth(), middleware.LoadUser(db))
	{
		// 退出登录
		auth.POST("/logout/", authHandler.Logout)

		// ===== 系统管理 =====
		system := auth.Group("/system")
		{
				// 用户管理
			user := system.Group("/user")
			{
				user.GET("/", userHandler.List)
				user.PUT("/:id/", userHandler.Update)
				user.PUT("/change_password/", userHandler.ChangePassword)
				user.GET("/simple_list/", userHandler.SimpleList)
				user.GET("/user_info/", authHandler.GetUserInfo)

				// Google 2FA
				user.GET("/google/bind/", authHandler.GoogleBind)
				user.POST("/google/check/", authHandler.GoogleCheck)

				// 管理员专用操作
				adminUser := user.Group("")
				adminUser.Use(middleware.RequireAdmin())
				{
					adminUser.POST("/", userHandler.Create)
					adminUser.DELETE("/:id/", userHandler.Delete)
					adminUser.PUT("/:id/reset_password/", userHandler.ResetPassword)
					adminUser.POST("/:id/login_agent/", userHandler.LoginAgent)
				}
			}
		}

		// ===== 代理管理 =====
		agent := auth.Group("/agent")
		{
			// 租户管理
			tenant := agent.Group("/tenant")
			tenant.Use(middleware.RequireTenant())
			{
				tenant.GET("/", tenantHandler.List)
				tenant.GET("/:id/", tenantHandler.Retrieve)
				tenant.PUT("/:id/", tenantHandler.Update)
				tenant.POST("/:id/change_money/", tenantHandler.ChangeMoney)
				tenant.POST("/:id/reset_google/", tenantHandler.ResetGoogle)
				tenant.GET("/:id/cash_flow/", tenantHandler.CashFlowList)
				tenant.GET("/simple_list/", tenantHandler.SimpleList)
			}

			// 商户管理
			merchant := agent.Group("/merchant")
			merchant.Use(middleware.RequireTenant())
			{
				merchant.GET("/", merchantHandler.List)
				merchant.GET("/:id/", merchantHandler.Retrieve)
				merchant.PUT("/:id/", merchantHandler.Update)
				merchant.POST("/:id/reset_google/", merchantHandler.ResetGoogle)
				merchant.GET("/simple_list/", merchantHandler.SimpleList)
			}

			// 核销管理
			writeoff := agent.Group("/writeoff")
			writeoff.Use(middleware.RequireTenant())
			{
				writeoff.GET("/", writeoffHandler.List)
				writeoff.GET("/:id/", writeoffHandler.Retrieve)
				writeoff.PUT("/:id/", writeoffHandler.Update)
				writeoff.POST("/:id/change_money/", writeoffHandler.ChangeMoney)
				writeoff.POST("/:id/transfer/", writeoffHandler.Transfer)
				writeoff.POST("/:id/reset_google/", writeoffHandler.ResetGoogle)
				writeoff.GET("/:id/cash_flow/", writeoffHandler.CashFlowList)
				writeoff.GET("/simple_list/", writeoffHandler.SimpleList)
			}
		}

		// ===== 订单管理 =====
		order := auth.Group("/order")
		{
			order.GET("/", orderHandler.List)
			order.GET("/statistics/", orderHandler.Statistics)
			order.GET("/:id/", orderHandler.Retrieve)
			order.POST("/:id/close/", orderHandler.Close)
			order.POST("/:id/refund/", orderHandler.Refund)
			order.GET("/:id/logs/", orderHandler.QueryLogs)
			order.POST("/:id/notify/", orderHandler.Notify)
			order.POST("/:id/reorder/", orderAPIHandler.Reorder)
		}

		// 重试全部通知（需要认证）
		auth.POST("/pay/order/retry/notify/all/", orderAPIHandler.RetryNotifyAll)

		// ===== 支付配置管理 =====
		payment := auth.Group("/payment")
		{
			// 支付方式
			payType := payment.Group("/types")
			{
				payType.GET("/", payTypeHandler.List)
				payType.POST("/", payTypeHandler.Create)
				payType.GET("/:id/", payTypeHandler.Retrieve)
				payType.PUT("/:id/", payTypeHandler.Update)
				payType.DELETE("/:id/", payTypeHandler.Delete)
			}

			// 支付插件
			payPlugin := payment.Group("/plugins")
			{
				payPlugin.GET("/", payPluginHandler.List)
				payPlugin.POST("/", payPluginHandler.Create)
				payPlugin.GET("/:id/", payPluginHandler.Retrieve)
				payPlugin.PUT("/:id/", payPluginHandler.Update)
				payPlugin.DELETE("/:id/", payPluginHandler.Delete)
				payPlugin.GET("/:id/config/", payPluginHandler.ConfigList)
				payPlugin.POST("/:id/config/", payPluginHandler.ConfigUpdate)
			}

			// 支付通道
			payChannel := payment.Group("/channels")
			{
				payChannel.GET("/", payChannelHandler.List)
				payChannel.POST("/", payChannelHandler.Create)
				payChannel.GET("/:id/", payChannelHandler.Retrieve)
				payChannel.PUT("/:id/", payChannelHandler.Update)
				payChannel.DELETE("/:id/", payChannelHandler.Delete)
			}

			// 通道费率
			channelTax := payment.Group("/channel-taxes")
			{
				channelTax.GET("/", payChannelTaxHandler.List)
				channelTax.POST("/", payChannelTaxHandler.Create)
				channelTax.PUT("/:id/", payChannelTaxHandler.Update)
				channelTax.DELETE("/:id/", payChannelTaxHandler.Delete)
			}

			// 支付域名
			payDomain := payment.Group("/domains")
			{
				payDomain.GET("/", payDomainHandler.List)
				payDomain.POST("/", payDomainHandler.Create)
				payDomain.GET("/:id/", payDomainHandler.Retrieve)
				payDomain.PUT("/:id/", payDomainHandler.Update)
				payDomain.DELETE("/:id/", payDomainHandler.Delete)
			}
		}

		// ===== 商户通道管理 =====
		merchantChannel := auth.Group("/merchant/channels")
		{
			merchantChannel.GET("/", merchantPayChannelHandler.List)
			merchantChannel.POST("/", merchantPayChannelHandler.Create)
			merchantChannel.PUT("/:id/", merchantPayChannelHandler.Update)
			merchantChannel.DELETE("/:id/", merchantPayChannelHandler.Delete)
		}

		// ===== 封禁管理 =====
		ban := auth.Group("/ban")
		{
			// 封禁用户ID
			banUser := ban.Group("/users")
			{
				banUser.GET("/", banHandler.BanUserIDList)
				banUser.POST("/", banHandler.BanUserIDCreate)
				banUser.DELETE("/:id/", banHandler.BanUserIDDelete)
			}

			// 封禁IP
			banIP := ban.Group("/ips")
			{
				banIP.GET("/", banHandler.BanIPList)
				banIP.POST("/", banHandler.BanIPCreate)
				banIP.DELETE("/:id/", banHandler.BanIPDelete)
			}
		}

		// ===== 充值管理 =====
		recharge := auth.Group("/recharge")
		{
			recharge.GET("/", rechargeHandler.List)
			recharge.POST("/", rechargeHandler.Create)
		}

		// ===== 数据分析/统计 =====
		statistics := auth.Group("/statistics")
		{
			statistics.GET("/dashboard/", dataAnalysisHandler.Dashboard)
			statistics.GET("/day/", dataAnalysisHandler.DayStatisticsList)
			statistics.GET("/day/export/", dataAnalysisHandler.DayStatisticsExport)
			statistics.GET("/pay_channel/", dataAnalysisHandler.PayChannelStatsList)
			statistics.GET("/split_group/", dataAnalysisHandler.SplitGroupStatsList)
			statistics.GET("/collection/", dataAnalysisHandler.CollectionStatsList)
			statistics.GET("/order_success_rate/", dataAnalysisHandler.OrderSuccessRate)

			// 首页统计接口
			success := statistics.Group("/success")
			{
				success.GET("/today/", dataAnalysisHandler.SuccessToday)
				success.GET("/yesterday/", dataAnalysisHandler.SuccessYesterday)
				success.GET("/total/", dataAnalysisHandler.SuccessTotal)
			}
			statistics.GET("/device/", dataAnalysisHandler.DeviceType)
			statistics.GET("/tenant/balance/", dataAnalysisHandler.TenantBalance)
			statistics.GET("/profit/", dataAnalysisHandler.Profit)
			statistics.GET("/month/half/", dataAnalysisHandler.MonthHalf)
			statistics.GET("/members/", dataAnalysisHandler.Members)
			statistics.GET("/enum/", dataAnalysisHandler.Enum)
		}

		// ===== 分账管理 =====
		split := auth.Group("/split")
		{
			// 分账用户组
			groups := split.Group("/groups")
			{
				groups.GET("/", splitHandler.GroupList)
				groups.POST("/", splitHandler.GroupCreate)
				groups.GET("/:id/", splitHandler.GroupRetrieve)
				groups.PUT("/:id/", splitHandler.GroupUpdate)
				groups.DELETE("/:id/", splitHandler.GroupDelete)
				groups.POST("/:id/pre_pay/", splitHandler.GroupPrePay)
				groups.GET("/:id/pre_pay_history/", splitHandler.GroupPrePayHistory)
				groups.POST("/:id/add_money/", splitHandler.GroupAddMoney)
			}

			// 分账用户
			users := split.Group("/users")
			{
				users.GET("/", splitHandler.UserList)
				users.POST("/", splitHandler.UserCreate)
				users.PUT("/:id/", splitHandler.UserUpdate)
				users.DELETE("/:id/", splitHandler.UserDelete)
				users.GET("/:id/flow/", splitHandler.UserFlowList)
			}

			// 分账历史
			split.GET("/history/", splitHandler.SplitHistoryList)
		}

		// ===== 归集管理 =====
		collection := auth.Group("/collection")
		{
			collUsers := collection.Group("/users")
			{
				collUsers.GET("/", splitHandler.CollectionUserList)
				collUsers.POST("/", splitHandler.CollectionUserCreate)
				collUsers.PUT("/:id/", splitHandler.CollectionUserUpdate)
				collUsers.DELETE("/:id/", splitHandler.CollectionUserDelete)
				collUsers.GET("/:id/flow/", splitHandler.CollectionFlowList)
			}
		}

		// ===== 原生管理-支付宝 =====
		alipay := auth.Group("/alipay")
		{
			// 支付宝产品管理
			product := alipay.Group("/product")
			{
				product.GET("/", alipayNativeHandler.ProductList)
				product.POST("/", alipayNativeHandler.ProductCreate)
				product.GET("/simple/", alipayNativeHandler.ProductSimple)
				product.POST("/batch/", alipayNativeHandler.ProductBatch)
				product.GET("/tags/", alipayNativeHandler.ProductTags)
				product.POST("/tags/", alipayNativeHandler.ProductTagsAdd)
				product.DELETE("/tags/:name/", alipayNativeHandler.ProductTagsDelete)
				product.GET("/:id/", alipayNativeHandler.ProductRetrieve)
				product.PUT("/:id/", alipayNativeHandler.ProductUpdate)
				product.DELETE("/:id/", alipayNativeHandler.ProductDelete)
				product.GET("/:id/statistics/day/", alipayNativeHandler.ProductStatisticsDay)
				product.GET("/:id/statistics/channel/", alipayNativeHandler.ProductStatisticsChannel)
				product.GET("/:id/weight/", alipayNativeHandler.ProductWeightList)
				product.POST("/:id/weight/", alipayNativeHandler.ProductWeightSet)
				product.GET("/:id/pay_channel/", alipayNativeHandler.ProductPayChannelList)
				product.GET("/:id/transfer_user/", alipayNativeHandler.TransferUserList)
				product.POST("/:id/transfer_user/", alipayNativeHandler.TransferUserCreate)
				product.PUT("/:id/transfer_user/:uid/", alipayNativeHandler.TransferUserUpdate)
				product.DELETE("/:id/transfer_user/:uid/", alipayNativeHandler.TransferUserDelete)
			}

			// 转账历史
			transfer := alipay.Group("/transfer")
			{
				transfer.GET("/history/", alipayNativeHandler.TransferHistoryList)
				transfer.GET("/history/statistics/", alipayNativeHandler.TransferHistoryStatistics)
			}

			// 公池管理
			publicPool := alipay.Group("/public_pool")
			{
				publicPool.GET("/", alipayNativeHandler.PublicPoolList)
				publicPool.PUT("/:id/", alipayNativeHandler.PublicPoolUpdate)
				publicPool.DELETE("/:id/", alipayNativeHandler.PublicPoolDelete)
				publicPool.GET("/statistics/", alipayNativeHandler.PublicPoolStatistics)
			}

			// 投诉管理
			complain := alipay.Group("/complain")
			{
				complain.GET("/", alipayNativeHandler.ComplainList)
				complain.PUT("/:id/", alipayNativeHandler.ComplainUpdate)
			}

			// 分账历史（原生）
			splitNative := alipay.Group("/split")
			{
				splitNative.GET("/history/", alipayNativeHandler.SplitNativeHistoryList)
				splitNative.GET("/history/statistics/", alipayNativeHandler.SplitNativeHistoryStatistics)

				// 分账用户组
				splitGroup := splitNative.Group("/group")
				{
					splitGroup.GET("/", alipayNativeHandler.NativeSplitGroupList)
					splitGroup.POST("/", alipayNativeHandler.NativeSplitGroupCreate)
					splitGroup.GET("/statistics/", alipayNativeHandler.NativeSplitGroupStatistics)
					splitGroup.GET("/:id/", alipayNativeHandler.NativeSplitGroupRetrieve)
					splitGroup.PUT("/:id/", alipayNativeHandler.NativeSplitGroupUpdate)
					splitGroup.DELETE("/:id/", alipayNativeHandler.NativeSplitGroupDelete)
					splitGroup.POST("/:id/bind/add/", alipayNativeHandler.NativeSplitGroupBindAdd)
					splitGroup.POST("/:id/bind/remove/", alipayNativeHandler.NativeSplitGroupBindRemove)
				}

				// 分账用户
				splitUser := splitNative.Group("/user")
				{
					splitUser.GET("/", alipayNativeHandler.NativeSplitUserList)
					splitUser.POST("/", alipayNativeHandler.NativeSplitUserCreate)
					splitUser.PUT("/:id/", alipayNativeHandler.NativeSplitUserUpdate)
					splitUser.DELETE("/:id/", alipayNativeHandler.NativeSplitUserDelete)
				}
			}

			// 神码管理
			shenma := alipay.Group("/shenma")
			{
				shenma.GET("/", alipayNativeHandler.ShenmaList)
				shenma.POST("/", alipayNativeHandler.ShenmaCreate)
				shenma.PUT("/:id/", alipayNativeHandler.ShenmaUpdate)
				shenma.DELETE("/:id/", alipayNativeHandler.ShenmaDelete)
				shenma.GET("/:id/pay_channel/", alipayNativeHandler.ShenmaPayChannel)
			}
		}

		// ===== 业务模块 =====
		business := auth.Group("/business")
		{
			// 商户通知管理
			business.GET("/merchant_notification/", businessHandler.MerchantNotificationList)

			// 商户预付历史
			merchantPre := business.Group("/merchant_pre")
			{
				merchantPre.GET("/history/", businessHandler.MerchantPreHistoryList)
				merchantPre.GET("/history/statistics/", businessHandler.MerchantPreHistoryStatistics)
			}

			// 核销流水
			writeoffFlow := business.Group("/writeoff_flow")
			{
				writeoffFlow.GET("/", businessHandler.WriteoffCashFlowList)
				writeoffFlow.GET("/brokerage/", businessHandler.WriteoffBrokerageFlowList)
			}

			// 核销预付历史
			writeoffPre := business.Group("/writeoff_pre")
			{
				writeoffPre.GET("/history/", businessHandler.WriteoffPreHistoryList)
				writeoffPre.GET("/history/statistics/", businessHandler.WriteoffPreHistoryStatistics)
			}

			// 租户流水
			business.GET("/tenant_flow/", businessHandler.TenantCashFlowList)

			// 订单设备管理
			orderDevice := business.Group("/order_device")
			{
				orderDevice.GET("/", businessHandler.OrderDeviceList)
					orderDevice.POST("/ban_ip/:id/", businessHandler.OrderDeviceBanIP)
				orderDevice.POST("/ban_userid/:id/", businessHandler.OrderDeviceBanUserID)
				orderDevice.GET("/statistics/", businessHandler.OrderDeviceStatistics)
			}

			// 重新下单记录
			business.GET("/reorder/", businessHandler.ReOrderList)

			// 租户预付用户(Telegram)
			tenantYufu := business.Group("/tenant_yufu")
			{
				tenantYufu.GET("/", businessHandler.TenantYufuUserList)
				tenantYufu.POST("/", businessHandler.TenantYufuUserCreate)
					tenantYufu.DELETE("/:id/", businessHandler.TenantYufuUserDelete)
					// Note: /bot/telegram/ is registered as public route (no JWT) since it's called by external Telegram bots
			}

			// 归集用户日统计
			collectionStats := business.Group("/collection_statistics")
			{
				collectionStats.GET("/", businessHandler.CollectionUserDayStatisticsList)
				collectionStats.GET("/statistics/", businessHandler.CollectionUserDayStatisticsStatistics)
			}

			// 租户小号/Cookie管理
			tenantAccount := business.Group("/tenant_account")
			{
				// 文件管理
				file := tenantAccount.Group("/file")
				{
					file.GET("/", businessHandler.TenantCookieFileList)
					file.POST("/", businessHandler.TenantCookieFileCreate)
					file.PUT("/:id/", businessHandler.TenantCookieFileUpdate)
					file.DELETE("/:id/", businessHandler.TenantCookieFileDelete)
					file.GET("/:id/export/", businessHandler.TenantCookieFileExport)
				}
				// Cookie管理
				cookie := tenantAccount.Group("/cookie")
				{
					cookie.GET("/", businessHandler.TenantCookieList)
					cookie.PUT("/:id/", businessHandler.TenantCookieUpdate)
					cookie.DELETE("/:id/", businessHandler.TenantCookieDelete)
					cookie.GET("/args/", businessHandler.TenantCookieArgs)
					cookie.GET("/count/", businessHandler.TenantCookieCount)
				}
			}

			// 话单管理
			phoneOrder := business.Group("/phone_order")
			{
				phoneOrder.GET("/", businessHandler.PhoneOrderList)
				phoneOrder.DELETE("/:id/", businessHandler.PhoneOrderDelete)
				phoneOrder.GET("/statistics/money/", businessHandler.PhoneOrderStatisticsMoney)
				phoneOrder.GET("/product/", businessHandler.PhoneOrderProduct)
				phoneOrder.GET("/statistics/", businessHandler.PhoneOrderStatistics)
			}

			// 日统计管理
			dayStats := business.Group("/day_statistics")
			{
				dayStats.GET("/merchant/", businessHandler.MerchantDayStatisticsList)
				dayStats.GET("/writeoff/", businessHandler.WriteoffDayStatisticsList)
				dayStats.GET("/tenant/", businessHandler.TenantDayStatisticsList)
				dayStats.GET("/pay_channel/", businessHandler.PayChannelDayStatisticsList)
				dayStats.GET("/all/", businessHandler.AllDayStatisticsList)
			}
		}

		// ===== 定时任务手动触发 =====
		jobs := auth.Group("/jobs")
		jobs.Use(middleware.RequireAdmin())
		{
			jobs.POST("/check_no_split_history/", jobsHandler.CheckNoSplitHistory)
			jobs.POST("/delete_order/", jobsHandler.DeleteOrder)
			jobs.POST("/delete_log/", jobsHandler.DeleteLog)
			jobs.POST("/auto_transfer/", jobsHandler.AutoTransfer)
		}

		// ===== 插件配置管理 =====
		pluginConfig := auth.Group("/payment/plugin/config")
		{
			pluginConfig.GET("/", payPluginConfigHandler.List)
			pluginConfig.POST("/", payPluginConfigHandler.Create)
			pluginConfig.GET("/:id/", payPluginConfigHandler.Retrieve)
			pluginConfig.PUT("/:id/", payPluginConfigHandler.Update)
			pluginConfig.DELETE("/:id/", payPluginConfigHandler.Delete)
			pluginConfig.POST("/save_content/", payPluginConfigHandler.SaveContent)
			pluginConfig.GET("/get_relation_info/", payPluginConfigHandler.GetRelationInfo)
		}

		// ===== 支付宝直付通子请求 =====
		subRequest := auth.Group("/alipay/sub/request")
		{
			subRequest.GET("/", alipaySubRequestHandler.List)
			subRequest.GET("/:external_id/", alipaySubRequestHandler.Retrieve)
			subRequest.POST("/image/upload/", alipaySubRequestHandler.UploadImage)
			subRequest.GET("/indirect/id/", alipaySubRequestHandler.GetIndirectID)
			subRequest.POST("/indirect/:indirect_id/draft/", alipaySubRequestHandler.IndirectCreate)
			subRequest.POST("/indirect/:indirect_id/query/", alipaySubRequestHandler.IndirectQuery)
			subRequest.POST("/indirect/:indirect_id/request/", alipaySubRequestHandler.IndirectRequest)
			subRequest.POST("/indirect/:indirect_id/remote/", alipaySubRequestHandler.IndirectRemote)
		}

		// ===== 域名鉴权（nginx subrequest） =====
		auth.GET("/auth_responder/check/:domain/", businessHandler.AuthResponderCheckAuth)
	}

	// 启动服务
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("服务启动在 %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
