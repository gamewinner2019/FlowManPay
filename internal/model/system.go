package model

import (
	"time"

	"gorm.io/gorm"
)

// Role maps to dvadmin_system_role
type Role struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Name           string         `gorm:"size:64;uniqueIndex" json:"name"`
	Key            string         `gorm:"size:64;uniqueIndex" json:"key"`
	Sort           int            `gorm:"default:1" json:"sort"`
	Status         bool           `gorm:"default:true" json:"status"`
	Admin          bool           `gorm:"default:false" json:"admin"`
	DataRange      int            `gorm:"default:0" json:"data_range"`
	Remark         string         `gorm:"size:255" json:"remark"`
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
	Permissions    []MenuButton   `gorm:"many2many:dvadmin_system_role_permission;" json:"permissions,omitempty"`
	Menus          []Menu         `gorm:"many2many:dvadmin_system_role_menu;" json:"menus,omitempty"`
}

func (Role) TableName() string {
	return TablePrefix + "system_role"
}

// RoleNoJoin 与 Role 相同但不含 many2many 关联，用于 AutoMigrate 避免关联表主键冲突
type RoleNoJoin struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Name           string         `gorm:"size:64;uniqueIndex" json:"name"`
	Key            string         `gorm:"size:64;uniqueIndex" json:"key"`
	Sort           int            `gorm:"default:1" json:"sort"`
	Status         bool           `gorm:"default:true" json:"status"`
	Admin          bool           `gorm:"default:false" json:"admin"`
	DataRange      int            `gorm:"default:0" json:"data_range"`
	Remark         string         `gorm:"size:255" json:"remark"`
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (RoleNoJoin) TableName() string {
	return TablePrefix + "system_role"
}

// Users maps to dvadmin_system_users
type Users struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Password       string         `gorm:"size:128" json:"-"`
	LastLogin      *time.Time     `json:"last_login"`
	IsSuperuser    bool           `gorm:"default:false" json:"is_superuser"`
	Username       string         `gorm:"size:150;uniqueIndex" json:"username"`
	FirstName      string         `gorm:"size:150" json:"-"`
	LastName       string         `gorm:"size:150" json:"-"`
	Email          string         `gorm:"size:254" json:"email"`
	IsStaff        bool           `gorm:"default:false" json:"-"`
	IsActive       bool           `gorm:"default:true" json:"-"`
	DateJoined     time.Time      `gorm:"autoCreateTime" json:"-"`
	Name           string         `gorm:"size:40" json:"name"`
	Mobile         string         `gorm:"size:100" json:"mobile"`
	Avatar         string         `gorm:"size:255" json:"avatar"`
	Gender         Gender         `gorm:"default:2" json:"gender"`
	Status         bool           `gorm:"default:true" json:"status"`
	RoleID         uint           `gorm:"index" json:"role_id"`
	Role           Role           `gorm:"foreignKey:RoleID" json:"role"`
	Key            string         `gorm:"size:32;uniqueIndex" json:"-"`
	OpPwd          *string        `gorm:"size:255" json:"-"`
	LastToken      string         `gorm:"type:text" json:"-"`
	TelegramUser   *int64         `json:"telegram_user"`
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Users) TableName() string {
	return TablePrefix + "system_users"
}

// IsMerchant checks if the user is a merchant.
func (u *Users) IsMerchant() bool {
	return u.Role.Key == RoleKeyMerchant
}

// IsTenant checks if the user is a tenant.
func (u *Users) IsTenant() bool {
	return u.Role.Key == RoleKeyTenant
}

// IsWriteoff checks if the user is a writeoff.
func (u *Users) IsWriteoff() bool {
	return u.Role.Key == RoleKeyWriteoff
}

// IsAdmin checks if the user is an admin.
func (u *Users) IsAdmin() bool {
	return u.Role.Key == RoleKeyAdmin || u.IsSuperuser
}

// IsOperation checks if the user is an operation.
func (u *Users) IsOperation() bool {
	return u.Role.Key == RoleKeyOperation
}

// Menu maps to dvadmin_system_menu
type Menu struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	ParentID       *uint          `gorm:"index" json:"parent_id"`
	Name           string         `gorm:"size:64" json:"name"`
	Icon           string         `gorm:"size:64" json:"icon"`
	Sort           int            `gorm:"default:1" json:"sort"`
	IsLink         bool           `gorm:"default:false" json:"is_link"`
	IsCatalog      bool           `gorm:"default:false" json:"is_catalog"`
	WebPath        string         `gorm:"size:128" json:"web_path"`
	ComponentPath  string         `gorm:"size:128" json:"component_path"`
	ComponentName  string         `gorm:"size:50" json:"component_name"`
	Status         bool           `gorm:"default:true" json:"status"`
	CachePage      bool           `gorm:"default:false" json:"cache_page"`
	Visible        bool           `gorm:"default:true" json:"visible"`
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Menu) TableName() string {
	return TablePrefix + "system_menu"
}

