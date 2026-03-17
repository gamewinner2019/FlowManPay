package service

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/sign"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

// AuthService handles authentication-related logic
type AuthService struct {
	DB  *gorm.DB
	RDB *redis.Client
}

// NewAuthService creates a new AuthService
func NewAuthService(db *gorm.DB, rdb *redis.Client) *AuthService {
	return &AuthService{DB: db, RDB: rdb}
}

// LoginRequest represents the login request body
type LoginRequest struct {
	Username   string `json:"username" binding:"required"`
	Password   string `json:"password" binding:"required"`
	CaptchaKey string `json:"captchaKey"`
	Captcha    string `json:"captcha"`
	GoogleCode string `json:"googleCode"`
}

// LoginResponse represents the login response (matches Django LoginSerializer)
type LoginResponse struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
	UserID  uint   `json:"userId"`
	Name    string `json:"name"`
	RoleKey string `json:"role_key"`
	Avatar  string `json:"avatar"`
}

// Login performs user authentication with all checks.
// Mirrors Python's LoginSerializer.validate
func (s *AuthService) Login(req *LoginRequest) (*LoginResponse, error) {
	// 1. Find user by username
	var user model.Users
	if err := s.DB.Preload("Role").Where("username = ?", req.Username).First(&user).Error; err != nil {
		return nil, fmt.Errorf("账号/密码不正确")
	}

	// 2. Check user status
	if !user.IsActive {
		return nil, fmt.Errorf("账号已被删除")
	}
	if !user.Status {
		return nil, fmt.Errorf("账号已被禁用")
	}

	// 3. Check parent status for merchant/writeoff/tenant
	if err := s.checkParentStatus(&user); err != nil {
		return nil, err
	}

	// 4. Verify password: MD5(plaintext) -> bcrypt check
	md5Pwd := sign.MD5Password(req.Password)
	if !checkPassword(md5Pwd, user.Password) {
		// Also try direct password check
		if !checkPassword(req.Password, user.Password) {
			return nil, fmt.Errorf("账号/密码不正确")
		}
	}

	// 5. Google 2FA check
	if err := s.checkGoogle2FA(user.ID, req.GoogleCode); err != nil {
		return nil, err
	}

	// 6. Generate tokens
	accessToken, err := middleware.GenerateAccessToken(user.ID)
	if err != nil {
		return nil, fmt.Errorf("生成Token失败")
	}
	refreshToken, err := middleware.GenerateRefreshToken(user.ID)
	if err != nil {
		return nil, fmt.Errorf("生成Token失败")
	}

	// 7. Update last login and token
	now := time.Now()
	s.DB.Model(&user).Updates(map[string]interface{}{
		"last_login": now,
		"last_token": refreshToken,
	})

	// 8. Record login log (non-admin)
	if user.Role.Key != model.RoleKeyAdmin {
		s.DB.Create(&model.LoginLog{
			Username:       user.Username,
			LoginType:      1,
			Creator:        &user.ID,
			CreateDatetime: model.DateTime{Time: now},
		})
	}

	return &LoginResponse{
		Access:  accessToken,
		Refresh: refreshToken,
		UserID:  user.ID,
		Name:    user.Name,
		RoleKey: user.Role.Key,
		Avatar:  user.Avatar,
	}, nil
}

// RefreshTokenRequest represents the refresh token request
type RefreshTokenRequest struct {
	Refresh string `json:"refresh" binding:"required"`
}

// RefreshAccessToken generates a new access token from a refresh token
func (s *AuthService) RefreshAccessToken(refreshTokenStr string) (string, error) {
	claims, err := middleware.ParseToken(refreshTokenStr)
	if err != nil {
		return "", fmt.Errorf("刷新Token无效或已过期")
	}
	if claims.Type != middleware.RefreshToken {
		return "", fmt.Errorf("Token类型错误")
	}

	// Generate new access token
	accessToken, err := middleware.GenerateAccessToken(claims.UserID)
	if err != nil {
		return "", fmt.Errorf("生成Token失败")
	}
	return accessToken, nil
}

