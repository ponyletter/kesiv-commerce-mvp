package cardplatform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/tuzi/cdk-recharge-system/internal/db"
)

// FundedCardSummary 名下有可用余额的虚拟卡摘要（不含 CVV）。
type FundedCardSummary struct {
	ID         string  `json:"id"`
	CardNumber string  `json:"card_number"`
	Last4      string  `json:"last4"`
	Status     string  `json:"status"`
	BalanceUSD float64 `json:"balance_usd"`
	Issuer     string  `json:"issuer"`
}

// CardSecretDetail 单卡支付四要素（仅在内存用于转发备用通道，绝不落盘或打日志）。
type CardSecretDetail struct {
	ID         string
	CardNumber string
	ExpMonth   int
	ExpYear    int
	CVV        string
	BalanceUSD float64
}

// ListFundedCards 调用 GET /openapi/v1/cards?page=1&page_size=50&sync=1 筛选余额 >= minBalance 且未在坏卡黑名单的活跃卡。
func (c *Client) ListFundedCards(ctx context.Context, minBalance float64) ([]FundedCardSummary, error) {
	data, err := c.doOpenAPI(ctx, http.MethodGet, "/cards?page=1&page_size=50&sync=1", nil, "")
	if err != nil {
		return nil, err
	}
	var rawList []map[string]any
	if err := json.Unmarshal(data, &rawList); err != nil {
		var wrapped struct {
			List []map[string]any `json:"list"`
		}
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil {
			return nil, err
		}
		rawList = wrapped.List
	}

	blocked := map[int64]bool{}
	if ids, err := db.ListActiveBlockedCardIDs(); err == nil {
		for _, id := range ids {
			blocked[id] = true
		}
	}

	out := make([]FundedCardSummary, 0)
	for _, m := range rawList {
		idStr := anyString(m["id"])
		if idStr == "" {
			idStr = anyString(m["card_id"])
		}
		if idStr == "" {
			continue
		}
		if numID, err := strconv.ParseInt(idStr, 10, 64); err == nil && blocked[numID] {
			continue
		}
		status := strings.ToUpper(anyString(m["status"]))
		if status != "" && status != "ACTIVE" && status != "NORMAL" && status != "OPEN" {
			continue
		}
		// 跳过明确禁止 ChatGPT 的卡
		if b, ok := m["google_chatgpt_blocked"].(bool); ok && b {
			continue
		}
		bal := anyFloat(m["balance"], m["available_balance"], m["card_balance"])
		if bal < minBalance {
			continue
		}
		num := anyString(m["card_number"], m["pan"], m["masked_pan"])
		last4 := anyString(m["last4"], m["last_four"])
		if last4 == "" && len(num) >= 4 {
			last4 = num[len(num)-4:]
		}
		out = append(out, FundedCardSummary{
			ID:         idStr,
			CardNumber: num,
			Last4:      last4,
			Status:     status,
			BalanceUSD: bal,
			Issuer:     anyString(m["issuer"]),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].BalanceUSD > out[j].BalanceUSD
	})
	return out, nil
}

// GetCardSecret 调用 GET /openapi/v1/cards/{id} 提取完整卡号、有效期和 CVV（仅驻留内存）。
func (c *Client) GetCardSecret(ctx context.Context, cardID string) (*CardSecretDetail, error) {
	cardID = strings.TrimSpace(cardID)
	if cardID == "" {
		return nil, fmt.Errorf("card id required")
	}
	data, err := c.doOpenAPI(ctx, http.MethodGet, "/cards/"+url.PathEscape(cardID), nil, "")
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if sub, ok := m["card"].(map[string]any); ok && len(sub) > 0 {
		for k, v := range sub {
			if _, exists := m[k]; !exists {
				m[k] = v
			}
		}
	}
	pan := digitsOnly(anyString(m["card_number"], m["pan"], m["number"]))
	cvv := strings.TrimSpace(anyString(m["cvv"], m["cvc"], m["security_code"]))
	expMonth := int(anyFloat(m["exp_month"], m["expire_month"]))
	expYear := int(anyFloat(m["exp_year"], m["expire_year"]))
	if expMonth == 0 || expYear == 0 {
		expStr := anyString(m["expire"], m["expiry"], m["expiration"])
		em, ey := parseExpiry(expStr)
		if expMonth == 0 {
			expMonth = em
		}
		if expYear == 0 {
			expYear = ey
		}
	}
	if expYear > 0 && expYear < 100 {
		expYear += 2000
	}
	if len(pan) < 12 || cvv == "" || expMonth < 1 || expMonth > 12 || expYear < 2024 {
		return nil, fmt.Errorf("卡台返回的卡片要素不完整 (card_id=%s)", cardID)
	}
	return &CardSecretDetail{
		ID:         cardID,
		CardNumber: pan,
		ExpMonth:   expMonth,
		ExpYear:    expYear,
		CVV:        cvv,
		BalanceUSD: anyFloat(m["balance"], m["available_balance"]),
	}, nil
}

func parseExpiry(s string) (int, int) {
	s = strings.TrimSpace(s)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == '-' || r == '.'
	})
	if len(parts) != 2 {
		return 0, 0
	}
	a, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	b, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	// 兼容 YYYY-MM 或 MM/YY
	if a > 12 {
		return b, a
	}
	return a, b
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func anyString(vals ...any) string {
	for _, v := range vals {
		if v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				return strings.TrimSpace(t)
			}
		case json.Number:
			return t.String()
		case float64:
			return strconv.FormatInt(int64(t), 10)
		case int64:
			return strconv.FormatInt(t, 10)
		}
	}
	return ""
}

func anyFloat(vals ...any) float64 {
	for _, v := range vals {
		if v == nil {
			continue
		}
		switch t := v.(type) {
		case float64:
			return t
		case json.Number:
			if f, err := t.Float64(); err == nil {
				return f
			}
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
				return f
			}
		case int64:
			return float64(t)
		}
	}
	return 0
}
