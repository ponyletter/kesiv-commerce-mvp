// Package zzshu 对接 https://card.zzshu.pro/docs 开放 API（自带虚拟卡直充容灾通道）。
package zzshu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tuzi/cdk-recharge-system/internal/db"
)

const defaultZzshuBase = "https://card.zzshu.pro"

type Config struct {
	Enabled        bool
	AutoFailover   bool
	BaseURL        string
	APIKey         string
	MinCardBalance float64
}

func LoadConfig() Config {
	enabledStr, _ := db.GetSetting("zzshu_enabled")
	autoStr, _ := db.GetSetting("zzshu_auto_failover")
	base, _ := db.GetSetting("zzshu_api_base")
	key, _ := db.GetSetting("zzshu_api_key")
	minBalStr, _ := db.GetSetting("zzshu_min_card_balance")

	if strings.TrimSpace(base) == "" {
		base = os.Getenv("ZZSHU_API_BASE")
	}
	if strings.TrimSpace(key) == "" {
		key = os.Getenv("ZZSHU_API_KEY")
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	base = strings.TrimSuffix(base, "/api/v1")
	if base == "" {
		base = defaultZzshuBase
	}
	minBal := 20.0
	if f, err := strconv.ParseFloat(strings.TrimSpace(minBalStr), 64); err == nil && f > 0 {
		minBal = f
	}
	return Config{
		Enabled:        strings.EqualFold(strings.TrimSpace(enabledStr), "true") || enabledStr == "1",
		AutoFailover:   autoStr == "" || strings.EqualFold(strings.TrimSpace(autoStr), "true") || autoStr == "1",
		BaseURL:        base,
		APIKey:         strings.TrimSpace(key),
		MinCardBalance: minBal,
	}
}

type Client struct {
	cfg  Config
	http *http.Client
}

func NewFromSettings() *Client {
	return &Client{
		cfg:  LoadConfig(),
		http: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *Client) Config() Config {
	return c.cfg
}

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type UserInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Points   int64  `json:"points"`
	Status   string `json:"status"`
	IsActive bool   `json:"is_active"`
}

type DirectOrderRequest struct {
	OrderType  string          `json:"orderType"` // 固定 "direct"
	CardNumber string          `json:"cardNumber"`
	ExpMonth   int             `json:"expMonth"`
	ExpYear    int             `json:"expYear"`
	CVV        string          `json:"cvv"`
	Token      json.RawMessage `json:"token"`
	PlanType   string          `json:"planType"`
	Region     string          `json:"region,omitempty"`
}

type DirectOrderResponse struct {
	OrderNo   string `json:"order_no"`
	Status    string `json:"status"`
	Email     string `json:"email"`
	Message   string `json:"message"`
	CardKey   string `json:"card_key"`
	OrderType string `json:"order_type"`
	PlanType  string `json:"plan_type"`
	Region    string `json:"region"`
}

type OrderStatusData struct {
	OrderNo       string          `json:"order_no"`
	OrderType     string          `json:"order_type"`
	PlanType      string          `json:"plan_type"`
	Region        string          `json:"region"`
	CardKey       string          `json:"card_key"`
	Email         string          `json:"email"`
	Status        string          `json:"status"` // processing / success / failed
	FailureReason string          `json:"failure_reason"`
	Verification  json.RawMessage `json:"verification,omitempty"`
	FinishedAt    string          `json:"finished_at"`
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("zzshu_api_key 未配置")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	u := c.cfg.BaseURL + "/api/v1" + path
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("zzshu http=%d 非法 JSON 响应", resp.StatusCode)
	}
	if resp.StatusCode >= 400 || env.Code != 0 {
		msg := strings.TrimSpace(env.Message)
		if msg == "" {
			msg = fmt.Sprintf("http %d code %d", resp.StatusCode, env.Code)
		}
		return nil, fmt.Errorf("zzshu 错误(code=%d): %s", env.Code, msg)
	}
	return env.Data, nil
}

// GetUser GET /api/v1/third-party/user — 校验 X-API-Key 并返回剩余点数。
func (c *Client) GetUser(ctx context.Context) (*UserInfo, error) {
	data, err := c.doJSON(ctx, http.MethodGet, "/third-party/user", nil)
	if err != nil {
		return nil, err
	}
	var u UserInfo
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateDirectOrder POST /api/v1/third-party/orders/direct — 自带虚拟卡直充下单。
func (c *Client) CreateDirectOrder(ctx context.Context, req DirectOrderRequest) (*DirectOrderResponse, error) {
	req.OrderType = "direct"
	req.PlanType = NormalizePlanType(req.PlanType)
	if strings.TrimSpace(req.Region) == "" {
		req.Region = "PH"
	}
	data, err := c.doJSON(ctx, http.MethodPost, "/third-party/orders/direct", req)
	if err != nil {
		return nil, err
	}
	var out DirectOrderResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// QueryOrderStatus POST /api/v1/third-party/orders/status — 查询三方直充订单状态。
func (c *Client) QueryOrderStatus(ctx context.Context, cardKey string) (*OrderStatusData, error) {
	payload := map[string]any{
		"cardKey":             strings.TrimSpace(cardKey),
		"includeVerification": true,
	}
	data, err := c.doJSON(ctx, http.MethodPost, "/third-party/orders/status", payload)
	if err != nil {
		return nil, err
	}
	var out OrderStatusData
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// NormalizePlanType 将 ZovoCard 套餐键映射为 card.zzshu.pro 标准套餐键。
func NormalizePlanType(plan string) string {
	p := strings.ToLower(strings.TrimSpace(plan))
	switch p {
	case "", "plus", "gpt_plus", "chatgpt_plus":
		return "plus"
	case "go", "chatgptgo", "chatgpto":
		return "go"
	case "pro", "pro20", "20x", "chatgpt_pro":
		return "pro20"
	case "pro5", "5x":
		return "pro5"
	case "pro25", "25x":
		return "pro25"
	case "team", "chatgpt_team":
		return "team"
	default:
		if strings.HasPrefix(p, "credit") || strings.Contains(p, "codex") {
			return "codex"
		}
		return p
	}
}