// MenuButton maps to dvadmin_system_menu_button
type MenuButton struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	MenuID         uint           `gorm:"index" json:"menu_id"`
	Name           string         `gorm:"size:64" json:"name"`
	Value          string         `gorm:"size:64" json:"value"`
	API            string         `gorm:"size:200" json:"api"`
	Method         int            `gorm:"default:0" json:"method"` // 0=GET, 1=POST, 2=PUT, 3=DELETE, ...
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (MenuButton) TableName() string {
	return TablePrefix + "system_menu_button"
}

// MethodToString converts method int to HTTP method string.
func MethodToString(method int) string {
	switch method {
	case 0:
		return "GET"
	case 1:
		return "POST"
	case 2:
		return "PUT"
	case 3:
		return "DELETE"
	case 4:
		return "PATCH"
	default:
		return "GET"
	}
}

// GoogleAuth maps to dvadmin_google_auth
type GoogleAuth struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	UserID         uint      `gorm:"uniqueIndex" json:"user_id"`
	Token          string    `gorm:"size:64" json:"-"`
	Status         bool      `gorm:"default:true" json:"status"`
	CreateDatetime time.Time `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time `gorm:"autoUpdateTime" json:"update_datetime"`
}

func (GoogleAuth) TableName() string {
	return TablePrefix + "google_auth"
}

// ApiWhiteList maps to dvadmin_api_white_list
type ApiWhiteList struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	URL            string         `gorm:"size:200" json:"url"`
	Method         int            `gorm:"default:0" json:"method"`
	EnableDatasource bool         `gorm:"default:true" json:"enable_datasource"`
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ApiWhiteList) TableName() string {
	return TablePrefix + "system_api_white_list"
}

// LoginLog maps to dvadmin_system_login_log
type LoginLog struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Username       string    `gorm:"size:150" json:"username"`
	IP             string    `gorm:"size:50" json:"ip"`
	Agent          string    `gorm:"type:text" json:"agent"`
	Browser        string    `gorm:"size:200" json:"browser"`
	OS             string    `gorm:"size:200" json:"os"`
	LoginType      int       `gorm:"default:1" json:"login_type"` // 1=普通登录
	Creator        *uint     `json:"creator"`
	CreateDatetime time.Time `gorm:"autoCreateTime" json:"create_datetime"`
}

func (LoginLog) TableName() string {
	return TablePrefix + "system_login_log"
}

// SystemConfig maps to dvadmin_system_config
type SystemConfig struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	ParentID       *uint          `gorm:"index" json:"parent_id"`
	Title          string         `gorm:"size:50" json:"title"`
	Key            string         `gorm:"size:20;index" json:"key"`
	Value          *string        `gorm:"type:json" json:"value"`
	Sort           int            `gorm:"default:0" json:"sort"`
	Status         bool           `gorm:"default:true" json:"status"`
	DataOptions    *string        `gorm:"type:json" json:"data_options"`
	FormItemType   int            `gorm:"default:0" json:"form_item_type"`
	Rule           *string        `gorm:"type:json" json:"rule"`
	Placeholder    string         `gorm:"size:50" json:"placeholder"`
	Setting        *string        `gorm:"type:json" json:"setting"`
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (SystemConfig) TableName() string {
	return TablePrefix + "system_config"
}

// Dictionary maps to dvadmin_system_dictionary
type Dictionary struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Label          string         `gorm:"size:100" json:"label"`
	Value          string         `gorm:"size:200" json:"value"`
	ParentID       *uint          `gorm:"index" json:"parent_id"`
	Sort           int            `gorm:"default:0" json:"sort"`
	Status         bool           `gorm:"default:true" json:"status"`
	Color          string         `gorm:"size:20" json:"color"`
	IsDefault      bool           `gorm:"default:false" json:"is_default"`
	Creator        *uint          `json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime time.Time      `gorm:"autoCreateTime" json:"create_datetime"`
	UpdateDatetime time.Time      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Dictionary) TableName() string {
	return TablePrefix + "system_dictionary"
}

// OperationLog maps to dvadmin_system_operation_log
type OperationLog struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	RequestModular string    `gorm:"size:64" json:"request_modular"`
	RequestPath    string    `gorm:"size:400" json:"request_path"`
	RequestBody    string    `gorm:"type:text" json:"request_body"`
	RequestMethod  string    `gorm:"size:8" json:"request_method"`
	RequestMsg     string    `gorm:"type:text" json:"request_msg"`
	RequestIP      string    `gorm:"size:50" json:"request_ip"`
	RequestBrowser string    `gorm:"size:200" json:"request_browser"`
	RequestOS      string    `gorm:"size:200" json:"request_os"`
	ResponseCode   string    `gorm:"size:32" json:"response_code"`
	JSONResult     string    `gorm:"type:text" json:"json_result"`
	Creator        *uint     `json:"creator"`
	CreateDatetime time.Time `gorm:"autoCreateTime" json:"create_datetime"`
}

func (OperationLog) TableName() string {
	return TablePrefix + "system_operation_log"
}
