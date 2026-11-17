package db

import (
	"database/sql"
	"strings"
	"time"
)

// FailoverOrder 记录 ZovoCard 直充失败后，自动携带卡内余额改走 card.zzshu.pro 的容灾订单。
type FailoverOrder struct {
	ID              int64   `json:"id"`
	CDKCode         string  `json:"cdk_code"`
	RedemptionToken string  `json:"redemption_token"`
	PlanType        string  `json:"plan_type"`
	Region          string  `json:"region"`
	AccountEmail    string  `json:"account_email"`
	ZovoCardID      string  `json:"zovo_card_id"`
	CardLast4       string  `json:"card_last4"`
	CardBalanceUSD  float64 `json:"card_balance_usd"`
	ZzshuOrderNo    string  `json:"zzshu_order_no"`
	ZzshuCardKey    string  `json:"zzshu_card_key"`
	Status          string  `json:"status"` // processing / success / failed
	TriggerReason   string  `json:"trigger_reason"`
	FailureReason   string  `json:"failure_reason"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// OKXTreasuryLog 记录 OKX Agentic Wallet 自动补款与支付链接审计日志。
type OKXTreasuryLog struct {
	ID            int64   `json:"id"`
	Action        string  `json:"action"` // auto_topup / manual_topup / dry_run / a2a_pay_link
	Chain         string  `json:"chain"`
	AmountUSD     float64 `json:"amount_usd"`
	TargetAddress string  `json:"target_address"`
	TxHash        string  `json:"tx_hash"`
	Status        string  `json:"status"` // success / simulated / rejected / failed
	Detail        string  `json:"detail"`
	CreatedAt     string  `json:"created_at"`
}

func migrateMultiprotocolTables() error {
	if DB == nil {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS zzshu_failover_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			cdk_code TEXT NOT NULL DEFAULT '',
			redemption_token TEXT UNIQUE NOT NULL,
			plan_type TEXT NOT NULL DEFAULT 'plus',
			region TEXT NOT NULL DEFAULT 'PH',
			account_email TEXT NOT NULL DEFAULT '',
			zovo_card_id TEXT NOT NULL DEFAULT '',
			card_last4 TEXT NOT NULL DEFAULT '',
			card_balance_usd REAL NOT NULL DEFAULT 0,
			zzshu_order_no TEXT NOT NULL DEFAULT '',
			zzshu_card_key TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'processing',
			trigger_reason TEXT NOT NULL DEFAULT '',
			failure_reason TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_zzshu_fo_cdk ON zzshu_failover_orders(cdk_code)`,
		`CREATE INDEX IF NOT EXISTS idx_zzshu_fo_status ON zzshu_failover_orders(status)`,
		`CREATE TABLE IF NOT EXISTS okx_treasury_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			action TEXT NOT NULL,
			chain TEXT NOT NULL DEFAULT '',
			amount_usd REAL NOT NULL DEFAULT 0,
			target_address TEXT NOT NULL DEFAULT '',
			tx_hash TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_okx_logs_created ON okx_treasury_logs(created_at)`,
	}
	for _, s := range stmts {
		if _, err := DB.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func SaveFailoverOrder(o *FailoverOrder) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`
		INSERT INTO zzshu_failover_orders (
			cdk_code, redemption_token, plan_type, region, account_email,
			zovo_card_id, card_last4, card_balance_usd, zzshu_order_no, zzshu_card_key,
			status, trigger_reason, failure_reason, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(redemption_token) DO UPDATE SET
			zzshu_order_no = excluded.zzshu_order_no,
			zzshu_card_key = excluded.zzshu_card_key,
			status = excluded.status,
			failure_reason = excluded.failure_reason,
			updated_at = CURRENT_TIMESTAMP
	`,
		strings.TrimSpace(o.CDKCode),
		strings.TrimSpace(o.RedemptionToken),
		strings.TrimSpace(o.PlanType),
		strings.TrimSpace(o.Region),
		strings.TrimSpace(o.AccountEmail),
		strings.TrimSpace(o.ZovoCardID),
		strings.TrimSpace(o.CardLast4),
		o.CardBalanceUSD,
		strings.TrimSpace(o.ZzshuOrderNo),
		strings.TrimSpace(o.ZzshuCardKey),
		strings.TrimSpace(o.Status),
		strings.TrimSpace(o.TriggerReason),
		strings.TrimSpace(o.FailureReason),
	)
	return err
}