// checkParentStatus checks if the user's parent (tenant/merchant) is active.
func (s *AuthService) checkParentStatus(user *model.Users) error {
	switch user.Role.Key {
	case model.RoleKeyMerchant:
		var merchant model.Merchant
		if err := s.DB.Preload("Parent.SystemUser").Where("system_user_id = ?", user.ID).First(&merchant).Error; err == nil {
			if merchant.Parent != nil && !merchant.Parent.SystemUser.Status {
				return fmt.Errorf("上级租户已被禁用,请联系管理员")
			}
		}
	case model.RoleKeyWriteoff:
		var writeoff model.WriteOff
		if err := s.DB.Preload("Parent.SystemUser").Where("system_user_id = ?", user.ID).First(&writeoff).Error; err == nil {
			if writeoff.Parent != nil && !writeoff.Parent.SystemUser.Status {
				return fmt.Errorf("上级租户已被禁用,请联系管理员")
			}
		}
	}
	return nil
}

// checkGoogle2FA verifies Google Authenticator code if enabled.
func (s *AuthService) checkGoogle2FA(userID uint, code string) error {
	var googleAuth model.GoogleAuth
	if err := s.DB.Where("user_id = ? AND status = ?", userID, true).First(&googleAuth).Error; err != nil {
		// No Google Auth configured or not enabled
		return nil
	}

	if code == "" {
		return fmt.Errorf("请输入谷歌验证码")
	}

	valid := totp.Validate(code, googleAuth.Token)
	if !valid {
		return fmt.Errorf("谷歌验证码不正确")
	}
	return nil
}

// GenerateGoogleQR generates a Google Authenticator QR code for binding.
func (s *AuthService) GenerateGoogleQR(userID uint, username string) (string, string, error) {
	// Check if already bound
	var existingAuth model.GoogleAuth
	if err := s.DB.Where("user_id = ?", userID).First(&existingAuth).Error; err == nil {
		return "", "", fmt.Errorf("已经绑定过谷歌验证,需要先验证旧的或重置")
	}

	// Generate new TOTP key
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "XXPayPlus",
		AccountName: username,
	})
	if err != nil {
		return "", "", fmt.Errorf("生成密钥失败")
	}

	secret := key.Secret()

	// Generate QR code
	qrPNG, err := qrcode.Encode(key.URL(), qrcode.Medium, 256)
	if err != nil {
		return "", "", fmt.Errorf("生成二维码失败")
	}
	qrBase64 := base64.StdEncoding.EncodeToString(qrPNG)

	return qrBase64, secret, nil
}

// VerifyAndBindGoogle verifies the TOTP code and binds Google Auth.
func (s *AuthService) VerifyAndBindGoogle(userID uint, secret, code string) error {
	valid := totp.Validate(code, secret)
	if !valid {
		return fmt.Errorf("谷歌验证码不正确")
	}

	// Create or update GoogleAuth record
	result := s.DB.Where("user_id = ?", userID).Assign(model.GoogleAuth{
		UserID: userID,
		Token:  secret,
		Status: true,
	}).FirstOrCreate(&model.GoogleAuth{})

	if result.Error != nil {
		return fmt.Errorf("绑定失败")
	}
	return nil
}

// ResetGoogle deletes the Google Auth for a user.
func (s *AuthService) ResetGoogle(userID uint) error {
	return s.DB.Where("user_id = ?", userID).Delete(&model.GoogleAuth{}).Error
}

// CheckGoogle2FAExported is the exported version of checkGoogle2FA for use by handlers.
func (s *AuthService) CheckGoogle2FAExported(userID uint, code string) error {
	return s.checkGoogle2FA(userID, code)
}

// checkPassword verifies a password against a bcrypt hash.
func checkPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// CheckPasswordExported is the exported version of checkPassword for use by handlers.
func CheckPasswordExported(password, hash string) bool {
	return checkPassword(password, hash)
}

// HashPassword creates a bcrypt hash from a password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}
