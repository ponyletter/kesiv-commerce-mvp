// Package okxwallet 封装 OKX Agentic Wallet (onchainos CLI) 收款链接生成与上游卡台自动补款守护。
package okxwallet

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tuzi/cdk-recharge-system/internal/cardplatform"
	"github.com/tuzi/cdk-recharge-system/internal/db"
)

const MinZovoCardDepositUSD = 50.0 // ZovoCard 硬性规定：链上单笔充值最低 50U

type Config struct {
	ReceiveTRC20        string  `json:"okx_receive_trc20"`
	ReceiveEVM          string  `json:"okx_receive_evm"`
	ReceiveSolana       string  `json:"okx_receive_solana"`
	AutoTopupEnabled    bool    `json:"okx_auto_topup_enabled"`
	ZovoDepositAddr     string  `json:"okx_zovocard_deposit_addr"`
	ZovoDepositChain    string  `json:"okx_zovocard_deposit_chain"`
	TopupTriggerUSD     float64 `json:"okx_topup_trigger_usd"`
	TopupAmountUSD      float64 `json:"okx_topup_amount_usd"`
	DailyLimitUSD       float64 `json:"okx_daily_limit_usd"`
}

func LoadConfig() Config {
	trc20, _ := db.GetSetting("okx_receive_trc20")
	evm, _ := db.GetSetting("okx_receive_evm")
	sol, _ := db.GetSetting("okx_receive_solana")
	autoStr, _ := db.GetSetting("okx_auto_topup_enabled")
	depAddr, _ := db.GetSetting("okx_zovocard_deposit_addr")
	depChain, _ := db.GetSetting("okx_zovocard_deposit_chain")
	trigStr, _ := db.GetSetting("okx_topup_trigger_usd")
	amtStr, _ := db.GetSetting("okx_topup_amount_usd")
	limStr, _ := db.GetSetting("okx_daily_limit_usd")

	if strings.TrimSpace(depChain) == "" {
		depChain = "xlayer"
	}
	trig := parseFloatDefault(trigStr, 30.0)
	amt := parseFloatDefault(amtStr, 50.0)
	if amt < MinZovoCardDepositUSD {
		amt = MinZovoCardDepositUSD
	}
	lim := parseFloatDefault(limStr, 200.0)

	return Config{
		ReceiveTRC20:     strings.TrimSpace(trc20),
		ReceiveEVM:       strings.TrimSpace(evm),
		ReceiveSolana:    strings.TrimSpace(sol),
		AutoTopupEnabled: strings.EqualFold(strings.TrimSpace(autoStr), "true") || autoStr == "1",
		ZovoDepositAddr:  strings.TrimSpace(depAddr),
		ZovoDepositChain: strings.TrimSpace(depChain),
		TopupTriggerUSD:  trig,
		TopupAmountUSD:   amt,
		DailyLimitUSD:    lim,
	}
}

type CLIProbeResult struct {
	Installed   bool    `json:"installed"`
	Binary      string  `json:"binary"`
	LoggedIn    bool    `json:"logged_in"`
	Email       string  `json:"email,omitempty"`
	AccountName string  `json:"account_name,omitempty"`
	RawOutput   string  `json:"raw_output"`
	TodayUsed   float64 `json:"today_used_usd"`
}