func UpdateFailoverOrderStatus(redemptionToken, status, failureReason string) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`
		UPDATE zzshu_failover_orders
		SET status = ?, failure_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE redemption_token = ?
	`, strings.TrimSpace(status), strings.TrimSpace(failureReason), strings.TrimSpace(redemptionToken))
	return err
}

func GetFailoverByTokenOrCode(token, code string) (*FailoverOrder, error) {
	if DB == nil {
		return nil, nil
	}
	var row *sql.Row
	if strings.TrimSpace(token) != "" {
		row = DB.QueryRow(`
			SELECT id, cdk_code, redemption_token, plan_type, region, account_email,
			       zovo_card_id, card_last4, card_balance_usd, zzshu_order_no, zzshu_card_key,
			       status, trigger_reason, failure_reason, created_at, updated_at
			FROM zzshu_failover_orders WHERE redemption_token = ? LIMIT 1
		`, strings.TrimSpace(token))
	} else if strings.TrimSpace(code) != "" {
		row = DB.QueryRow(`
			SELECT id, cdk_code, redemption_token, plan_type, region, account_email,
			       zovo_card_id, card_last4, card_balance_usd, zzshu_order_no, zzshu_card_key,
			       status, trigger_reason, failure_reason, created_at, updated_at
			FROM zzshu_failover_orders WHERE cdk_code = ? ORDER BY id DESC LIMIT 1
		`, strings.TrimSpace(code))
	} else {
		return nil, nil
	}
	var o FailoverOrder
	err := row.Scan(
		&o.ID, &o.CDKCode, &o.RedemptionToken, &o.PlanType, &o.Region, &o.AccountEmail,
		&o.ZovoCardID, &o.CardLast4, &o.CardBalanceUSD, &o.ZzshuOrderNo, &o.ZzshuCardKey,
		&o.Status, &o.TriggerReason, &o.FailureReason, &o.CreatedAt, &o.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func ListFailoverOrders(limit int) ([]FailoverOrder, error) {
	if DB == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := DB.Query(`
		SELECT id, cdk_code, redemption_token, plan_type, region, account_email,
		       zovo_card_id, card_last4, card_balance_usd, zzshu_order_no, zzshu_card_key,
		       status, trigger_reason, failure_reason, created_at, updated_at
		FROM zzshu_failover_orders ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FailoverOrder, 0)
	for rows.Next() {
		var o FailoverOrder
		if err := rows.Scan(
			&o.ID, &o.CDKCode, &o.RedemptionToken, &o.PlanType, &o.Region, &o.AccountEmail,
			&o.ZovoCardID, &o.CardLast4, &o.CardBalanceUSD, &o.ZzshuOrderNo, &o.ZzshuCardKey,
			&o.Status, &o.TriggerReason, &o.FailureReason, &o.CreatedAt, &o.UpdatedAt,
		); err == nil {
			out = append(out, o)
		}
	}
	return out, nil
}

func InsertOKXTreasuryLog(l OKXTreasuryLog) {
	if DB == nil {
		return
	}
	_, _ = DB.Exec(`
		INSERT INTO okx_treasury_logs (action, chain, amount_usd, target_address, tx_hash, status, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, l.Action, l.Chain, l.AmountUSD, l.TargetAddress, l.TxHash, l.Status, l.Detail)
}

func SumTodayOKXAutoTopupUSD() float64 {
	if DB == nil {
		return 0
	}
	todayPrefix := time.Now().UTC().Format("2006-01-02") + "%"
	var sum sql.NullFloat64
	_ = DB.QueryRow(`
		SELECT SUM(amount_usd) FROM okx_treasury_logs
		WHERE action IN ('auto_topup', 'manual_topup')
		  AND status = 'success'
		  AND created_at LIKE ?
	`, todayPrefix).Scan(&sum)
	if sum.Valid {
		return sum.Float64
	}
	return 0
}

func ListOKXTreasuryLogs(limit int) ([]OKXTreasuryLog, error) {
	if DB == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := DB.Query(`
		SELECT id, action, chain, amount_usd, target_address, tx_hash, status, detail, created_at
		FROM okx_treasury_logs ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OKXTreasuryLog, 0)
	for rows.Next() {
		var l OKXTreasuryLog
		if err := rows.Scan(
			&l.ID, &l.Action, &l.Chain, &l.AmountUSD, &l.TargetAddress,
			&l.TxHash, &l.Status, &l.Detail, &l.CreatedAt,
		); err == nil {
			out = append(out, l)
		}
	}
	return out, nil
}
