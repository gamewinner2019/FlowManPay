package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/config"
	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
	"github.com/gamewinner2019/FlowManPay/internal/service"
)

// AuthHandler handles authentication-related endpoints
type AuthHandler struct {
	AuthService *service.AuthService
	RDB         *redis.Client
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(db *gorm.DB, rdb *redis.Client) *AuthHandler {
	return &AuthHandler{
		AuthService: service.NewAuthService(db, rdb),
		RDB:         rdb,
	}
}

// Captcha 获取验证码
// GET /api/captcha/
func (h *AuthHandler) Captcha(c *gin.Context) {
	data := gin.H{}

	// 检查是否开启验证码（通过父子关系查询：parent key="base", child key="captcha_state"）
	var captchaConfig model.SystemConfig
	captchaEnabled := true
	configTable := model.SystemConfig{}.TableName()
	if err := h.AuthService.DB.Table(configTable).
		Joins("JOIN "+configTable+" AS parent ON parent.id = "+configTable+".parent_id").
		Where("parent.`key` = ? AND "+configTable+".`key` = ?", "base", "captcha_state").
		First(&captchaConfig).Error; err == nil {
		if captchaConfig.Value != nil && (*captchaConfig.Value == "false" || *captchaConfig.Value == "0") {
			captchaEnabled = false
		}
	}

	if captchaEnabled {
		// 生成验证码图片
		driver := base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 80)
		captchaObj := base64Captcha.NewCaptcha(driver, base64Captcha.DefaultMemStore)
		id, b64s, _, err := captchaObj.Generate()
		if err != nil {
			response.ErrorResponse(c, "生成验证码失败")
			return
		}

		// 将验证码存入Redis, 5分钟过期
		ctx := context.Background()
		code := base64Captcha.DefaultMemStore.Get(id, false)
		captchaKey := fmt.Sprintf("captcha:%s", id)
		h.RDB.Set(ctx, captchaKey, code, 5*time.Minute)

		data = gin.H{
			"key":        id,
			"image_base": b64s,
		}
	}

	response.DetailResponse(c, data, "")
}

// Login handles user login
// POST /api/token/
func (h *AuthHandler) Login(c *gin.Context) {
	var req service.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	// 验证码校验(如果开启，通过父子关系查询)
	captchaRequired := true
	var captchaConfig2 model.SystemConfig
	configTable := model.SystemConfig{}.TableName()
	if err := h.AuthService.DB.Table(configTable).
		Joins("JOIN "+configTable+" AS parent ON parent.id = "+configTable+".parent_id").
		Where("parent.`key` = ? AND "+configTable+".`key` = ?", "base", "captcha_state").
		First(&captchaConfig2).Error; err == nil {
		if captchaConfig2.Value != nil && (*captchaConfig2.Value == "false" || *captchaConfig2.Value == "0") {
			captchaRequired = false
		}
	}
	if captchaRequired && req.CaptchaKey == "" {
		response.ErrorResponse(c, "请输入验证码")
		return
	}
	if captchaRequired && req.CaptchaKey != "" {
		ctx := context.Background()
		if req.Captcha == "" {
			response.ErrorResponse(c, "请输入验证码")
			return
		}
		cachedCode, err := h.RDB.Get(ctx, "captcha:"+req.CaptchaKey).Result()
		if err != nil || cachedCode == "" || cachedCode != req.Captcha {
			response.ErrorResponse(c, "验证码错误或已过期")
			return
		}
		h.RDB.Del(ctx, "captcha:"+req.CaptchaKey)
	}

	resp, err := h.AuthService.Login(&req)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	response.DetailResponse(c, resp, "登录成功")
}

// RefreshToken handles token refresh
// POST /api/token/refresh/
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req service.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	accessToken, err := h.AuthService.RefreshAccessToken(req.Refresh)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	response.DetailResponse(c, gin.H{"access": accessToken}, "刷新成功")
}

// Logout handles user logout
// POST /api/logout/
func (h *AuthHandler) Logout(c *gin.Context) {
	// 将当前token加入黑名单
	tokenRaw, exists := c.Get("token_raw")
	if exists {
		ctx := context.Background()
		h.RDB.Set(ctx, "blacklist:"+tokenRaw.(string), "1", 24*time.Hour)
	}
	response.DetailResponse(c, nil, "退出成功")
}

// GetUserInfo returns current user info
// GET /api/system/user/user_info/
func (h *AuthHandler) GetUserInfo(c *gin.Context) {
	user, exists := middleware.GetCurrentUser(c)
	if !exists {
		response.ErrorResponse(c, "未获取到用户信息", 4001)
		return
	}

	data := gin.H{
		"id":       user.ID,
		"username": user.Username,
		"name":     user.Name,
		"email":    user.Email,
		"mobile":   user.Mobile,
		"avatar":   user.Avatar,
		"gender":   user.Gender,
		"role": gin.H{
			"id":   user.Role.ID,
			"name": user.Role.Name,
			"key":  user.Role.Key,
		},
	}

	response.DetailResponse(c, data, "获取成功")
}

