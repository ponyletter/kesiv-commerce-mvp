package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tuzi/cdk-recharge-system/internal/db"
)

// 允许写入 site_settings 的公开安全键（无密钥）
var publicSettingKeys = map[string]bool{
	"brand_name":  true,
	"brand_sub":   true,
	"skin":        true,
	"theme_mode":  true,
}

// 密钥类键：只接受写入，读出脱敏
var secretSettingKeys = map[string]bool{
	"card_api_base":             false,
	"card_api_key":              true,
	"webhook_secret":            true, // 卡台开发者页 whsec_…
	"telegram_token":            true,
	"telegram_chat_id":          false,
	"zzshu_enabled":             false,
	"zzshu_auto_failover":       false,
	"zzshu_api_base":            false,
	"zzshu_api_key":             true,
	"zzshu_min_card_balance":    false,
	"mock_simulate_zovo_fail":   false,
	"okx_receive_trc20":         false,
	"okx_receive_evm":           false,
	"okx_receive_solana":        false,
	"okx_auto_topup_enabled":    false,
	"okx_zovocard_deposit_addr": false,
	"okx_zovocard_deposit_chain": false,
	"okx_topup_trigger_usd":     false,
	"okx_topup_amount_usd":      false,
	"okx_daily_limit_usd":       false,
	// agent_swap handled specially (hash)
}

// 站点未设置时的默认外观：卡台新版（zovo）+ 夜间，与前端 theme.ts / index.html 一致。
// 已在后台选过皮肤的站点保持原样（存了 skin 设置项就不走默认）。
const (
	defaultSiteSkin  = "zovo"
	defaultThemeMode = "dark"
)

// PublicSiteConfig GET /api/v1/public/site — 用户端拉品牌/皮肤（无鉴权）
func PublicSiteConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"installed":  db.IsInstalled(),
		"brand_name": settingOr("brand_name", "Recharge Portal"),
		"brand_sub":  settingOr("brand_sub", "Account Upgrade Service"),
		"skin":       settingOr("skin", defaultSiteSkin),
		"theme_mode": settingOr("theme_mode", defaultThemeMode),
	})
}

// AdminGetSettings GET /api/v1/admin/settings
func AdminGetSettings(c *gin.Context) {
	out := gin.H{
		"brand_name": settingOr("brand_name", "Recharge Portal"),
		"brand_sub":  settingOr("brand_sub", "Account Upgrade Service"),
		"skin":       settingOr("skin", defaultSiteSkin),
		"theme_mode": settingOr("theme_mode", defaultThemeMode),
	}
	// 非密钥可读
	for k, isSecret := range secretSettingKeys {
		v, _ := db.GetSetting(k)
		if isSecret {
			out[k+"_configured"] = strings.TrimSpace(v) != ""
			out[k+"_hint"] = maskSecret(v)
		} else {
			out[k] = v
		}
	}
	out["agent_swap_password_configured"] = agentSwapPasswordConfigured()
	c.JSON(http.StatusOK, out)
}

type adminSettingsBody struct {
	BrandName      *string `json:"brand_name"`
	BrandSub       *string `json:"brand_sub"`
	Skin           *string `json:"skin"`
	ThemeMode      *string `json:"theme_mode"`
	CardAPIBase    *string `json:"card_api_base"`
	CardAPIKey     *string `json:"card_api_key"`
	TelegramToken  *string `json:"telegram_token"`
	TelegramChatID *string `json:"telegram_chat_id"`
	WebhookSecret  *string `json:"webhook_secret"`
	// 代理失败换码密码（明文一次写入，存 bcrypt；空=不改）
	AgentSwapPassword *string `json:"agent_swap_password"`
	// 协议多元化（card.zzshu.pro 备用直充通道）
	ZzshuEnabled        *string `json:"zzshu_enabled"`
	ZzshuAutoFailover   *string `json:"zzshu_auto_failover"`
	ZzshuAPIBase        *string `json:"zzshu_api_base"`
	ZzshuAPIKey         *string `json:"zzshu_api_key"`
	ZzshuMinCardBalance *string `json:"zzshu_min_card_balance"`
	MockSimulateFail    *string `json:"mock_simulate_zovo_fail"`
	// OKX Agentic Wallet 收付款与自动补款配置
	OKXReceiveTRC20     *string `json:"okx_receive_trc20"`
	OKXReceiveEVM       *string `json:"okx_receive_evm"`
	OKXReceiveSolana    *string `json:"okx_receive_solana"`
	OKXAutoTopupEnabled *string `json:"okx_auto_topup_enabled"`
	OKXZovoDepositAddr  *string `json:"okx_zovocard_deposit_addr"`
	OKXZovoDepositChain *string `json:"okx_zovocard_deposit_chain"`
	OKXTopupTriggerUSD  *string `json:"okx_topup_trigger_usd"`
	OKXTopupAmountUSD   *string `json:"okx_topup_amount_usd"`
	OKXDailyLimitUSD    *string `json:"okx_daily_limit_usd"`
}

