package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tuzi/cdk-recharge-system/internal/config"
	"github.com/tuzi/cdk-recharge-system/internal/db"
	"github.com/tuzi/cdk-recharge-system/internal/mocksandbox"
)

func TestMultiprotocolFailoverFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	_ = os.Setenv("DB_PATH", filepath.Join(tmpDir, "test_mp.db"))
	defer os.Unsetenv("DB_PATH")

	if err := db.Init(&config.DatabaseConfig{}); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}

	r := gin.New()
	api := r.Group("/api/v1")
	mocksandbox.Mount(api.Group("/mock-sandbox"))
	api.POST("/public/cdk/preview", PublicCDKPreview)
	api.POST("/public/cdk/preflight", PublicCDKPreflight)
	api.POST("/public/cdk/redeem", PublicCDKRedeem)
	api.GET("/public/cdk/result", PublicCDKResult)

	srv := httptest.NewServer(r)
	defer srv.Close()

	// 配置指向测试沙箱，并开启模拟 ZovoCard 失败 + zzshu 自动带卡容灾
	_ = db.SetSetting("card_api_base", srv.URL+"/api/v1/mock-sandbox/zovocard")
	_ = db.SetSetting("card_api_key", "sk_mock_test")
	_ = db.SetSetting("zzshu_enabled", "true")
	_ = db.SetSetting("zzshu_auto_failover", "true")
	_ = db.SetSetting("zzshu_api_base", srv.URL+"/api/v1/mock-sandbox/zzshu")
	_ = db.SetSetting("zzshu_api_key", "CDK-ZZSHU-TEST")
	_ = db.SetSetting("zzshu_min_card_balance", "20")
	_ = db.SetSetting("mock_simulate_zovo_fail", "true")

	code := "ZOVO-SBX-TEST-0001"

	// 1. Preview
	resp1, err := http.Post(srv.URL+"/api/v1/public/cdk/preview", "application/json",
		bytes.NewBufferString(`{"code":"`+code+`"}`))
	if err != nil || resp1.StatusCode != 200 {
		t.Fatalf("preview failed: status=%v err=%v", resp1.StatusCode, err)
	}
	var prev map[string]any
	_ = json.NewDecoder(resp1.Body).Decode(&prev)
	resp1.Body.Close()
	rtok, _ := prev["redemption_token"].(string)
	if rtok == "" {
		t.Fatalf("expected redemption_token, got %v", prev)
	}

	// 2. Preflight (绑定买家 ChatGPT Session)
	sessionJSON := `{"user":{"id":"user-123","email":"buyer@kesiv.com"},"expires":"2027-02-01T00:00:00Z","account":{"id":"acc-1"},"accessToken":"at_test","sessionToken":"st_test"}`
	pfBody, _ := json.Marshal(map[string]any{
		"code":             code,
		"redemption_token": rtok,
		"credential":       map[string]any{"session": sessionJSON},
	})
	resp2, err := http.Post(srv.URL+"/api/v1/public/cdk/preflight", "application/json", bytes.NewReader(pfBody))
	if err != nil || resp2.StatusCode != 200 {
		t.Fatalf("preflight failed: %v", err)
	}
	resp2.Body.Close()

	// 3. Redeem (ZovoCard 侧将被置为 declined)
	rdBody, _ := json.Marshal(map[string]any{"redemption_token": rtok})
	resp3, err := http.Post(srv.URL+"/api/v1/public/cdk/redeem", "application/json", bytes.NewReader(rdBody))
	if err != nil || (resp3.StatusCode != 200 && resp3.StatusCode != 202) {
		t.Fatalf("redeem failed: status=%v err=%v", resp3.StatusCode, err)
	}
	resp3.Body.Close()

	// 4. 第 1 次轮询 Result：应捕获 ZovoCard declined，提取尾号 8899 ($35.50) 虚拟卡并自动创建 zzshu 直充单！
	resp4, err := http.Get(srv.URL + "/api/v1/public/cdk/result?token=" + rtok)
	if err != nil || resp4.StatusCode != 200 {
		t.Fatalf("result poll 1 failed: %v", err)
	}
	var res1 map[string]any
	_ = json.NewDecoder(resp4.Body).Decode(&res1)
	resp4.Body.Close()
	if res1["provider"] != "card.zzshu.pro" || res1["card_last4"] != "8899" {
		t.Fatalf("expected failover to card.zzshu.pro with card 8899, got %v", res1)
	}

	// 5. 第 2 次轮询 Result：zzshu 返回 success -> 本站统一映射为 completed！
	resp5, err := http.Get(srv.URL + "/api/v1/public/cdk/result?token=" + rtok)
	if err != nil || resp5.StatusCode != 200 {
		t.Fatalf("result poll 2 failed: %v", err)
	}
	var res2 map[string]any
	_ = json.NewDecoder(resp5.Body).Decode(&res2)
	resp5.Body.Close()
	if res2["status"] != "completed" {
		t.Fatalf("expected completed after zzshu poll, got %v", res2)
	}
}