// GoogleBind generates a Google Authenticator QR code
// GET /api/system/user/google/bind/
func (h *AuthHandler) GoogleBind(c *gin.Context) {
	user, exists := middleware.GetCurrentUser(c)
	if !exists {
		response.ErrorResponse(c, "未获取到用户信息", 4001)
		return
	}

	qrBase64, secret, err := h.AuthService.GenerateGoogleQR(user.ID, user.Username)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	// 缓存secret, 600秒过期
	ctx := context.Background()
	h.RDB.Set(ctx, "google_secret:"+user.Username, secret, 600*time.Second)

	response.DetailResponse(c, gin.H{
		"qr":     qrBase64,
		"secret": secret,
	}, "获取成功")
}

// GoogleCheck verifies and binds Google Authenticator
// POST /api/system/user/google/check/
func (h *AuthHandler) GoogleCheck(c *gin.Context) {
	user, exists := middleware.GetCurrentUser(c)
	if !exists {
		response.ErrorResponse(c, "未获取到用户信息", 4001)
		return
	}

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "请输入验证码")
		return
	}

	// 从缓存获取secret
	ctx := context.Background()
	secret, err := h.RDB.Get(ctx, "google_secret:"+user.Username).Result()
	if err != nil {
		response.ErrorResponse(c, "二维码已过期,请重新获取")
		return
	}

	if err := h.AuthService.VerifyAndBindGoogle(user.ID, secret, req.Code); err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	// 删除缓存
	h.RDB.Del(ctx, "google_secret:"+user.Username)

	response.DetailResponse(c, nil, "绑定成功")
}

// InitSettings 获取初始化配置
// GET /api/init/settings/
func (h *AuthHandler) InitSettings(c *gin.Context) {
	data := gin.H{}

	// 查询parent key为 "base" 或 "login" 的父级配置
	var parents []model.SystemConfig
	h.AuthService.DB.Where("`key` IN ? AND parent_id IS NULL", []string{"base", "login"}).Find(&parents)

	parentIDs := make([]uint, 0, len(parents))
	parentKeyMap := make(map[uint]string, len(parents))
	for _, p := range parents {
		parentIDs = append(parentIDs, p.ID)
		parentKeyMap[p.ID] = p.Key
	}

	if len(parentIDs) > 0 {
		var children []model.SystemConfig
		h.AuthService.DB.Where("parent_id IN ?", parentIDs).Order("sort").Find(&children)

		for _, child := range children {
			if child.ParentID == nil {
				continue
			}
			parentKey := parentKeyMap[*child.ParentID]
			fullKey := parentKey + "." + child.Key

			var value interface{}
			if child.Value != nil {
				// 尝试解析JSON值
				if err := json.Unmarshal([]byte(*child.Value), &value); err != nil {
					value = *child.Value
				}
				// form_item_type == 7 时取第一个元素的url
				if child.FormItemType == 7 {
					if arr, ok := value.([]interface{}); ok && len(arr) > 0 {
						if m, ok := arr[0].(map[string]interface{}); ok {
							value = m["url"]
						}
					}
				}
				// form_item_type == 11 时只保留key/title/value字段
				if child.FormItemType == 11 {
					if arr, ok := value.([]interface{}); ok {
						newArr := make([]map[string]interface{}, 0, len(arr))
						for _, item := range arr {
							if m, ok := item.(map[string]interface{}); ok {
								newArr = append(newArr, map[string]interface{}{
									"key":   m["key"],
									"title": m["title"],
									"value": m["value"],
								})
							}
						}
						value = newArr
					}
				}
			}
			data[fullKey] = value
		}
	}

	// 支持key过滤
	if keyFilter := c.Query("key"); keyFilter != "" {
		filtered := gin.H{}
		keys := strings.Split(keyFilter, "|")
		for k, v := range data {
			for _, prefix := range keys {
				if prefix != "" && strings.HasPrefix(k, prefix) {
					filtered[k] = v
					break
				}
			}
		}
		data = filtered
	}

	response.DetailResponse(c, data, "")
}

// LoginNoCaptcha 无验证码登录接口
// POST /api/login/
func (h *AuthHandler) LoginNoCaptcha(c *gin.Context) {
	cfg := config.Get()
	if cfg == nil || !cfg.System.LoginNoCaptchaAuth {
		response.ErrorResponse(c, "该接口暂未开通!")
		return
	}

	var req service.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	resp, err := h.AuthService.Login(&req)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	response.DetailResponse(c, resp, "登录成功")
}
