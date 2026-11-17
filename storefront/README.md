# Storefront & Payment Gateway Integration (`Dujiao-Next` + `BEpusdt` + `OKX Agentic Wallet`)

本目录存放商城前台（[`dujiao-next/dujiao-next`](https://github.com/dujiao-next/dujiao-next)）与多链收款网关（[`v03413/bepusdt`](https://github.com/v03413/bepusdt)）的标准配置模板及反代规则。

## 组件职责

1. **发卡与营销商城 (`Dujiao-Next` v1.5.3+)**
   - 负责商品展示、阶梯批发价、优惠券 (`coupon`)、推广返佣 (`affiliate`)、Telegram 机器人成交通知、管理员 TOTP 2FA 及自动发放 CDK 激活码。
   - 买家付款成功后立即获得 CDK 码，引导前往独立兑换站核销，实现**售前收款与售后履约彻底解耦**。

2. **收款网关层 (`BEpusdt` v1.24.4+ & `OKX Agentic Wallet`)**
   - **扫码直入智能钱包**：在 `BEpusdt` 后台添加你的 **OKX Agentic Wallet** 多链收款地址（支持 TRON TRC20、X Layer 0-Gas、Arbitrum、BSC、Polygon、Solana），无需在服务器存放任何私钥。
   - **OKX 原生支付链接 (`a2a-pay` / `x402`)**：由兑换站/金库模块内置的 `okxwallet` 服务调用 `onchainos payment a2a-pay create` 生成支付挑战链接。
   - **法币收款（可选开关）**：`Dujiao-Next` 后台原生支持易支付 (`epay`)、Stripe、PayPal 等适配器，可按推广渠道随时启用。
