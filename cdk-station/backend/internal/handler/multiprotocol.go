package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/tuzi/cdk-recharge-system/internal/cardplatform"
	"github.com/tuzi/cdk-recharge-system/internal/db"
	"github.com/tuzi/cdk-recharge-system/internal/okxwallet"
	"github.com/tuzi/cdk-recharge-system/internal/zzshu"
)

var failoverMu sync.Mutex

// AdminZzshuPing GET /api/v1/admin/multiprotocol/zzshu/ping — 检测 card.zzshu.pro 连通性与剩余点数。
func AdminZzshuPing(c *gin.Context) {
	cli := zzshu.NewFromSettings()
	u, err := cli.GetUser(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"ok":    false,
			"error": err.Error(),
			"base":  cli.Config().BaseURL,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"base":      cli.Config().BaseURL,
		"points":    u.Points,
		"status":    u.Status,
		"is_active": u.IsActive,
		"username":  u.Username,
	})
}

// AdminListFailoverOrders GET /api/v1/admin/multiprotocol/failover-orders
func AdminListFailoverOrders(c *gin.Context) {
	list, err := db.ListFailoverOrders(50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list, "total": len(list)})
}

// AdminListFundedCards GET /api/v1/admin/multiprotocol/funded-cards — 查看当前可用于容灾直充的有余额虚拟卡。
func AdminListFundedCards(c *gin.Context) {
	zzCfg := zzshu.LoadConfig()
	cli := cardplatform.NewFromSettings()
	cards, err := cli.ListFundedCards(c.Request.Context(), zzCfg.MinCardBalance)
	if err != nil {
		writeCardErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"min_balance_usd": zzCfg.MinCardBalance,
		"list":            cards,
		"total":           len(cards),
	})
}

// AdminEnableMockSandbox POST /api/v1/admin/multiprotocol/enable-mock — 一键切换至内置双上游联调沙箱。
func AdminEnableMockSandbox(c *gin.Context) {
	var body struct {
		Port             int  `json:"port"`
		SimulateZovoFail bool `json:"simulate_zovo_fail"`
	}
	_ = c.ShouldBindJSON(&body)
	port := body.Port
	if port <= 0 {
		if envP, err := strconv.Atoi(strings.TrimSpace(os.Getenv("SERVER_PORT"))); err == nil && envP > 0 {
			port = envP
		} else {
			port = 8080
		}
	}
	localSelf := fmt.Sprintf("http://127.0.0.1:%d/api/v1/mock-sandbox", port)
	_ = db.SetSetting("card_api_base", localSelf+"/zovocard")
	_ = db.SetSetting("card_api_key", "sk_mock_sandbox_demo_key")
	_ = db.SetSetting("zzshu_enabled", "true")
	_ = db.SetSetting("zzshu_auto_failover", "true")
	_ = db.SetSetting("zzshu_api_base", localSelf+"/zzshu")
	_ = db.SetSetting("zzshu_api_key", "CDK-ZZSHU-MOCK-99PTS")
	_ = db.SetSetting("zzshu_min_card_balance", "20")
	if body.SimulateZovoFail {
		_ = db.SetSetting("mock_simulate_zovo_fail", "true")
	} else {
		_ = db.SetSetting("mock_simulate_zovo_fail", "false")
	}
	auditAdmin(c, "enable_mock_sandbox", fmt.Sprintf("simulate_zovo_fail=%v", body.SimulateZovoFail))
	c.JSON(http.StatusOK, gin.H{
		"ok":                 true,
		"card_api_base":      localSelf + "/zovocard",
		"zzshu_api_base":     localSelf + "/zzshu",
		"simulate_zovo_fail": body.SimulateZovoFail,
	})
}

// AdminOKXStatus GET /api/v1/admin/okx/status
func AdminOKXStatus(c *gin.Context) {
	cfg := okxwallet.LoadConfig()
	probe := okxwallet.ProbeCLI(c.Request.Context())
	logs, _ := db.ListOKXTreasuryLogs(20)
	c.JSON(http.StatusOK, gin.H{
		"config": cfg,
		"cli":    probe,
		"logs":   logs,
	})
}

// AdminOKXTopup POST /api/v1/admin/okx/topup — 手动或演练触发 OKX Agentic Wallet 向 ZovoCard 补款。
func AdminOKXTopup(c *gin.Context) {
	var body struct {
		DryRun bool `json:"dry_run"`
	}
	_ = c.ShouldBindJSON(&body)
	res, err := okxwallet.CheckAndTopup(c.Request.Context(), true, body.DryRun)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	auditAdmin(c, "okx_topup", fmt.Sprintf("dry_run=%v", body.DryRun))
	c.JSON(http.StatusOK, res)
}

