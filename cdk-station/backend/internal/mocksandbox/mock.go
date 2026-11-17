// Package mocksandbox 内置零成本双上游沙箱模拟器（ZovoCard Sandbox + card.zzshu.pro），
// 支持管理员一键验证「主通道正常发码兑换」与「ZovoCard 支付失败但卡内有钱 -> 自动切换 zzshu 自带卡直充容灾」。
package mocksandbox

import (
	"crypto/rand"
	"encoding/hex"
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
	Plan           string `json:"plan"`
	Status         string `json:"status"` // unused / reserved / consumed
	PaymentCountry string `json:"payment_country"`
	RedeemToken    string `json:"-"`
	OrderStatus    string `json:"-"`
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
	nextID      int64 = 1001
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
					"balance":                "120.00",
					"spendable_balance":      "100.00",
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
						"description":            "Mock 香港星链 Visa（放行 OpenAI / Claude / Grok）",
					},
				},
			})
		})

		zovoOpen.GET("/gpt-direct/plans", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{
					"version": 20261118,
					"plans": gin.H{
						"plus": gin.H{
							"key":                "plus",
							"label":              "ChatGPT Plus (1 Month)",
							"currency":           "USD",
							"enabled":            true,
							"serviceFeeUsdMinor": 100,
						},
						"pro20": gin.H{
							"key":                "pro20",
							"label":              "ChatGPT Pro 20X",
							"currency":           "USD",
							"enabled":            true,
							"serviceFeeUsdMinor": 500,
						},
					},
					"registry": []gin.H{
						{"key": "plus", "label": "ChatGPT Plus 月付", "flow": "cdk", "tier": 1, "sort_order": 1},
						{"key": "pro20", "label": "ChatGPT Pro 20X", "flow": "cdk", "tier": 2, "sort_order": 2},
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
				Plan           string `json:"plan"`
				Count          int    `json:"count"`
				PaymentCountry string `json:"payment_country"`
			}
			_ = c.ShouldBindJSON(&body)
			if body.Plan == "" {
				body.Plan = "plus"
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
					Plan:           body.Plan,
					Status:         "unused",
					PaymentCountry: body.PaymentCountry,
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
				"data": gin.H{"cdks": created, "list": created, "count": len(created)},
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
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{"list": []any{}, "total": 0},
			})
		})

		// 名下有钱的虚拟卡列表（用于演示 ZovoCard 直充失败后提取卡要素切走 zzshu）
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
							"balance":                35.50,
							"issuer":                 "four",
							"google_chatgpt_blocked": false,
						},
					},
					"total": 1,
				},
			})
		})

		zovoOpen.GET("/cards/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "ok",
				"data": gin.H{
					"id":          c.Param("id"),
					"card_number": "4937280012348899",
					"expire":      "12/29",
					"cvv":         "852",
					"status":      "ACTIVE",
					"balance":     35.50,
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
			item, ok := cdks[code]
			if !ok && strings.HasPrefix(code, "ZOVO-SBX-") {
				item = &mockCDK{
					ID:             nextID,
					Code:           code,
					CodePrefix:     code[:14],
					Plan:           "plus",
					Status:         "unused",
					PaymentCountry: "PH",
				}
				nextID++
				cdks[code] = item
				ok = true
			}
			if !ok {
				mu.Unlock()
				c.JSON(http.StatusNotFound, gin.H{"error": "CDK 不存在", "error_code": "code_not_found"})
				return
			}
			if item.RedeemToken == "" {
				item.RedeemToken = "rtok_" + strings.ToLower(randHex(12))
				tokToCode[item.RedeemToken] = item.Code
			}
			tok := item.RedeemToken
			plan := item.Plan
			region := item.PaymentCountry
			st := item.Status
			mu.Unlock()

			c.JSON(http.StatusOK, gin.H{
				"code":             code,
				"plan":             plan,
				"product":          "gpt",
				"status":           st,
				"payment_country":  region,
				"redemption_token": tok,
			})
		})

		zovoPub.POST("/preflight", func(c *gin.Context) {
			var body map[string]any
			_ = c.ShouldBindJSON(&body)
			tok, _ := body["redemption_token"].(string)
			c.JSON(http.StatusOK, gin.H{
				"ok":               true,
				"eligible":         true,
				"redemption_token": tok,
				"plan":             "plus",
				"payment_country":  "PH",
				"message":          "沙箱预检通过：账号可正常订阅",
			})
		})

		zovoPub.POST("/redeem", func(c *gin.Context) {
			var body map[string]any
			_ = c.ShouldBindJSON(&body)
			tok, _ := body["redemption_token"].(string)
			mu.Lock()
			code := tokToCode[tok]
			if item, ok := cdks[code]; ok {
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
			item := cdks[code]
			mu.Unlock()
			if item == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			if simulateZovoFail() || item.OrderStatus == "declined" {
				c.JSON(http.StatusOK, gin.H{
					"status":           "declined",
					"error_code":       "zovo_gateway_declined",
					"message":          "ZovoCard 直充网关支付失败（卡片 8899 内仍有 $35.50 可用余额）",
					"redemption_token": tok,
					"plan":             item.Plan,
					"payment_country":  item.PaymentCountry,
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"status":           "completed",
				"provider":         "zovocard",
				"redemption_token": tok,
				"plan":             item.Plan,
				"payment_country":  item.PaymentCountry,
				"message":          "ZovoCard 沙箱直充成功",
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
			email := "sandbox-buyer@kesiv.com"
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
