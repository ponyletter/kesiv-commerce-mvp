// Package mocksandbox 内置零成本双上游沙箱模拟器（ZovoCard Sandbox + card.zzshu.pro），
// 支持无外部沙箱依赖的完整浏览器端到端模拟（预览 -> 预检 preflight_token -> 兑换进度时间轴 -> 对账列表 -> 自动切 zzshu 容灾）。
package mocksandbox

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tuzi/cdk-recharge-system/internal/db"
)

type mockCDK struct {
	ID             int64  `json:"id"`
	Code           string `json:"code"`
	CodePrefix     string `json:"code_prefix"`
	Product        string `json:"product"`
	Plan           string `json:"plan"`
	Status         string `json:"status"` // unused / reserved / consumed
	PaymentCountry string `json:"payment_country"`
	AccountEmail   string `json:"account_email"`
	RedeemToken    string `json:"-"`
	OrderStatus    string `json:"-"`
	UpdatedAt      string `json:"updated_at"`
}

type mockZzshuOrder struct {
	OrderNo  string
	CardKey  string
	Email    string
	PlanType string
	Region   string
	Created  time.Time
}

var (
	mu          sync.Mutex
	nextID      int64 = 2001
	cdks              = map[string]*mockCDK{}
	tokToCode         = map[string]string{}
	zzshuOrders       = map[string]*mockZzshuOrder{}
)

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

func simulateZovoFail() bool {
	v, _ := db.GetSetting("mock_simulate_zovo_fail")
	return strings.EqualFold(strings.TrimSpace(v), "true") || v == "1"
}

func inferProductFromPlan(plan string) string {
	p := strings.ToLower(strings.TrimSpace(plan))
	if strings.Contains(p, "grok") || p == "plus_monthly" || p == "plus_yearly" || p == "premium_monthly" {
		return "grok"
	}
	if p == "pro" || strings.Contains(p, "claude") {
		return "claude"
	}
	return "gpt"
}

// resolveOrHydrateCDK 优先查内存，若重启后内存为空则从 SQLite cardplatform_cdk_codes 精确恢复套餐与地区。
func resolveOrHydrateCDK(code string) (*mockCDK, bool) {
	code = strings.TrimSpace(code)
	if item, ok := cdks[code]; ok {
		return item, true
	}
	if id, plan, country, st, ok := db.LookupStoredCDKMeta(code); ok {
		if id <= 0 {
			id = nextID
			nextID++
		}
		if plan == "" {
			plan = "plus"
		}
		if country == "" {
			country = "PH"
		}
		if st == "" {
			st = "unused"
		}
		prefix := code
		if len(prefix) > 14 {
			prefix = prefix[:14]
		}
		item := &mockCDK{
			ID:             id,
			Code:           code,
			CodePrefix:     prefix,
			Product:        inferProductFromPlan(plan),
			Plan:           plan,
			Status:         st,
			PaymentCountry: country,
			UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
		}
		cdks[code] = item
		return item, true
	}
	if strings.HasPrefix(code, "ZOVO-SBX-") || strings.HasPrefix(code, "MOCK-") {
		prefix := code
		if len(prefix) > 14 {
			prefix = prefix[:14]
		}
		item := &mockCDK{
			ID:             nextID,
			Code:           code,
			CodePrefix:     prefix,
			Product:        "gpt",
			Plan:           "plus",
			Status:         "unused",
			PaymentCountry: "PH",
			UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
		}
		nextID++
		cdks[code] = item
		return item, true
	}
	return nil, false
}