// AdminOKXCreatePayLink POST /api/v1/admin/okx/a2a-link — 创建 OKX Agent Payments Protocol 收款链接。
func AdminOKXCreatePayLink(c *gin.Context) {
	var body struct {
		Amount      string `json:"amount"`
		Symbol      string `json:"symbol"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Amount) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount is required"})
		return
	}
	res, err := okxwallet.CreateA2APayLink(c.Request.Context(), body.Amount, body.Symbol, body.Description)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": res})
}

// TryZzshuFailoverResult 在轮询兑换结果时拦截主通道失败：
// 1) 若该 token/code 已走过 zzshu 容灾，直接轮询 zzshu 最新状态并返回统一结构；
// 2) 若 ZovoCard 刚返回 declined / failed / failed_precharge，且后台开启了 zzshu 自动容灾，
//    自动从 ZovoCard 提取有余额的虚拟卡四要素并提交 zzshu orderType=direct 补单！
func TryZzshuFailoverResult(ctx context.Context, token, code string, zovoStatus int, zovoRaw json.RawMessage) (int, map[string]any, bool) {
	zzCfg := zzshu.LoadConfig()

	// 1. 先看是否已有正在进行或已完成的容灾单
	if existing, _ := db.GetFailoverByTokenOrCode(token, code); existing != nil && existing.ZzshuCardKey != "" {
		return pollExistingFailover(ctx, existing)
	}

	if !zzCfg.Enabled || !zzCfg.AutoFailover || zzCfg.APIKey == "" {
		return 0, nil, false
	}

	// 2. 判断 ZovoCard 是否处于支付失败状态
	isFailed := zovoStatus >= 500
	plan := "plus"
	region := "PH"
	failReason := fmt.Sprintf("zovocard_http_%d", zovoStatus)

	var zovoMap map[string]any
	if len(zovoRaw) > 0 && json.Unmarshal(zovoRaw, &zovoMap) == nil {
		st := strings.ToLower(strAny(zovoMap["status"]))
		if st == "" {
			if ord, ok := zovoMap["order"].(map[string]any); ok {
				st = strings.ToLower(strAny(ord["status"]))
			}
		}
		if p := strAny(zovoMap["plan"]); p != "" {
			plan = p
		}
		if r := strAny(zovoMap["payment_country"]); r != "" {
			region = r
		}
		if st == "declined" || st == "failed" || st == "failed_precharge" || st == "upstream_error" {
			isFailed = true
			ec := strAny(zovoMap["error_code"])
			msg := strAny(zovoMap["message"])
			failReason = strings.TrimSpace(ec + " " + msg)
		}
	}
	if !isFailed {
		return 0, nil, false
	}

	// 3. 加互斥锁防止并发重复触发补单
	failoverMu.Lock()
	defer failoverMu.Unlock()

	if existing, _ := db.GetFailoverByTokenOrCode(token, code); existing != nil && existing.ZzshuCardKey != "" {
		return pollExistingFailover(ctx, existing)
	}

	// 查找已绑定的买家 ChatGPT Session
	if code == "" && token != "" {
		if found, err := db.FindCodeByRedemptionToken(token); err == nil {
			code = found
		}
	}
	bind, err := db.GetBindingByCDK(code)
	if err != nil || bind == nil || strings.TrimSpace(bind.SessionPayload) == "" {
		log.Printf("[zzshu-failover] skip: no session payload bound for code=%s tok=%s", code, shortTok(token))
		return 0, nil, false
	}
	if token == "" {
		token = bind.RedemptionToken
	}

	// 校验 Session 是否合法 JSON
	var rawToken json.RawMessage
	trimmedSess := strings.TrimSpace(bind.SessionPayload)
	if !json.Valid([]byte(trimmedSess)) || !strings.HasPrefix(trimmedSess, "{") {
		log.Printf("[zzshu-failover] skip: session is not JSON object for code=%s", code)
		return 0, nil, false
	}
	rawToken = json.RawMessage(trimmedSess)

	// 4. 查询 ZovoCard 名下余额充足的活跃虚拟卡
	cpCli := cardplatform.NewFromSettings()
	cards, err := cpCli.ListFundedCards(ctx, zzCfg.MinCardBalance)
	if err != nil || len(cards) == 0 {
		log.Printf("[zzshu-failover] no funded card >= %.2f USD available: %v", zzCfg.MinCardBalance, err)
		return 0, nil, false
	}
	chosen := cards[0]
	secret, err := cpCli.GetCardSecret(ctx, chosen.ID)
	if err != nil {
		log.Printf("[zzshu-failover] fetch card secret id=%s failed: %v", chosen.ID, err)
		return 0, nil, false
	}

	// 5. 提交 card.zzshu.pro 自带卡直充订单 (orderType="direct")
	zzCli := zzshu.NewFromSettings()
	resp, err := zzCli.CreateDirectOrder(ctx, zzshu.DirectOrderRequest{
		OrderType:  "direct",
		CardNumber: secret.CardNumber,
		ExpMonth:   secret.ExpMonth,
		ExpYear:    secret.ExpYear,
		CVV:        secret.CVV,
		Token:      rawToken,
		PlanType:   plan,
		Region:     region,
	})
	// 清空内存中的敏感卡号与 CVV
	secret.CardNumber = ""
	secret.CVV = ""

	if err != nil {
		log.Printf("[zzshu-failover] zzshu CreateDirectOrder failed: %v", err)
		return 0, nil, false
	}

	fo := &db.FailoverOrder{
		CDKCode:         code,
		RedemptionToken: token,
		PlanType:        resp.PlanType,
		Region:          resp.Region,
		AccountEmail:    resp.Email,
		ZovoCardID:      chosen.ID,
		CardLast4:       chosen.Last4,
		CardBalanceUSD:  chosen.BalanceUSD,
		ZzshuOrderNo:    resp.OrderNo,
		ZzshuCardKey:    resp.CardKey,
		Status:          "processing",
		TriggerReason:   failReason,
	}
	_ = db.SaveFailoverOrder(fo)
	log.Printf("[zzshu-failover] triggered! code=%s card_id=%s(last4=%s,$%.2f) -> zzshu_key=%s",
		code, chosen.ID, chosen.Last4, chosen.BalanceUSD, resp.CardKey)

	return http.StatusOK, map[string]any{
		"status":             "processing",
		"provider":           "card.zzshu.pro",
		"failover_triggered": true,
		"zzshu_card_key":     resp.CardKey,
		"card_last4":         chosen.Last4,
		"cdk_code":           code,
		"redemption_token":   token,
		"plan":               resp.PlanType,
		"payment_country":    resp.Region,
		"account_email":      resp.Email,
		"message":            fmt.Sprintf("主通道繁忙，已自动启用卡片尾号 %s 切换备用协议(card.zzshu.pro)继续开通...", chosen.Last4),
	}, true
}

func pollExistingFailover(ctx context.Context, fo *db.FailoverOrder) (int, map[string]any, bool) {
	zzCli := zzshu.NewFromSettings()
	stData, err := zzCli.QueryOrderStatus(ctx, fo.ZzshuCardKey)
	if err != nil {
		// 查询瞬时网络抖动时保持 processing，避免误判失败
		return http.StatusOK, map[string]any{
			"status":              mapZzshuStatusToPublic(fo.Status),
			"provider":           "card.zzshu.pro",
			"failover_triggered": true,
			"zzshu_card_key":     fo.ZzshuCardKey,
			"card_last4":         fo.CardLast4,
			"cdk_code":           fo.CDKCode,
			"redemption_token":   fo.RedemptionToken,
			"message":            "备用协议(card.zzshu.pro)正在同步开通进度...",
		}, true
	}

	if stData.Status != "" && stData.Status != fo.Status {
		_ = db.UpdateFailoverOrderStatus(fo.RedemptionToken, stData.Status, stData.FailureReason)
		fo.Status = stData.Status
		fo.FailureReason = stData.FailureReason
	}

	pubStatus := mapZzshuStatusToPublic(fo.Status)
	msg := fmt.Sprintf("备用协议(card.zzshu.pro · 卡尾号 %s)正在处理中", fo.CardLast4)
	if pubStatus == "completed" {
		msg = fmt.Sprintf("已通过备用协议(card.zzshu.pro · 卡尾号 %s)成功完成开通！", fo.CardLast4)
	} else if pubStatus == "failed" {
		msg = "主备双通道均未能完成支付: " + fo.FailureReason
	}

	out := map[string]any{
		"status":             pubStatus,
		"provider":           "card.zzshu.pro",
		"failover_triggered": true,
		"zzshu_order_no":     fo.ZzshuOrderNo,
		"zzshu_card_key":     fo.ZzshuCardKey,
		"card_last4":         fo.CardLast4,
		"cdk_code":           fo.CDKCode,
		"redemption_token":   fo.RedemptionToken,
		"plan":               fo.PlanType,
		"payment_country":    fo.Region,
		"account_email":      nonEmptyStr(stData.Email, fo.AccountEmail),
		"message":            msg,
	}
	if len(stData.Verification) > 0 && string(stData.Verification) != "null" {
		var ver any
		if json.Unmarshal(stData.Verification, &ver) == nil {
			out["verification"] = ver
		}
	}
	if pubStatus == "failed" {
		out["error"] = msg
	}
	return http.StatusOK, out, true
}

func mapZzshuStatusToPublic(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success", "completed":
		return "completed"
	case "failed", "cancelled", "rejected":
		return "failed"
	default:
		return "processing"
	}
}

func nonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
