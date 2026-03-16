package main

import (
	"fmt"
	"log"

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
		api.POST("/pay/order/notify/:plugin_type/:product_id/", notifyHandler.AlipayNotify)
		api.GET("/pay/order/notify/test/", notifyHandler.NotifyTest)
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
		}

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
	}

	// 启动服务
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("服务启动在 %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
