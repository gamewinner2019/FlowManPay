package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gamewinner2019/FlowManPay/internal/config"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/rds"
)

// TelegramService Telegram Bot 通知服务
type TelegramService struct {
	mu sync.RWMutex
}

var telegramInstance *TelegramService
var telegramOnce sync.Once

// GetTelegramService 获取 Telegram 服务单例
func GetTelegramService() *TelegramService {
	telegramOnce.Do(func() {
		telegramInstance = &TelegramService{}
	})
	return telegramInstance
}

// ForwardBot 转发消息给 Telegram Bot
func (s *TelegramService) ForwardBot(data map[string]interface{}) bool {
	cfg := config.Get()
	if cfg == nil || cfg.Telegram.BotHost == "" {
		log.Printf("[Telegram] Bot未配置")
		return false
	}

	host := cfg.Telegram.BotHost
	path := cfg.Telegram.ForwardsPath
	if path == "" {
		path = "/forwards"
	}
	url := host + path

	body, err := json.Marshal(data)
	if err != nil {
		log.Printf("[Telegram] JSON序列化失败: %v", err)
		return false
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("[Telegram] 请求失败: %v", err)
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[Telegram] 解析响应失败: %v", err)
		return false
	}

	return result.Code == 0
}

// ForwardMessage 发送文本消息
func (s *TelegramService) ForwardMessage(chatID string, message string, isHTML bool) bool {
	if chatID == "" {
		return false
	}
	data := map[string]interface{}{
		"forwards": message,
		"chat_id":  chatID,
		"is_html":  isHTML,
	}
	for i := 0; i < 3; i++ {
		if s.ForwardBot(data) {
			return true
		}
	}
	log.Printf("[Telegram] 消息发送失败")
	return false
}

// ForwardMarkdown 发送 Markdown 消息
func (s *TelegramService) ForwardMarkdown(chatID string, message string) bool {
	if chatID == "" {
		return false
	}
	data := map[string]interface{}{
		"forwards": message,
		"chat_id":  chatID,
		"is_html":  false,
		"is_md":    true,
	}
	for i := 0; i < 3; i++ {
		if s.ForwardBot(data) {
			return true
		}
	}
	log.Printf("[Telegram] Markdown消息发送失败")
	return false
}

// AlipayOfflineForward 支付宝账号掉线通知（带缓存去重）
func (s *TelegramService) AlipayOfflineForward(name string, failCount int, telegram string) {
	if telegram == "" {
		return
	}
	cacheKey := fmt.Sprintf("alipay_offline_forward%s", name)
	ctx := context.Background()
	rdb := rds.Get()
	if rdb != nil {
		val, _ := rdb.Get(ctx, cacheKey).Result()
		if val != "" {
			return
		}
	}

	msg := fmt.Sprintf("信息通知：\n主体名称：%s\n连续%d次未支付系统已自动关闭该主体,请手动检查是否受限", name, failCount)
	data := map[string]interface{}{
		"forwards": msg,
		"chat_id":  telegram,
		"is_html":  false,
	}
	for i := 0; i < 3; i++ {
		if s.ForwardBot(data) {
			if rdb != nil {
				rdb.Set(ctx, cacheKey, "1", 180*time.Second)
			}
			return
		}
	}
	log.Printf("[Telegram] alipay_offline_forward 通知失败")
}

// AlipayUserOfflineForward 支付宝归集账号掉线通知（带缓存去重）
func (s *TelegramService) AlipayUserOfflineForward(productName string, message string, telegram string) {
	if telegram == "" {
		return
	}
	cacheKey := fmt.Sprintf("alipay_user_offline_forward%s", productName)
	ctx := context.Background()
	rdb := rds.Get()
	if rdb != nil {
		val, _ := rdb.Get(ctx, cacheKey).Result()
		if val != "" {
			return
		}
	}

	msg := fmt.Sprintf("信息通知：\n主体名称：%s\n%s", productName, message)
	data := map[string]interface{}{
		"forwards": msg,
		"chat_id":  telegram,
		"is_html":  false,
	}
	for i := 0; i < 3; i++ {
		if s.ForwardBot(data) {
			if rdb != nil {
				rdb.Set(ctx, cacheKey, "1", 180*time.Second)
			}
			return
		}
	}
	log.Printf("[Telegram] alipay_user_offline_forward 通知失败")
}

// CheckBalanceForward 余额不足通知（带缓存去重）
func (s *TelegramService) CheckBalanceForward(balance int64, telegram string, tenantID uint, tenantName string) {
	if telegram == "" {
		return
	}
	cacheKey := fmt.Sprintf("check_balance_forward_%s", telegram)
	ctx := context.Background()
	rdb := rds.Get()
	if rdb != nil {
		val, _ := rdb.Get(ctx, cacheKey).Result()
		if val != "" {
			return
		}
	}

	cfg := config.Get()
	msgTemplate := "重要通知：\n当前租户%s余额不足 `%.2f` 为了防止系统关闭请及时充值\n\n充值方法：\n登陆系统后台,右上角名字或者头像→个人设置→自主充值\n快捷充值：\n选择下方USDT金额直接充值"
	if cfg != nil && cfg.Telegram.BalanceMsg != "" {
		msgTemplate = cfg.Telegram.BalanceMsg
	}

	msg := fmt.Sprintf(msgTemplate, tenantName, float64(balance)/100.0)
	data := map[string]interface{}{
		"forwards":     msg,
		"chat_id":      telegram,
		"is_html":      false,
		"is_md":        true,
		"is_recharge":  true,
		"tenant_id":    tenantID,
		"tenant_name":  tenantName,
	}
	for i := 0; i < 3; i++ {
		if s.ForwardBot(data) {
			if rdb != nil {
				rdb.Set(ctx, cacheKey, "1", 60*time.Second)
			}
			return
		}
	}
	log.Printf("[Telegram] check_balance_forward 通知失败")
}

// CheckSplitPreForward 分账预付不足通知
func (s *TelegramService) CheckSplitPreForward(balance int64, telegram string) {
	if telegram == "" {
		return
	}
	msg := fmt.Sprintf("预付即将不足: %.2f", float64(balance)/100.0)
	s.ForwardMessage(telegram, msg, false)
}