func ProbeCLI(ctx context.Context) CLIProbeResult {
	res := CLIProbeResult{TodayUsed: db.SumTodayOKXAutoTopupUSD()}
	bin, err := exec.LookPath("onchainos")
	if err != nil {
		res.RawOutput = "onchainos CLI 未安装（可执行 npx -y @okxweb3/onchainos-installer install 安装）"
		return res
	}
	res.Installed = true
	res.Binary = bin

	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, "wallet", "status").CombinedOutput()
	txt := strings.TrimSpace(string(out))
	res.RawOutput = txt

	var parsed struct {
		OK   bool `json:"ok"`
		Data struct {
			LoggedIn           bool   `json:"loggedIn"`
			Email              string `json:"email"`
			CurrentAccountName string `json:"currentAccountName"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &parsed) == nil && parsed.OK {
		res.LoggedIn = parsed.Data.LoggedIn
		res.Email = parsed.Data.Email
		res.AccountName = parsed.Data.CurrentAccountName
	} else if err == nil && !strings.Contains(strings.ToLower(txt), "not logged") {
		res.LoggedIn = true
	}
	return res
}

type A2APayLinkResult struct {
	PaymentID   string `json:"payment_id"`
	DeliveryURL string `json:"delivery_url"`
	Amount      string `json:"amount"`
	Symbol      string `json:"symbol"`
	Recipient   string `json:"recipient"`
	Simulated   bool   `json:"simulated"`
}

// CreateA2APayLink 调用 OKX Agent Payments Protocol 创建收款链接。
func CreateA2APayLink(ctx context.Context, amount, symbol, description string) (*A2APayLinkResult, error) {
	cfg := LoadConfig()
	recipient := cfg.ReceiveEVM
	if recipient == "" {
		return nil, fmt.Errorf("请先在后台配置 OKX EVM 收款地址 (0x...)")
	}
	if strings.TrimSpace(symbol) == "" {
		symbol = "USDT"
	}
	bin, err := exec.LookPath("onchainos")
	if err != nil {
		// CLI 未登录/安装时返回清晰提示
		return nil, fmt.Errorf("本机尚未登录 onchainos CLI，请先使用 BEpusdt 扫码通道或登录 onchainos wallet login")
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	args := []string{
		"payment", "a2a-pay", "create",
		"--amount", amount,
		"--symbol", symbol,
		"--recipient", recipient,
		"--expires-in", "1800",
	}
	if strings.TrimSpace(description) != "" {
		args = append(args, "--description", description)
	}
	out, err := exec.CommandContext(cctx, bin, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("onchainos a2a-pay 失败: %s", strings.TrimSpace(string(out)))
	}
	var parsed map[string]any
	_ = json.Unmarshal(out, &parsed)
	pid := extractStr(parsed, "payment_id")
	durl := ""
	if deliv, ok := parsed["deliveries"].(map[string]any); ok {
		durl = extractStr(deliv, "url")
	}
	db.InsertOKXTreasuryLog(db.OKXTreasuryLog{
		Action:        "a2a_pay_link",
		Chain:         "evm",
		AmountUSD:     parseFloatDefault(amount, 0),
		TargetAddress: recipient,
		TxHash:        pid,
		Status:        "success",
		Detail:        description,
	})
	return &A2APayLinkResult{
		PaymentID:   pid,
		DeliveryURL: durl,
		Amount:      amount,
		Symbol:      symbol,
		Recipient:   recipient,
	}, nil
}

var topupMu sync.Mutex

// CheckAndTopup 检查 ZovoCard 可消费余额并在低于水位线时用 OKX Agentic Wallet 自动打款。
func CheckAndTopup(ctx context.Context, manual bool, dryRun bool) (map[string]any, error) {
	topupMu.Lock()
	defer topupMu.Unlock()

	cfg := LoadConfig()
	if !manual && !cfg.AutoTopupEnabled {
		return map[string]any{"skipped": true, "reason": "auto_topup_disabled"}, nil
	}
	if cfg.TopupAmountUSD < MinZovoCardDepositUSD {
		return nil, fmt.Errorf("ZovoCard 链上充值单笔最低 50U，当前配置 %.2fU 被拦截", cfg.TopupAmountUSD)
	}
	if cfg.ZovoDepositAddr == "" {
		return nil, fmt.Errorf("未配置 ZovoCard 专属充值白名单地址 (okx_zovocard_deposit_addr)")
	}

	cli := cardplatform.NewFromSettings()
	bal, err := cli.GetBalance(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询卡台余额失败: %w", err)
	}
	spendable, _ := strconv.ParseFloat(bal.SpendableBalance.String(), 64)
	if !manual && spendable >= cfg.TopupTriggerUSD {
		return map[string]any{
			"skipped":           true,
			"reason":            "spendable_balance_sufficient",
			"spendable_balance": spendable,
			"trigger_usd":       cfg.TopupTriggerUSD,
		}, nil
	}

	todayUsed := db.SumTodayOKXAutoTopupUSD()
	if todayUsed+cfg.TopupAmountUSD > cfg.DailyLimitUSD {
		msg := fmt.Sprintf("触发单日自动补款熔断上限: 今日已用 %.2fU + 本次 %.2fU > 上限 %.2fU", todayUsed, cfg.TopupAmountUSD, cfg.DailyLimitUSD)
		db.InsertOKXTreasuryLog(db.OKXTreasuryLog{
			Action:        "auto_topup",
			Chain:         cfg.ZovoDepositChain,
			AmountUSD:     cfg.TopupAmountUSD,
			TargetAddress: cfg.ZovoDepositAddr,
			Status:        "rejected",
			Detail:        msg,
		})
		return nil, fmt.Errorf("%s", msg)
	}

	if dryRun {
		detail := fmt.Sprintf("演练通过：卡台可用余额 %.2fU (阈值 %.2fU)，将通过 %s 向白名单地址 %s 转入 %.2f USDT",
			spendable, cfg.TopupTriggerUSD, cfg.ZovoDepositChain, cfg.ZovoDepositAddr, cfg.TopupAmountUSD)
		db.InsertOKXTreasuryLog(db.OKXTreasuryLog{
			Action:        "dry_run",
			Chain:         cfg.ZovoDepositChain,
			AmountUSD:     cfg.TopupAmountUSD,
			TargetAddress: cfg.ZovoDepositAddr,
			TxHash:        "DRYRUN-" + strconv.FormatInt(time.Now().Unix(), 10),
			Status:        "simulated",
			Detail:        detail,
		})
		return map[string]any{
			"ok":                true,
			"dry_run":           true,
			"spendable_balance": spendable,
			"amount_usd":        cfg.TopupAmountUSD,
			"target_address":    cfg.ZovoDepositAddr,
			"chain":             cfg.ZovoDepositChain,
			"message":           detail,
		}, nil
	}

	bin, err := exec.LookPath("onchainos")
	if err != nil {
		return nil, fmt.Errorf("onchainos CLI 未安装，请先安装并登录 TEE 会话后再关闭演练模式")
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	amtText := strconv.FormatFloat(cfg.TopupAmountUSD, 'f', 2, 64)
	cmd := exec.CommandContext(cctx, bin, "wallet", "send",
		"--chain", cfg.ZovoDepositChain,
		"--to", cfg.ZovoDepositAddr,
		"--amount", amtText,
		"--symbol", "USDT",
	)
	out, err := cmd.CombinedOutput()
	rawText := strings.TrimSpace(string(out))
	actionName := "auto_topup"
	if manual {
		actionName = "manual_topup"
	}
	if err != nil {
		db.InsertOKXTreasuryLog(db.OKXTreasuryLog{
			Action:        actionName,
			Chain:         cfg.ZovoDepositChain,
			AmountUSD:     cfg.TopupAmountUSD,
			TargetAddress: cfg.ZovoDepositAddr,
			Status:        "failed",
			Detail:        rawText,
		})
		return nil, fmt.Errorf("OKX 转账命令失败: %s", rawText)
	}
	db.InsertOKXTreasuryLog(db.OKXTreasuryLog{
		Action:        actionName,
		Chain:         cfg.ZovoDepositChain,
		AmountUSD:     cfg.TopupAmountUSD,
		TargetAddress: cfg.ZovoDepositAddr,
		Status:        "success",
		Detail:        rawText,
	})
	return map[string]any{
		"ok":             true,
		"amount_usd":     cfg.TopupAmountUSD,
		"target_address": cfg.ZovoDepositAddr,
		"chain":          cfg.ZovoDepositChain,
		"cli_output":     rawText,
	}, nil
}

// StartWatcher 后台每 5 分钟巡检一次卡台余额（仅在开启 okx_auto_topup_enabled 时实际执行）。
func StartWatcher(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := CheckAndTopup(ctx, false, false); err != nil {
					log.Printf("[okx-treasury] auto check error: %v", err)
				}
			}
		}
	}()
}

func parseFloatDefault(s string, def float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && f > 0 {
		return f
	}
	return def
}

func extractStr(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[k].(string); ok {
		return s
	}
	if data, ok := m["data"].(map[string]any); ok {
		if s, ok := data[k].(string); ok {
			return s
		}
	}
	return ""
}
