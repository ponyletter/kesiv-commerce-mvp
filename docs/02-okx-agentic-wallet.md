# 02. OKX Agentic Wallet（欧易智能钱包）收付款闭环方案

## 一、 官方仓库与工具链

- **官方开源仓库**：[`okx/onchainos-skills`](https://github.com/okx/onchainos-skills)
- **开发者文档中心**：`https://web3.okx.com/build/dev-docs/wallet-api`
- **一键安装 CLI 与运行时**：
  ```bash
  npx -y @okxweb3/onchainos-installer install
  ```
- **核心模块**：
  1. `okx-agentic-wallet`：基于 **TEE（可信执行环境）** 的自托管智能钱包，私钥不出 TEE；支持 20+ 公链（X Layer 0 Gas、TRON、Arbitrum、Base、BSC、Polygon、Solana、Ethereum 等），最高支持派生 50 个子钱包，内置风控扫描、限额策略（Policy）与白名单。
  2. `okx-agent-payments-protocol`：支持 **`a2a-pay`（生成支付链接 / Payment ID）**、**HTTP `x402` 自动微支付协议**、以及 `MPP` 通道支付。

---

## 二、 收款方案设计（买家 -> 你的 OKX Agentic Wallet）

为了让**普通加密货币买家（任意交易所/钱包提币扫码）**和 **OKX 智能钱包 / AI Agent 买家**都能顺畅付款，且资金 **100% 归集到你的 OKX Agentic Wallet**，采用双轨并行设计：

### 轨道 A：通用扫码收款（`Dujiao-Next` 内置 `BEpusdt` -> OKX 钱包地址）
- **组件**：[`v03413/bepusdt`](https://github.com/v03413/bepusdt)（`Dujiao-Next` 已内置 `internal/modules/payment/infrastructure/gateway/adapters/bepusdt` 适配器，零代码即可对接）。
- **原理**：
  1. 通过 `onchainos wallet` 获取你的 OKX Agentic Wallet 收款地址（如 TRON `T...`、X Layer / Arbitrum / BSC `0x...`、Solana 地址）。
  2. 将这些收款地址填入 `BEpusdt` 后台（无私钥暴露风险，`BEpusdt` 仅监听链上入账事件并按小数点尾数区分订单）。
  3. 买家在 `Dujiao-Next` 下单后直接展示 USDT/USDC 收款二维码，链上确认后自动回调 `Dujiao-Next` 发放 CDK 卡密。

### 轨道 B：OKX 原生智能支付链接 / x402（`okx-agent-payments-protocol`）
- **适用场景**：使用 OKX Wallet 的买家，或通过 API 采购卡密的下游 AI Agent。
- **生成收款单命令（Seller 侧）**：
  ```bash
  onchainos payment a2a-pay create \
    --amount "20.00" \
    --symbol "USDT" \
    --recipient "0xYourOkxAgenticWalletAddress" \
    --description "Order #20261118001" \
    --expires-in 1800
  ```
  返回 `payment_id`（形如 `a2a_...`）与 `deliveries.url` 支付链接。
- **状态轮询与自动发货**：
  ```bash
  onchainos payment a2a-pay status --payment-id <payment_id> --wait
  ```
  当返回 `terminal: true` 且交易哈希确认时，触发 `Dujiao-Next` 订单完成并发放 CDK。
- **扩展实现**：在 `Dujiao-Next` 的 `internal/modules/payment/infrastructure/gateway/adapters/` 下新增 `okxagent` 适配器（参考现有 `okpay`/`bepusdt` 适配器，约 150 行 Go 代码即可完成）。

---

## 三、 付款方案设计（你的 OKX Agentic Wallet -> 上游卡台自动补款）

当商城持续售出卡密，上游卡台（ZovoCard）余额下降时，由后台 **Treasury Watcher（金库巡检模块）** 自动使用 OKX Agentic Wallet 付款补仓：

### 1. 自动补款触发逻辑
1. 定时请求 ZovoCard `GET /openapi/v1/balance` 获取 **`spendable_balance`（可用余额）**。
2. 当 `spendable_balance < 后台设定阈值（如 30 USDT）` 时，生成补款任务。
3. **强制校验 ZovoCard 最低起充线**：
   - ZovoCard 链上充值**单笔最低 `50U`（USDT/USDC）**！低于 `50U` 不会自动入账，会卡在人工审核。
   - 因此后台自动补款单笔金额配置必须强制 `>= 50 USDT`（例如固定每次补 `50 USDT` 或 `100 USDT`）。
4. 调用 `onchainos` 执行链上转账至你在 ZovoCard 后台绑定的专属充值地址（白名单锁定目标地址，防止篡改）。
5. 轮询链上 `tx_hash` 与 ZovoCard `GET /openapi/v1/balance-logs` 确认入账，并推送 Telegram 通知给站长。

### 2. 安全护栏（必配）
- **收款白名单锁死**：付款模块只允许向管理员在后台签名确认过的上游充值地址（ZovoCard 专属地址等）转账。
- **单日限额熔断**：后台设置单笔上限（如 `200U`）与单日累计自动付款上限（如 `500U`），超出上限自动转人工 Telegram 审批。
- **预授权限额策略**：提前在 `onchainos wallet` 配置 Policy / 白名单，避免自动转账时触发邮件验证码拦截（`scene=approval_code_sent`）。