// Mount 挂载 /api/v1/mock-sandbox 路由组。
func Mount(rg *gin.RouterGroup) {
	// ==================== 1. Mock ZovoCard OpenAPI ====================
	zovoOpen := rg.Group("/zovocard/openapi/v1")
	{
		zovoOpen.GET("/balance", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{
					"balance":                "520.00",
					"spendable_balance":      "500.00",
					"account_reserve_amount": "20.00",
				},
			})
		})

		zovoOpen.GET("/products", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": []gin.H{
					{
						"id":                     101,
						"product_code":           "HK_VISA_GPT",
						"issuer":                 "four",
						"bin":                    "493728",
						"network":                "VISA",
						"issuing_area":           "HK",
						"scene":                  "AI Subscription",
						"enabled":                true,
						"google_chatgpt_blocked": false,
						"description":            "模拟香港星链商业 Visa（放行 OpenAI / Grok / Claude）",
					},
				},
			})
		})

		zovoOpen.GET("/gpt-direct/plans", func(c *gin.Context) {
			prod := strings.ToLower(strings.TrimSpace(c.Query("product")))
			if prod == "grok" {
				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"msg":  "ok",
					"data": gin.H{
						"version": 20261118,
						"plans": gin.H{
							"grok_plus_monthly": gin.H{
								"key": "grok_plus_monthly", "label": "Grok Plus 月付", "currency": "USD", "enabled": true, "serviceFeeUsdMinor": 100, "expectedAmountMinor": 800,
							},
							"grok_plus_yearly": gin.H{
								"key": "grok_plus_yearly", "label": "Grok Plus 年付", "currency": "USD", "enabled": true, "serviceFeeUsdMinor": 100, "expectedAmountMinor": 8000,
							},
							"grok_premium_monthly": gin.H{
								"key": "grok_premium_monthly", "label": "SuperGrok (Premium) 月付", "currency": "USD", "enabled": true, "serviceFeeUsdMinor": 100, "expectedAmountMinor": 3000,
							},
						},
						"registry": []gin.H{
							{"key": "plus_monthly", "label": "Grok Plus 月付 ($8)", "flow": "direct", "tier": 1, "sort_order": 1, "checkout_currency": "USD", "checkout_amount_minor": 800},
							{"key": "premium_monthly", "label": "SuperGrok 月付 ($30)", "flow": "direct", "tier": 2, "sort_order": 2, "checkout_currency": "USD", "checkout_amount_minor": 3000},
							{"key": "plus_yearly", "label": "Grok Plus 年付 ($80)", "flow": "direct", "tier": 3, "sort_order": 3, "checkout_currency": "USD", "checkout_amount_minor": 8000},
						},
						"payment_regions": []gin.H{{"country": "US", "currency": "USD"}},
					},
				})
				return
			}
			if prod == "claude" {
				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"msg":  "ok",
					"data": gin.H{
						"version": 20261118,
						"plans": gin.H{
							"claude_pro": gin.H{
								"key": "claude_pro", "label": "Claude Pro 月付", "currency": "USD", "enabled": true, "serviceFeeUsdMinor": 100, "expectedAmountMinor": 2000,
							},
						},
						"registry": []gin.H{
							{"key": "pro", "label": "Claude Pro 月付 ($20)", "flow": "direct", "tier": 1, "sort_order": 1, "checkout_currency": "USD", "checkout_amount_minor": 2000},
						},
						"payment_regions": []gin.H{{"country": "US", "currency": "USD"}},
					},
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{
					"version": 20261118,
					"plans": gin.H{
						"go": gin.H{
							"key": "go", "label": "ChatGPT Go (1 Month)", "currency": "USD", "enabled": true, "serviceFeeUsdMinor": 50, "expectedAmountMinor": 500,
						},
						"plus": gin.H{
							"key":                "plus",
							"label":              "ChatGPT Plus (1 Month)",
							"currency":           "USD",
							"enabled":            true,
							"serviceFeeUsdMinor": 100,
							"expectedAmountMinor": 2000,
						},
						"pro_20x": gin.H{
							"key":                "pro_20x",
							"label":              "ChatGPT Pro 20X ($200)",
							"currency":           "USD",
							"enabled":            true,
							"serviceFeeUsdMinor": 500,
							"expectedAmountMinor": 20000,
						},
					},
					"registry": []gin.H{
						{"key": "go", "label": "ChatGPT Go 月付", "flow": "cdk", "tier": 0, "sort_order": 1},
						{"key": "plus", "label": "ChatGPT Plus 月付", "flow": "cdk", "tier": 1, "sort_order": 2},
						{"key": "pro_20x", "label": "ChatGPT Pro 20X 月付", "flow": "cdk", "tier": 2, "sort_order": 3},
					},
					"payment_regions": []gin.H{
						{"country": "PH", "currency": "PHP"},
						{"country": "US", "currency": "USD"},
						{"country": "EG", "currency": "EGP"},
						{"country": "CL", "currency": "CLP"},
						{"country": "JP", "currency": "JPY"},
					},
				},
			})
		})

		zovoOpen.POST("/gpt-direct/cdks", func(c *gin.Context) {
			var body struct {
				Product        string `json:"product"`
				Plan           string `json:"plan"`
				Count          int    `json:"count"`
				PaymentCountry string `json:"payment_country"`
			}
			_ = c.ShouldBindJSON(&body)
			if body.Plan == "" {
				body.Plan = "plus"
			}
			if body.Product == "" {
				body.Product = inferProductFromPlan(body.Plan)
			}
			if body.Count <= 0 {
				body.Count = 1
			}
			if body.PaymentCountry == "" {
				body.PaymentCountry = "PH"
			}
			mu.Lock()
			created := make([]gin.H, 0, body.Count)
			for i := 0; i < body.Count; i++ {
				id := nextID
				nextID++
				code := "ZOVO-SBX-" + randHex(4) + "-" + randHex(4)
				prefix := code[:14]
				item := &mockCDK{
					ID:             id,
					Code:           code,
					CodePrefix:     prefix,
					Product:        body.Product,
					Plan:           body.Plan,
					Status:         "unused",
					PaymentCountry: body.PaymentCountry,
					UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
				}
				cdks[code] = item
				created = append(created, gin.H{
					"id":               id,
					"code":             code,
					"code_prefix":      prefix,
					"plan":             body.Plan,
					"status":           "unused",
					"fee_amount_minor": 100,
					"payment_country":  body.PaymentCountry,
				})
			}
			mu.Unlock()
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{
					"requested": len(created),
					"issued":    created,
					"cdks":      created,
					"list":      created,
					"count":     len(created),
				},
			})
		})

		zovoOpen.GET("/gpt-direct/cdks", func(c *gin.Context) {
			mu.Lock()
			list := make([]gin.H, 0, len(cdks))
			for _, it := range cdks {
				list = append(list, gin.H{
					"id":               it.ID,
					"code":             it.Code,
					"code_prefix":      it.CodePrefix,
					"plan":             it.Plan,
					"status":           it.Status,
					"fee_amount_minor": 100,
					"payment_country":  it.PaymentCountry,
				})
			}
			mu.Unlock()
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{"list": list, "total": len(list)},
			})
		})

		zovoOpen.GET("/gpt-direct/cdk-orders", func(c *gin.Context) {
			mu.Lock()
			orders := make([]gin.H, 0)
			for _, it := range cdks {
				if it.OrderStatus != "" {
					orders = append(orders, gin.H{
						"id":                 it.ID,
						"cdk_id":             it.ID,
						"code_prefix":        it.CodePrefix,
						"plan":               it.Plan,
						"status":             it.OrderStatus,
						"cdk_status":         it.Status,
						"account_email":      nonEmpty(it.AccountEmail, "mock-buyer@kesiv.com"),
						"card_last_four":     "8899",
						"payment_country":    it.PaymentCountry,
						"service_fee_status": "charged",
						"created_at":         it.UpdatedAt,
					})
				}
			}
			mu.Unlock()
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{"list": orders, "total": len(orders)},
			})
		})

		zovoOpen.GET("/cards", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{
					"list": []gin.H{
						{
							"id":                     "8801",
							"card_number":            "493728******8899",
							"last4":                  "8899",
							"status":                 "ACTIVE",
							"balance":                235.50,
							"issuer":                 "four",
							"google_chatgpt_blocked": false,
						},
						{
							"id":                     "8802",
							"card_number":            "493728******6621",
							"last4":                  "6621",
							"status":                 "ACTIVE",
							"balance":                35.00,
							"issuer":                 "four",
							"google_chatgpt_blocked": false,
						},
					},
					"total": 2,
				},
			})
		})

		zovoOpen.GET("/cards/:id", func(c *gin.Context) {
			id := c.Param("id")
			last4 := "8899"
			bal := 235.50
			if id == "8802" {
				last4 = "6621"
				bal = 35.00
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{
					"id":          id,
					"card_number": "493728001234" + last4,
					"expire":      "12/29",
					"cvv":         "852",
					"status":      "ACTIVE",
					"balance":     bal,
					"issuer":      "four",
				},
			})
		})
	}

	// ==================== 2. Mock ZovoCard 公开 CDK 兑换接口 ====================
	zovoPub := rg.Group("/zovocard/api/v1/cdk")
	{
		zovoPub.POST("/preview", func(c *gin.Context) {
			var body struct {
				Code string `json:"code"`
			}
			_ = c.ShouldBindJSON(&body)
			code := strings.TrimSpace(body.Code)
			mu.Lock()
			item, ok := resolveOrHydrateCDK(code)
			if !ok {
				mu.Unlock()
				c.JSON(http.StatusNotFound, gin.H{"error": "模拟环境中未找到该 CDK（请使用 ZOVO-SBX-... 格式卡密）", "error_code": "code_not_found"})
				return
			}
			if item.Status == "consumed" {
				mu.Unlock()
				c.JSON(http.StatusConflict, gin.H{"error": "该 CDK 已兑换使用", "error_code": "code_consumed"})
				return
			}
			if item.RedeemToken == "" {
				item.RedeemToken = "rtok_" + strings.ToLower(randHex(12))
				tokToCode[item.RedeemToken] = item.Code
			}
			tok := item.RedeemToken
			plan := item.Plan
			prod := nonEmpty(item.Product, inferProductFromPlan(plan))
			region := item.PaymentCountry
			st := item.Status
			mu.Unlock()

			c.JSON(http.StatusOK, gin.H{
				"code":             code,
				"plan":             plan,
				"product":          prod,
				"status":           st,
				"payment_country":  region,
				"redemption_token": tok,
				"message":          "模拟卡密有效，请点击下一步填写账号凭证",
			})
		})

		zovoPub.POST("/preflight", func(c *gin.Context) {
			var body map[string]any
			_ = c.ShouldBindJSON(&body)
			tok, _ := body["redemption_token"].(string)
			codeStr, _ := body["code"].(string)

			email := "sim-buyer@kesiv.com"
			if cred, ok := body["credential"].(map[string]any); ok {
				if em, _ := cred["email"].(string); strings.TrimSpace(em) != "" {
					email = strings.TrimSpace(em)
				} else if sessStr, _ := cred["session"].(string); strings.HasPrefix(strings.TrimSpace(sessStr), "{") {
					var sessObj map[string]any
					if json.Unmarshal([]byte(sessStr), &sessObj) == nil {
						if u, ok := sessObj["user"].(map[string]any); ok {
							if uem, _ := u["email"].(string); uem != "" {
								email = uem
							}
						}
					}
				}
			}

			mu.Lock()
			if codeStr == "" {
				codeStr = tokToCode[tok]
			}
			plan := "plus"
			region := "PH"
			if item, ok := resolveOrHydrateCDK(codeStr); ok {
				item.AccountEmail = email
				plan = item.Plan
				region = item.PaymentCountry
			}
			mu.Unlock()

			pftok := "pftok_mock_" + strings.ToLower(randHex(8))
			c.JSON(http.StatusOK, gin.H{
				"code":             0,
				"msg":              "ok",
				"redemption_token": tok,
				"preflight_token":  pftok,
				"data": gin.H{
					"ok":                        true,
					"eligible":                  true,
					"email":                     email,
					"account_email":             email,
					"currentPlan":               "free",
					"current_plan":              "free",
					"plan":                      plan,
					"payment_country":           region,
					"subscription_has_active":   false,
					"subscription_will_renew":   false,
					"preflight_token":           pftok,
					"redemption_token":          tok,
					"blocking_reason":           "",
					"message":                   "模拟预检通过：当前账号为免费版，满足直充条件",
				},
			})
		})

		zovoPub.POST("/redeem", func(c *gin.Context) {
			var body map[string]any
			_ = c.ShouldBindJSON(&body)
			tok, _ := body["redemption_token"].(string)
			mu.Lock()
			code := tokToCode[tok]
			if item, ok := resolveOrHydrateCDK(code); ok {
				item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				if simulateZovoFail() {
					item.OrderStatus = "declined"
					item.Status = "unused"
				} else {
					item.OrderStatus = "completed"
					item.Status = "consumed"
				}
			}
			mu.Unlock()
			c.JSON(http.StatusAccepted, gin.H{
				"status":           "processing",
				"redemption_token": tok,
				"message":          "accepted",
			})
		})

		zovoPub.GET("/result", func(c *gin.Context) {
			tok := strings.TrimSpace(c.Query("token"))
			mu.Lock()
			code := tokToCode[tok]
			item, ok := resolveOrHydrateCDK(code)
			mu.Unlock()
			if !ok || item == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			now := time.Now().UTC().Format(time.RFC3339)
			email := nonEmpty(item.AccountEmail, "sim-buyer@kesiv.com")

			if simulateZovoFail() || item.OrderStatus == "declined" {
				msg := "ZovoCard 模拟主通道网关拒付（卡片尾号 8899 仍有 $35.50 余额）"
				c.JSON(http.StatusOK, gin.H{
					"status":           "declined",
					"stage":            "payment",
					"error_code":       "zovo_gateway_declined",
					"message":          msg,
					"redemption_token": tok,
					"plan":             item.Plan,
					"payment_country":  item.PaymentCountry,
					"account_email":    email,
					"order": gin.H{
						"status":         "declined",
						"stage":          "payment",
						"message":        msg,
						"account_email":  email,
						"card_last_four": "8899",
					},
				})
				return
			}

			msg := "模拟直充成功！订阅已激活并自动关闭到期续费"
			c.JSON(http.StatusOK, gin.H{
				"status":           "completed",
				"stage":            "completed",
				"provider":         "zovocard_mock",
				"redemption_token": tok,
				"plan":             item.Plan,
				"payment_country":  item.PaymentCountry,
				"account_email":    email,
				"message":          msg,
				"order": gin.H{
					"status":         "completed",
					"stage":          "completed",
					"message":        msg,
					"account_email":  email,
					"card_last_four": "8899",
				},
				"events": []gin.H{
					{"step": "queued", "category": "info", "message": "模拟订单已入队受理", "created_at": now},
					{"step": "checkout", "category": "info", "message": "已分配模拟星链 Visa 虚拟卡 (尾号 8899)", "created_at": now},
					{"step": "payment", "category": "success", "message": "Stripe 账单扣款成功", "created_at": now},
					{"step": "completed", "category": "success", "message": msg, "created_at": now},
				},
			})
		})
	}

	// ==================== 3. Mock card.zzshu.pro 开放 API ====================
	zz := rg.Group("/zzshu/api/v1/third-party")
	{
		zz.GET("/user", func(c *gin.Context) {
			if strings.TrimSpace(c.GetHeader("X-API-Key")) == "" {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 40101, "message": "missing X-API-Key"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code":    0,
				"message": "success",
				"data": gin.H{
					"id":        "0",
					"username":  "api-user",
					"email":     "",
					"points":    99,
					"role":      "normal",
					"status":    "active",
					"is_active": true,
				},
			})
		})

		zz.POST("/orders/direct", func(c *gin.Context) {
			if strings.TrimSpace(c.GetHeader("X-API-Key")) == "" {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 40101, "message": "missing X-API-Key"})
				return
			}
			var body struct {
				OrderType  string         `json:"orderType"`
				CardNumber string         `json:"cardNumber"`
				ExpMonth   int            `json:"expMonth"`
				ExpYear    int            `json:"expYear"`
				CVV        string         `json:"cvv"`
				Token      map[string]any `json:"token"`
				PlanType   string         `json:"planType"`
				Region     string         `json:"region"`
			}
			if err := c.ShouldBindJSON(&body); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "message": "参数解析失败"})
				return
			}
			if body.OrderType != "direct" || len(body.CardNumber) < 12 || body.CVV == "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "message": "必须使用 orderType=direct 并提交完整银行卡要素"})
				return
			}
			email := "sim-buyer@kesiv.com"
			if u, ok := body.Token["user"].(map[string]any); ok {
				if em, _ := u["email"].(string); em != "" {
					email = em
				}
			}
			cardKey := "DIRECT-SBX-" + randHex(5)
			orderNo := "ZZ-" + randHex(3)
			mu.Lock()
			zzshuOrders[cardKey] = &mockZzshuOrder{
				OrderNo:  orderNo,
				CardKey:  cardKey,
				Email:    email,
				PlanType: body.PlanType,
				Region:   body.Region,
				Created:  time.Now(),
			}
			mu.Unlock()
			c.JSON(http.StatusOK, gin.H{
				"code":    0,
				"message": "success",
				"data": gin.H{
					"order_no":   orderNo,
					"status":     "processing",
					"email":      email,
					"pay_link":   nil,
					"message":    "下单成功，订单已进入队列处理",
					"card_key":   cardKey,
					"order_type": "direct",
					"plan_type":  body.PlanType,
					"region":     body.Region,
				},
			})
		})

		zz.POST("/orders/status", func(c *gin.Context) {
			var body struct {
				CardKey string `json:"cardKey"`
			}
			_ = c.ShouldBindJSON(&body)
			mu.Lock()
			ord := zzshuOrders[strings.TrimSpace(body.CardKey)]
			mu.Unlock()
			if ord == nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 40401, "message": "订单不存在"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code":    0,
				"message": "success",
				"data": gin.H{
					"order_no":     ord.OrderNo,
					"order_type":   "direct",
					"plan_type":    ord.PlanType,
					"region":       ord.Region,
					"card_key":     ord.CardKey,
					"email":        ord.Email,
					"status":       "success",
					"verification": nil,
					"finished_at":  time.Now().Format("2006-01-02 15:04:05"),
				},
			})
		})
	}
}

func nonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
