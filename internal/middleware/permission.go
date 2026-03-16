package middleware

import (
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
)

// RoleLevel defines the hierarchy level of roles
type RoleLevel int

const (
	RoleLevelAdmin     RoleLevel = 0
	RoleLevelOperation RoleLevel = 1
	RoleLevelTenant    RoleLevel = 2
	RoleLevelWriteoff  RoleLevel = 3
	RoleLevelMerchant  RoleLevel = 4
)

// GetRoleLevel returns the hierarchy level for a role key
func GetRoleLevel(key string) RoleLevel {
	switch key {
	case model.RoleKeyAdmin:
		return RoleLevelAdmin
	case model.RoleKeyOperation:
		return RoleLevelOperation
	case model.RoleKeyTenant:
		return RoleLevelTenant
	case model.RoleKeyWriteoff:
		return RoleLevelWriteoff
	case model.RoleKeyMerchant:
		return RoleLevelMerchant
	default:
		return RoleLevelMerchant + 1 // lowest
	}
}

// RequireRole creates a middleware that checks if the user's role
// meets the minimum required level.
// Mirrors the LGPermission hierarchy:
//   Admin > Operation > Tenant > Writeoff/Merchant
func RequireRole(minLevel RoleLevel) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, exists := GetCurrentUser(c)
		if !exists {
			response.ErrorResponse(c, "未获取到用户信息", 4001)
			c.Abort()
			return
		}

		if user.IsSuperuser {
			c.Next()
			return
		}

		userLevel := GetRoleLevel(user.Role.Key)
		if userLevel > minLevel {
			response.ErrorResponse(c, "权限不足", 4003)
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAdmin requires admin role
func RequireAdmin() gin.HandlerFunc {
	return RequireRole(RoleLevelAdmin)
}

// RequireOperation requires operation or higher
func RequireOperation() gin.HandlerFunc {
	return RequireRole(RoleLevelOperation)
}

// RequireTenant requires tenant or higher
func RequireTenant() gin.HandlerFunc {
	return RequireRole(RoleLevelTenant)
}

// RequireWriteoff requires writeoff or higher
func RequireWriteoff() gin.HandlerFunc {
	return RequireRole(RoleLevelWriteoff)
}

// RequireAny requires at least merchant level (any authenticated user)
func RequireAny() gin.HandlerFunc {
	return RequireRole(RoleLevelMerchant)
}

// APIPermission implements RBAC-based API permission checking.
// Mirrors Python's APIPermission class.
// It checks the user's role permissions (MenuButton) against the current request path and method.
func APIPermission(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, exists := GetCurrentUser(c)
		if !exists {
			response.ErrorResponse(c, "未获取到用户信息", 4001)
			c.Abort()
			return
		}

		// Superuser and admin bypass permission check
		if user.IsSuperuser || user.Role.Key == model.RoleKeyAdmin {
			c.Next()
			return
		}

		requestPath := c.Request.URL.Path
		requestMethod := strings.ToUpper(c.Request.Method)

		// Check API whitelist first
		var whiteList []model.ApiWhiteList
		db.Find(&whiteList)
		for _, item := range whiteList {
			if item.URL != "" {
				matched, _ := regexp.MatchString(item.URL, requestPath)
				if matched && model.MethodToString(item.Method) == requestMethod {
					c.Next()
					return
				}
			}
		}

		// Get role permissions (MenuButton entries)
		var permissions []model.MenuButton
		db.Table(model.MenuButton{}.TableName()).
			Joins("JOIN "+model.TablePrefix+"system_role_permission rp ON rp.menubutton_id = "+model.MenuButton{}.TableName()+".id").
			Where("rp.role_id = ?", user.RoleID).
			Find(&permissions)

		// Check if any permission matches current request
		for _, perm := range permissions {
			if perm.API == "" {
				continue
			}
			matched, _ := regexp.MatchString(perm.API, requestPath)
			if matched && model.MethodToString(perm.Method) == requestMethod {
				c.Next()
				return
			}
		}

		response.ErrorResponse(c, "权限不足", 4003)
		c.Abort()
	}
}

// JobPermission checks for the fixed job authentication header.
// Mirrors Python's JobPermission class.
func JobPermission(authKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth != authKey {
			response.ErrorResponse(c, "接口未开放", 4003)
			c.Abort()
			return
		}
		c.Next()
	}
}

// BotPermission checks for the fixed bot authentication header.
func BotPermission(authKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth != authKey {
			response.ErrorResponse(c, "接口未开放", 4003)
			c.Abort()
			return
		}
		c.Next()
	}
}

// LoadUser is a middleware that loads the full user object from DB
// and stores it in the gin context. Must be used after JWTAuth.
func LoadUser(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			response.ErrorResponse(c, "未获取到用户信息", 4001)
			c.Abort()
			return
		}

		var user model.Users
		if err := db.Preload("Role").First(&user, userID).Error; err != nil {
			response.ErrorResponse(c, "用户不存在", 4001)
			c.Abort()
			return
		}

		if !user.IsActive || !user.Status {
			response.ErrorResponse(c, "用户已被禁用", 4001)
			c.Abort()
			return
		}

		c.Set("current_user", &user)
		c.Next()
	}
}

// GetCurrentUser retrieves the current user from the gin context.
func GetCurrentUser(c *gin.Context) (*model.Users, bool) {
	val, exists := c.Get("current_user")
	if !exists {
		return nil, false
	}
	user, ok := val.(*model.Users)
	return user, ok
}
