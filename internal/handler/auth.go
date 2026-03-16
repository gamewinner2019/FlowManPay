package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

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

	// 检查是否开启验证码
	var captchaConfig model.SystemConfig
	captchaEnabled := true
	if err := h.AuthService.DB.Where("`key` = ?", "base.captcha_state").First(&captchaConfig).Error; err == nil {
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

	// 验证码校验(如果开启)
	if req.CaptchaKey != "" {
		ctx := context.Background()
		cachedCode, err := h.RDB.Get(ctx, "captcha:"+req.CaptchaKey).Result()
		if err != nil || cachedCode != req.Captcha {
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