// AdminPutSettings PUT /api/v1/admin/settings
func AdminPutSettings(c *gin.Context) {
	var body adminSettingsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	setIf := func(key string, p *string, max int) error {
		if p == nil {
			return nil
		}
		v := strings.TrimSpace(*p)
		if max > 0 && len(v) > max {
			v = v[:max]
		}
		// 空字符串对密钥类表示「不修改」；若要清空用 " " 不支持清空密钥防误操作
		if v == "" && secretSettingKeys[key] {
			return nil
		}
		return db.SetSetting(key, v)
	}

	allowedSkins := map[string]bool{
		"zovo": true, "terracotta": true, "ocean": true, "cyber": true, "forest": true, "violet": true,
		"slate": true, "rose": true, "ember": true, "noir": true, "paper": true,
	}
	if body.Skin != nil {
		s := strings.TrimSpace(*body.Skin)
		if !allowedSkins[s] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid skin"})
			return
		}
		_ = db.SetSetting("skin", s)
	}
	if body.ThemeMode != nil {
		m := strings.TrimSpace(*body.ThemeMode)
		if m != "light" && m != "dark" && m != "auto" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid theme_mode"})
			return
		}
		_ = db.SetSetting("theme_mode", m)
	}
	_ = setIf("brand_name", body.BrandName, 40)
	_ = setIf("brand_sub", body.BrandSub, 80)
	_ = setIf("card_api_base", body.CardAPIBase, 200)
	_ = setIf("card_api_key", body.CardAPIKey, 200)
	_ = setIf("telegram_token", body.TelegramToken, 200)
	_ = setIf("telegram_chat_id", body.TelegramChatID, 64)
	_ = setIf("webhook_secret", body.WebhookSecret, 200)
	_ = setIf("zzshu_enabled", body.ZzshuEnabled, 16)
	_ = setIf("zzshu_auto_failover", body.ZzshuAutoFailover, 16)
	_ = setIf("zzshu_api_base", body.ZzshuAPIBase, 200)
	_ = setIf("zzshu_api_key", body.ZzshuAPIKey, 200)
	_ = setIf("zzshu_min_card_balance", body.ZzshuMinCardBalance, 32)
	_ = setIf("mock_simulate_zovo_fail", body.MockSimulateFail, 16)
	_ = setIf("okx_receive_trc20", body.OKXReceiveTRC20, 128)
	_ = setIf("okx_receive_evm", body.OKXReceiveEVM, 128)
	_ = setIf("okx_receive_solana", body.OKXReceiveSolana, 128)
	_ = setIf("okx_auto_topup_enabled", body.OKXAutoTopupEnabled, 16)
	_ = setIf("okx_zovocard_deposit_addr", body.OKXZovoDepositAddr, 128)
	_ = setIf("okx_zovocard_deposit_chain", body.OKXZovoDepositChain, 32)
	_ = setIf("okx_topup_trigger_usd", body.OKXTopupTriggerUSD, 32)
	_ = setIf("okx_topup_amount_usd", body.OKXTopupAmountUSD, 32)
	_ = setIf("okx_daily_limit_usd", body.OKXDailyLimitUSD, 32)
	if body.AgentSwapPassword != nil {
		pw := strings.TrimSpace(*body.AgentSwapPassword)
		if pw != "" {
			if err := setAgentSwapPassword(pw); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}
	}

	auditAdmin(c, "update_settings", "site settings")
	AdminGetSettings(c)
}

func settingOr(key, def string) string {
	v, _ := db.GetSetting(key)
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func maskSecret(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}
