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
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
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
	err := db.AutoMigrate(
		// 系统模型
		&model.Role{},
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
	)
	if err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	log.Println("数据库迁移完成")
}
