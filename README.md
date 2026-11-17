# Next-Gen Open-Source Auto-Delivery & Multi-Protocol CDK Platform

基于成熟开源组件构建的新一代虚拟商品发卡、OKX 智能钱包收付款闭环与多上游协议（ZovoCard + 吱吱鼠）自动兑换系统架构与接口文档库。

## 目录索引

| 文档 | 说明 |
| :--- | :--- |
| [01-开源发卡站与架构选型](./docs/01-storefront-comparison.md) | `Dujiao-Next` vs `独角数卡(原版)` vs `异次元发卡` 深度对比、整体系统拓扑与服务器环境选型 |
| [02-OKX-Agentic-Wallet收付款方案](./docs/02-okx-agentic-wallet.md) | 欧易智能钱包 (`okx/onchainos-skills`) 收款（`a2a-pay` / `x402` + `BEpusdt` 双轨）与上游自动补款设计 |
| [03-ZovoCard沙箱与兑换站对接文档](./docs/03-zovocard-sandbox-and-cdk.md) | `zovocard.com` 沙箱/生产 OpenAPI 详解、资金风控约束（`50U` 起充 / `20U` 保证金）及 `zovo_card_cdk_auto` 改造点 |
| [04-协议多元化与故障转移规范](./docs/04-zzshu-fallback-protocol.md) | `card.zzshu.pro` 开放 API 完整规范（`orderType=direct` 自带卡直充）及「ZovoCard 失败自动切卡直充」状态机 |

## 核心开源仓库清单

1. **商城发卡前台与管理后台**：[`dujiao-next/dujiao-next`](https://github.com/dujiao-next/dujiao-next)（Go 1.26 + Gin + Vue 3 + PostgreSQL + Redis）
2. **CDK 自动兑换与卡台对接站**：[`zovocard/zovo_card_cdk_auto`](https://github.com/zovocard/zovo_card_cdk_auto)（Go + Gin + Vue 3 + PostgreSQL + Redis）
3. **OKX 智能钱包与支付协议工具集**：[`okx/onchainos-skills`](https://github.com/okx/onchainos-skills)（`@okxweb3/onchainos-installer` CLI / MCP / TEE 签名）
4. **链上通用扫码收款网关（配合 OKX 钱包地址）**：[`v03413/bepusdt`](https://github.com/v03413/bepusdt)（Go 编写，`Dujiao-Next` 已原生内置适配器）
