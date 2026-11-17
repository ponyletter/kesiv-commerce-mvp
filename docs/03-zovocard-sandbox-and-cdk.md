# 03. ZovoCard 沙箱环境、OpenAPI 与兑换站对接规范

## 一、 环境地址与鉴权

| 环境 | 控制台地址 | OpenAPI Base URL | 公开 CDK 接口前缀 |
| :--- | :--- | :--- | :--- |
| **沙箱环境 (Sandbox)** | `https://sandbox.zovocard.com` | `https://sandbox.zovocard.com/openapi/v1` | `https://sandbox.zovocard.com/api/v1/cdk` |
| **生产环境 (Production)** | `https://zovocard.com` | `https://zovocard.com/openapi/v1` | `https://zovocard.com/api/v1/cdk` |

- **鉴权方式**：在控制台「开发者」页面创建密钥对（`app_id` 为 `ak_...`，`app_secret` 为 `sk_...`）。所有 OpenAPI 请求头携带：
  ```http
  X-API-Key: sk_xxxxxxxxxxxxxxxx
  Content-Type: application/json
  Idempotency-Key: <uuid>   # 开卡、充值、直充等写操作强烈建议携带幂等键
  ```
- **沙箱隔离说明**：沙箱账户、密钥与生产完全隔离；沙箱内的开卡、充值、退款与直充均为模拟返回，不产生真实资金变动，非常适合本次全流程联调。

---

## 二、 核心 API 接口清单（摘自官方 `zovocard-openapi-zh.md`）

### 1. 账户与风控查询
- `GET /balance`：查询账户余额。
  - **关键字段**：`balance`（总余额）、`spendable_balance`（**实际可用余额**）、`account_reserve_amount`（全账户共享 `20U` 风险保证金）。**所有业务判断必须以 `spendable_balance` 为准！**
- `GET /balance-logs?page=1&page_size=20`：查询账户资金流水（用于自动补款对账）。

### 2. 虚拟卡生命周期接口（用于开卡及故障转移提取卡要素）
- `GET /products`：获取可开卡产品列表（注意检查 `issuer` 及 `restricted_merchants` / `google_chatgpt_blocked`，香港发行卡默认无禁用商户，非香港卡未配卡头时默认禁用 `GOOGLE CHATGPT`）。
- `POST /cards/open`：单张开卡（即时返回 `card_number`、`expire` `MM/YY`、`cvv`）。
- `GET /cards?page=1&page_size=20&sync=1`：获取名下卡片列表（`sync=1` 拉取实时余额，不含 CVV）。
- `GET /cards/{id}`：**获取单卡详情（含完整卡号 `card_number`、有效期 `expire`、`cvv` 及实时余额）**——当需要把卡切换到 `card.zzshu.pro` 支付时调用此接口。
- `POST /cards/recharge`：给指定虚拟卡充值。
- `POST /cards/refund`：将卡内剩余余额退回 ZovoCard 平台余额。
- `POST /cards/freeze` / `DELETE /cards/{id}`：冻结/解冻或删卡。
- `GET /cards/{id}/transactions`：查询卡片消费授权与拒付记录。

### 3. 多产品直充接口（GPT / Claude / Grok）
- `GET /gpt-direct/plans?product=gpt|claude|grok`：实时拉取当前可售档位、支持地区（默认 `PH/PHP`）及服务费（`service_fee_usd_minor`）。
- `POST /gpt-direct/orders`：提交直充订单。
  - **异步受理注意**：返回 **HTTP `202` (`"msg": "accepted"`) 仅代表入队受理，不代表充值成功**，需通过订单查询或 Webhook 获取终态。
  - 支持平台选卡，也支持 `raw_card`（自带卡模式）。

### 4. 必须遵守的平台风控与计费约束
1. **链上充值最低 `50U`**：低于 `50U` 不自动入账。
2. **保证金 `20U`**：总余额中锁定 `20U` 作为保证金，主动消费只能动用 `spendable_balance`。
3. **拒付/小额费率**：非白名单前 3 笔真实拒付免费，第 4 笔起 `$0.30+`；`0 < amount < 0.50U` 小额真实消费每笔收 `$0.30`；授权撤销按 `1%` 收取。
4. **两个特例响应需代码特判**：
   - `GET /cards/{id}/openai-payments` 卡不存在时返回 HTTP `200` 但 JSON 内 `code=404`。
   - `GET /balance-logs` 参数非法返回 HTTP `400` 时无 `error_code` 字段，仅有 `msg`。

---

## 三、 开源兑换站 `zovocard/zovo_card_cdk_auto` 现有基础与改造规划

- **开源地址**：`https://github.com/zovocard/zovo_card_cdk_auto`
- **现有能力**：
  - Go (Gin) + Vue 3 + Tailwind CSS + PostgreSQL/SQLite + Redis。
  - 已完整实现用户端 `/recharge`（CDK 校验与提交）、`/history`（进度与记录查询）及管理端 `/dashboard`、CDK 批量生成/禁用。
  - `backend/internal/cardplatform/client.go` 已原生支持从数据库 `site_settings` 表优先读取 `card_api_base` 和 `card_api_key`（未配时回退环境变量 `CARD_API_BASE` / `CARD_API_KEY`），并已封装 GPT / Claude / Grok / X Premium 相关逻辑。
- **我们需要在其上扩展的模块**：
  1. **后台多上游配置页**：除现有的 `card_api_base`（设为 `https://sandbox.zovocard.com` 即可跑沙箱）外，新增备用通道 `zzshu_api_base` (`https://card.zzshu.pro`)、`zzshu_api_key`、路由策略开关（主 ZovoCard -> 备 zzshu，或指定套餐直走某通道）。
  2. **多协议执行器（Provider Router）**：实现抽象接口 `RechargeProvider`，当 ZovoCard 直充返回渠道不可用或支付失败、且校验虚拟卡内余额充足时，自动携带该虚拟卡四要素无缝改走 `card.zzshu.pro` 的 `direct` 接口。
