# 04. `card.zzshu.pro` 开放接口规范与多协议故障转移（协议多元化）

## 一、 为什么 `ZovoCard` + `card.zzshu.pro` 能完美互补？

在虚拟卡代充场景中，最常见的问题是：**你已经在 ZovoCard 成功开出了虚拟卡并充入了美元余额，但 ZovoCard 自带的直充通道（`gpt-direct`）因临时维护、并发排队或局部风控导致支付失败——此时钱还留在那张虚拟卡里。**

通过接入 `https://card.zzshu.pro/docs` 的 **`orderType=direct`（自带银行卡直充模式）**：
- 兑换站直接调 ZovoCard `GET /openapi/v1/cards/{id}` 取出这张有余额的虚拟卡 `(cardNumber, expMonth, expYear, cvv)`；
- 连同买家的 ChatGPT `session` JSON 一起提交给 `card.zzshu.pro` 执行扣款开通；
- **`card.zzshu.pro` 默认开通成功才扣点，失败不扣点**，从而实现零损耗的**跨平台协议多元化容灾**！

---

## 二、 `card.zzshu.pro` 开放 API 完整规范（实测提取自 `/docs`）

- **生产 Base URL**：`https://card.zzshu.pro/api/v1`
- **认证请求头**：
  ```http
  X-API-Key: <在后台 API管理 生成的固定 API Key 或卡密>
  Content-Type: application/json
  ```
  > **重要约定**：订单归属与 `X-API-Key` 强绑定，后续查单必须使用下单时的同一把固定 `X-API-Key`；服务端会自动加密卡号与登录态入库，**CVV 不入库**。

### 1. 查询当前 Key 状态与剩余点数
- **端点**：`GET /api/v1/third-party/user`
- **成功响应 (`200 OK`, `code=0`)**：
  ```json
  {
    "code": 0,
    "message": "success",
    "data": {
      "id": "0",
      "username": "api-user",
      "points": 100,
      "status": "active",
      "is_active": true
    }
  }
  ```

### 2. 创建自带卡直充订单（核心容灾接口）
- **端点**：`POST /api/v1/third-party/orders/direct`
- **支持套餐 (`planType`)**：
  `plus` | `go` | `pro5` | `pro20` | `pro25` | `plus_to_5x` | `renew_20x` | `to_20x` | `to_25x` | `codex` | `team`
- **支持结账地区 (`region`)**：
  默认省略时为菲律宾 `PH` (`PHP`)；支持显式传 `EG` (`EGP`)、`CL` (`CLP`)、`US` (`USD`)、`PH` (`PHP`) 等。
- **请求体示例（传入 ZovoCard 虚拟卡四要素 + 完整 ChatGPT Session JSON）**：
  ```json
  {
    "orderType": "direct",
    "cardNumber": "4242424242424242",
    "expMonth": 12,
    "expYear": 2032,
    "cvv": "123",
    "token": {
      "user": { "id": "user-xxx", "email": "buyer@example.com" },
      "expires": "2026-12-01T08:34:59.567Z",
      "account": { "id": "account-xxx" },
      "accessToken": "<chatgpt_access_token>",
      "sessionToken": "<chatgpt_session_token>"
    },
    "planType": "plus",
    "region": "PH"
  }
  ```
  > **注意**：`token` 必须原样提交 `chatgpt.com/api/auth/session` 返回的完整 JSON（必须包含 `sessionToken`、`user.id`、`expires`），不可只拼 `email` 和 `accessToken`。
- **入队成功响应 (`code=0`)**：
  ```json
  {
    "code": 0,
    "message": "success",
    "data": {
      "order_no": "12",
      "status": "processing",
      "email": "buyer@example.com",
      "card_key": "DIRECT-xxxxxx",
      "order_type": "direct",
      "plan_type": "plus",
      "region": "PH"
    }
  }
  ```
  *(必须持久化保存返回的 `data.card_key`，用于后续轮询状态！)*

### 3. 查询订单状态与 3DS 验证处理
- **端点**：`POST /api/v1/third-party/orders/status`
- **请求体**：
  ```json
  {
    "cardKey": "DIRECT-xxxxxx",
    "includeVerification": true
  }
  ```
  *(支持传数组批量查询：`{"cardKey": ["DIRECT-1", "DIRECT-2"]}`)*
- **响应状态流转**：
  - `status = "processing"`：继续轮询（间隔建议 3~5 秒）。
  - 若 `verification != null`：包含 Stripe.js `handleNextAction` 所需的 `client_secret` 与 `publishable_key`，兑换站前端可直接调起验证弹窗，完成后继续轮询原单。
  - `status = "success"`：开通完成！核销买家 CDK。
  - `status = "failed"`：建议延迟 2~3 秒二次确认；确认失败后**不扣 `zzshu` 点数**，且买家 CDK 不被核销（可提示买家重试或换卡）。

### 4. 辅助与充值接口
- `POST /api/v1/third-party/orders/history`：分页查询历史开通记录。
- `POST /api/v1/third-party/credits/recharge`：用新购入的 `zzshu` 卡密给当前固定 `X-API-Key` 自动充点。
- 工具类接口：`POST /api/v1/third-party/tools/subscription-lookup`（订阅查询）、`upgrade-quote`（升级查价）、`billing-invoices`（账单查询）。

---

## 三、 跨平台自动故障转移流程（Failover State Machine）

```
[买家在兑换站提交 CDK + ChatGPT Session]
                 │
                 ▼
      [1. 校验 CDK 有效并加分布式锁]
                 │
                 ▼
  [2. ZovoCard 检查/准备虚拟卡 (Sandbox/Prod)]
   (复用卡池内可用余额充足的卡，或调 POST /cards/open 开卡)
                 │
                 ▼
  [3. 主通道尝试：ZovoCard 直充 (POST /gpt-direct/orders)]
         ├── 成功 ──────────────────────────────► [标记 CDK 已使用，返回充值成功]
         │
         └── 失败 / 渠道维护 (403/5xx/支付器报错)
                 │
                 ▼
  [4. 实时查卡：GET /cards/{id}?sync=1]
   确认卡状态正常且卡内余额 >= 套餐所需金额？
         ├── 否（卡被锁或余额不足）──────────────► [按策略换卡或退回卡余额 POST /cards/refund]
         │
         └── 是（卡里有钱，仅 ZovoCard 代充执行器失败！）
                 │
                 ▼
  [5. 自动切换备通道：card.zzshu.pro (POST /third-party/orders/direct)]
   携带 orderType="direct" + 该张 ZovoCard 的卡号/有效期/CVV + Session
         ├── 轮询 status="success" ─────────────► [标记 CDK 已使用，记录备通道成交日志]
         └── 轮询 status="failed" ──────────────► [释放 CDK 锁（买家码不作废），告警或退卡余额]
```
