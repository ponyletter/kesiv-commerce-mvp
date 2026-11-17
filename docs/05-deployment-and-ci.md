# 05. 轻量单二进制部署与 GitHub Actions CI/CD 指南

## 一、 为什么采用「单二进制 + Systemd + Caddy」而非嵌套 Docker？

在 LXC 容器集群（如独立分配的 `c5` 容器）中，由于 `Dujiao-Next`、`cdk-station`（基于 `zovo_card_cdk_auto` 增强）与 `BEpusdt` **全部为 Go 语言编译产物**：
- 直接运行静态/精简二进制 + `systemd` 守护，**整套系统总磁盘占用不足 300 MB，常驻内存不足 400 MB**；
- 彻底避免 Docker-in-LXD 带来的 `runc` / AppArmor 兼容性问题与数十 GB 的构建缓存膨胀；
- 配合 GitHub Actions（[`.github/workflows/ci-build.yml`](../.github/workflows/ci-build.yml)）自动测试并产物打包，服务器侧只需拉取编译产物并 `systemctl restart` 即可秒级更新。

---

## 二、 容器内标准目录与端口规划

| 服务组件 | 运行目录 | 内部监听端口 | 外部 Cloudflare 域名示例 |
| :--- | :--- | :--- | :--- |
| **Caddy 统一网关** | `/etc/caddy/Caddyfile` | `2087` (CF 官方 HTTPS 回源端口) | 统一按 `Host` 分发下列三个站点 |
| **Dujiao-Next 商城** | `/opt/kesiv-mvp/dujiao` | `127.0.0.1:18080` | `https://mall-mvp.<your-domain>` |
| **CDK 多协议兑换站** | `/opt/kesiv-mvp/cdk` | `127.0.0.1:18081` | `https://cdk-mvp.<your-domain>` |
| **BEpusdt 收款网关** | `/opt/kesiv-mvp/bepusdt` | `127.0.0.1:18082` | `https://pay-mvp.<your-domain>` |

---

## 三、 后台配置与沙箱验证流程

1. 登录 **CDK 多协议兑换站后台** (`/ops/integration`)：
   - **零密钥快速体验**：点击右上角 **「一键载入内置双通道联调沙箱」**：
     - 保持「模拟主通道正常」：前往「发码管理」生成测试 CDK，在前台输入 CDK + 测试 ChatGPT Session，验证主通道 ZovoCard 正常核销流程。
     - 开启 **「模拟主通道失败(测容灾)」**：再次用新 CDK 兑换，观察主通道返回 `declined` 后，系统自动读取名下余额 `$35.50` 的虚拟卡尾号 `8899`，无缝切换 `card.zzshu.pro` (`orderType=direct`) 完成开通，并在后台表格生成容灾记录！
   - **切换真实沙箱 / 生产**：
     - 点击 `沙盒 · sandbox.zovocard.com`（或 `生产 · zovocard.com`），填入 `sk_...` 密钥，并将页面顶部显示的本机出口 IP 加入 ZovoCard 白名单。
     - 在备用通道填入 `https://card.zzshu.pro` 与你的固定 `X-API-Key`，点击「检测连通与剩余点数」。
     - 在 OKX 金库卡片填入你的 OKX 智能钱包收款地址及 ZovoCard 专属充值白名单地址（如需开启无人值守补款，在服务器终端执行一次 `onchainos wallet login` 完成邮箱 OTP 绑定）。
2. 登录 **Dujiao-Next 商城后台** (`/admin`)：
   - 在「支付配置」启用 `BEpusdt`（指向 `http://127.0.0.1:18082` 或 `https://pay-mvp.<your-domain>`）及可选易支付渠道。
   - 创建商品并导入从兑换站批量生成的 CDK 卡密，开启推广返佣 (`Affiliate`) 与优惠码即可对外推广变现。
