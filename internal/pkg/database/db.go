package database

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/gamewinner2019/FlowManPay/internal/config"
	"github.com/gamewinner2019/FlowManPay/internal/model"
)

var db *gorm.DB

// Init initializes the database connection and runs auto-migration.
func Init() *gorm.DB {
	cfg := config.Get()

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.DBName,
		cfg.Database.Charset,
	)

	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("获取数据库实例失败: %v", err)
	}

	// 连接池配置
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 自动迁移
	autoMigrate()

	log.Println("数据库连接成功")
	return db
}

// Get returns the database instance.
func Get() *gorm.DB {
	return db
}

// autoMigrate runs auto migration for all models.
func autoMigrate() {
	// 先检查已有的关联表，手动创建不存在的关联表（跳过已存在的）
	migrateJoinTables()

	err := db.AutoMigrate(
		// 系统模型（Role 不放在这里迁移，单独处理以避免关联表冲突）
		&model.RoleNoJoin{},
		&model.Users{},
		&model.Menu{},
		&model.MenuButton{},
		&model.GoogleAuth{},
		&model.ApiWhiteList{},
		&model.LoginLog{},
		&model.SystemConfig{},
		&model.Dictionary{},
		&model.OperationLog{},
		// 代理模型
		&model.Tenant{},
		&model.Merchant{},
		&model.WriteOff{},
		&model.TenantTax{},
		&model.WriteoffTax{},
		&model.WriteoffBrokerage{},
		&model.MerchantPre{},
		&model.WriteoffPre{},
		&model.TenantCashFlow{},
		&model.WriteoffCashFlow{},
		&model.WriteoffBrokerageFlow{},
		// 支付模型
		&model.PayType{},
		&model.PayPlugin{},
		&model.PayPluginConfig{},
		&model.PayChannel{},
		&model.PayChannelTax{},
		&model.MerchantPayChannel{},
		&model.WriteoffPayChannel{},
		&model.PayDomain{},
		&model.RechargeHistory{},
		&model.ProductTax{},
		// 订单模型
		&model.Order{},
		&model.OrderDetail{},
		&model.OrderDeviceDetails{},
		&model.ReOrder{},
		&model.OrderLog{},
		&model.QueryLog{},
		// 通知模型
		&model.MerchantNotification{},
		&model.MerchantNotificationHistory{},
		&model.PhoneProductNotificationHistory{},
		&model.HouseProductNotificationHistory{},
		// 统计模型
		&model.TenantDayStatistics{},
		&model.MerchantDayStatistics{},
		&model.WriteOffDayStatistics{},
		&model.WriteOffChannelDayStatistics{},
		&model.PayChannelDayStatistics{},
		&model.DayStatistics{},
		// 封禁模型
		&model.BanUserId{},
		&model.BanIp{},
		// 支付宝原生管理模型
		&model.AlipayProduct{},
		&model.AlipayProductDayStatistics{},
		&model.AlipayWeight{},
		&model.AlipayShenma{},
		&model.AlipayShenmaDayStatistics{},
		&model.AlipayPublicPool{},
		&model.AlipayPublicPoolDayStatistics{},
		&model.AlipaySplitUserGroup{},
		&model.AlipaySplitUserGroupPre{},
		&model.AlipaySplitUserGroupPreHistory{},
		&model.AlipaySplitUserGroupAddMoney{},
		&model.AlipaySplitUser{},
		&model.AlipaySplitUserFlow{},
		&model.CollectionUser{},
		&model.CollectionDayFlow{},
		&model.AlipayTransferUserFlow{},
		&model.SplitHistory{},
		&model.AlipayProductTag{},
		&model.AlipayTransferUser{},
		&model.TransferHistory{},
		&model.AlipayComplain{},
		&model.TenantCookie{},
		&model.TenantCookieDayStatistics{},
		&model.TenantCookieFile{},
		// 业务模型
		&model.MerchantPreHistory{},
		&model.WriteoffPreHistory{},
		&model.TenantYufuUser{},
		&model.PhoneProduct{},
		&model.PhoneOrderFlow{},
		// 快速转账模型
		&model.AlipayQuickTransfer{},
		&model.QuickTransferHistory{},
		// 直付通二级商户模型
		&model.AlipaySubProduct{},
		&model.AlipaySubProductRequestHistory{},
	)
	if err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	log.Println("数据库迁移完成")
}

// migrateJoinTables 仅在关联表不存在时创建，已存在则跳过
func migrateJoinTables() {
	// dvadmin_system_role_permission
	if !db.Migrator().HasTable("dvadmin_system_role_permission") {
		_ = db.Exec(`CREATE TABLE dvadmin_system_role_permission (
			role_id bigint unsigned NOT NULL,
			menu_button_id bigint unsigned NOT NULL,
			PRIMARY KEY (role_id, menu_button_id)
		)`).Error
		log.Println("创建关联表 dvadmin_system_role_permission")
	}
	// dvadmin_system_role_menu
	if !db.Migrator().HasTable("dvadmin_system_role_menu") {
		_ = db.Exec(`CREATE TABLE dvadmin_system_role_menu (
			role_id bigint unsigned NOT NULL,
			menu_id bigint unsigned NOT NULL,
			PRIMARY KEY (role_id, menu_id)
		)`).Error
		log.Println("创建关联表 dvadmin_system_role_menu")
	}
}
