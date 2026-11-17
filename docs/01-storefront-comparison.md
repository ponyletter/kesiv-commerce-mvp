# 01. 开源发卡站选型与服务器环境规划

## 一、 开源发卡系统横向对比

针对“不从 0 造轮子、直接复用成熟开源方案”的目标，我们对主流开源发卡系统进行了源码级调研：

| 维度 | **Dujiao-Next（强烈推荐）** | 独角数卡原版 (`dujiaoka`) | 异次元发卡 (`acg-faka`) |
| :--- | :--- | :--- | :--- |
| **GitHub 仓库** | [`dujiao-next/dujiao-next`](https://github.com/dujiao-next/dujiao-next) | [`assimon/dujiaoka`](https://github.com/assimon/dujiaoka) | [`lizhipay/acg-faka`](https://github.com/lizhipay/acg-faka) |
| **维护状态** | **活跃维护**（2026-11 持续更新，1.8k+ Stars） | **已归档停更 (`Archived`)**，官方引导迁往 Dujiao-Next | 持续更新（5.9k+ Stars），核心开源 + 插件商城付费模式 |
| **后端技术栈** | **Go 1.26 + Gin + GORM** | PHP + Laravel + Dcat Admin | PHP 自研轻量内核 |
| **前端技术栈** | **Vue 3 + Vite + TypeScript + Tailwind v4** | Blade 模板（传统服务端渲染） | PHP 模板 + jQuery/Layui（偏向二次元视觉） |
| **数据库 / 队列** | **PostgreSQL / SQLite + Redis (`asynq`)** | MySQL + Redis + Supervisor | MySQL |
| **内置安全能力** | Casbin RBAC 权限控制、管理员 **TOTP 2FA**、可信代理校验 | 社区二开质量参差不齐，历史第三方支付插件曾有验签漏洞 | 部分高级插件闭源/加密，深度定制受限 |
| **内置支付网关** | 原生内置 `bepusdt`、`epusdt`、`tokenpay`、`okpay`、`epay`、`stripe`、`alipay`、`wechatpay` | 常见易支付、支付宝、微信，需额外装 USDT 插件 | 依赖插件商店安装各类支付插件 |
| **与兑换站协同性** | **100% 技术栈同构**（与 `zovo_card_cdk_auto` 同为 Go + Gin + Vue 3 + PG + Redis） | 异构（需额外维护 PHP + MySQL 环境） | 异构（需额外维护 PHP + MySQL 环境） |

### 选型结论
选择 **`Dujiao-Next` + `zovo_card_cdk_auto`** 组合：
1. **商城只管“卖码与收款”**：`Dujiao-Next` 原生支持商品分类、阶梯价、卡密库存管理（`cardsecret`）、订单风控（`orderrisk`）、上下游回调（`upstreamapi`）以及 Telegram/Google 登录。
2. **兑换站只管“核销与多通道代充”**：`zovo_card_cdk_auto` 专门负责接收买家的 CDK 与目标账号 Session，在后台调度 ZovoCard 与 `card.zzshu.pro`。
3. **共享基础设施**：两者共用同一个 PostgreSQL 实例（不同库名 `dujiao` / `cdk_recharge`）和同一个 Redis 实例（不同 DB index），内存占用极低，编译产物均为单个 Go 静态二进制文件。

---

## 二、 开发与部署服务器实测对比

结合实测硬件数据，对三台候选环境评估如下：

| 环境 | 实测配置与当前负载 | 适用定位 | 风险与注意事项 |
| :--- | :--- | :--- | :--- |
| **1. 本机 (`lubancat`)** | `aarch64` (ARM64) · **8 核 / 15 GiB 内存（12 GiB 可用）/ 115G 磁盘（95G 可用）** | **代码开发、文档整理、单元测试、交叉编译构建机** | ARM64 架构；无公网固定域名入口，不便直接接收外部 Webhook（OKX / ZovoCard 回调） |
| **2. 现有 VPS (`107.173.10.79`)** | `x86_64` · 2 核 / **1.9 GiB 内存（已用 1.4 GiB，仅剩 384 MiB 可用，Swap 已用 1.2 GiB）** / 50G 磁盘（剩 23G） | **维持现有旧版 Demo / `sub2api` 运行，不建议叠加新项目** | 内存严重饱和！在此机器上执行 `go build` 或 `pnpm build` 极易触发 OOM 导致现有容器崩溃 |
| **3. 软银服务器（待接入）** | `x86_64` · **2 核 / 4 GiB 内存 / 22G 硬盘** | **新方案沙箱联调环境 & 准生产运行主机（推荐）** | **22G 硬盘较小**：严禁在软银机上堆积 Docker 构建缓存和 `node_modules`；应在本地/GitHub Actions 编译好轻量镜像或单二进制后再部署 |

### 推荐协同工作流
- **开发与构建（本机 8C15G + GitHub Actions）**：在本机完成代码编写、配置接口适配器与 GitHub 提交；利用本机 15G 大内存或 GitHub Actions 编译 `linux/amd64` 二进制/精简镜像。
- **沙箱联调与运行（软银 2C4G22G）**：软银机器仅运行 Caddy + PostgreSQL + Redis + `Dujiao-Next` + `zovo_card_cdk_auto` + `BEpusdt` + `onchainos` 守护进程。整套 Go 服务运行时内存总占用不到 `1.2 GiB`，磁盘占用不到 `5 GB`，4G 内存 + 22G 硬盘完全游刃有余。
