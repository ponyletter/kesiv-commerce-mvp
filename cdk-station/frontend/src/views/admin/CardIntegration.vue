<template>
  <div class="space-y-4">
    <!-- 出口 IP -->
    <div class="card egress-hero">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div class="text-xs uppercase tracking-wide text-muted">本机出口 IP · 卡台白名单</div>
          <div class="mt-1 flex flex-wrap items-baseline gap-3">
            <span class="text-3xl font-bold mono text-ink">{{ egressIp || '…' }}</span>
            <el-tag v-if="egressIp" size="small" effect="dark" type="warning">填到卡台 API Key 白名单</el-tag>
          </div>
          <p class="text-xs text-subtle mt-2">发码/拉价格/余额从此 IP 出网（不是浏览器 IP）</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <el-button type="warning" plain :loading="enablingMock" @click="enableMockSandbox(false)">
            一键载入内置双通道联调沙箱
          </el-button>
          <el-button type="primary" :disabled="!egressIp" @click="copyText(egressIp)">复制 IP</el-button>
          <el-button :loading="loadingNet" @click="loadNetwork">重探测</el-button>
        </div>
      </div>
    </div>

    <!-- 1. ZovoCard 主通道配置 -->
    <div class="card space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 class="text-xl font-bold text-ink">主通道 · ZovoCard 卡台接入</h2>
          <p class="text-sm text-muted mt-1">Base 用站点根；Open API / CDK 路径自动拼接（支持生产、官方沙盒与内置模拟）</p>
        </div>
        <div class="flex gap-2">
          <el-tag v-if="hints.card_api_key_configured" type="success" effect="plain">Key 已存 {{ hints.card_api_key_hint }}</el-tag>
          <el-tag v-else type="info" effect="plain">Key 未配置</el-tag>
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <el-button round :type="presetActive === 'prod' ? 'primary' : 'default'" @click="applyPreset('prod')">
          生产 · zovocard.com
        </el-button>
        <el-button round :type="presetActive === 'sandbox' ? 'primary' : 'default'" @click="applyPreset('sandbox')">
          沙盒 · sandbox.zovocard.com
        </el-button>
        <el-button round :type="isMockMode ? 'warning' : 'default'" @click="enableMockSandbox(false)">
          内置模拟 · Mock Sandbox
        </el-button>
      </div>

      <div v-if="isMockMode" class="p-3 rounded-lg border border-amber-500/40 bg-amber-500/10 flex flex-wrap items-center justify-between gap-3">
        <div class="text-xs text-ink">
          <strong>内置联调模式已激活：</strong>开启右侧开关后，ZovoCard 兑换将模拟「网关拒付但虚拟卡尾号 8899 仍有 $35.50 余额」，自动触发下方 <code class="mono">card.zzshu.pro</code> 自带卡直充容灾！
        </div>
        <el-switch
          v-model="form.mock_simulate_zovo_fail"
          active-text="模拟主通道失败(测容灾)"
          inactive-text="模拟主通道正常"
          @change="save"
        />
      </div>

      <div class="path-chips">
        <div class="path-chip">
          <span class="k">Open API</span>
          <code class="v">{{ resolvedOpenapi }}</code>
        </div>
        <div class="path-chip">
          <span class="k">公开 CDK</span>
          <code class="v">{{ resolvedCdk }}</code>
        </div>
        <div class="path-chip">
          <span class="k">Webhook 回调</span>
          <code class="v">{{ webhookCallbackUrl }}</code>
        </div>
      </div>

      <el-form label-position="top" class="max-w-xl" @submit.prevent>
        <el-form-item label="卡台 Base URL">
          <el-input v-model="form.card_api_base" clearable size="large" @blur="normalizeBase" />
        </el-form-item>
        <el-form-item label="Open API Key (sk_…)">
          <el-input
            v-model="secrets.card_api_key"
            type="password"
            show-password
            clearable
            size="large"
            :placeholder="keyHint"
            autocomplete="off"
          />
        </el-form-item>
        <el-form-item label="Webhook 签名密钥 (whsec_… 可选)">
          <el-input
            v-model="secrets.webhook_secret"
            type="password"
            show-password
            clearable
            size="large"
            :placeholder="hints.webhook_secret_configured ? `已配置 ${hints.webhook_secret_hint || ''}` : '卡台开发者页 Webhook Secret'"
            autocomplete="off"
          />
        </el-form-item>
        <el-form-item label="代理换码密码">
          <el-input
            v-model="secrets.agent_swap_password"
            type="password"
            show-password
            clearable
            size="large"
            :placeholder="swapPwHint"
            autocomplete="off"
          />
          <p class="text-xs text-subtle mt-1">
            代理凭此密码进入隐藏页，将<strong>失败且未扣款</strong>的 CDK 换一张新码。留空保存=不修改；
            <el-tag v-if="hints.agent_swap_password_configured" size="small" type="success" effect="plain">已设置</el-tag>
            <el-tag v-else size="small" type="info" effect="plain">未设置</el-tag>
          </p>
        </el-form-item>
        <el-form-item label="发给代理的换码链接（可复制）">
          <div class="flex flex-wrap items-center gap-2 w-full">
            <el-input :model-value="agentSwapUrl" readonly size="large" class="!flex-1 mono" />
            <el-button type="primary" size="large" @click="copyText(agentSwapUrl)">复制链接</el-button>
          </div>
        </el-form-item>
        <div class="flex flex-wrap gap-2">
          <el-button type="primary" size="large" :loading="saving" @click="save">保存全部配置</el-button>
          <el-button type="success" size="large" plain :loading="busy" @click="runAllChecks">
            一键检测主通道
          </el-button>
          <el-button size="large" :loading="pinging" @click="ping">连通</el-button>
          <el-button size="large" :loading="loadingPlans" @click="loadPlans">价格</el-button>
          <el-button size="large" :loading="loadingBal" @click="loadBalance">余额</el-button>
        </div>
      </el-form>
    </div>

    <!-- 状态摘要卡片 -->
    <div class="grid gap-3 sm:grid-cols-3">
      <button type="button" class="status-card" @click="openStatusDialog">
        <div class="sc-label">ZovoCard 连通状态</div>
        <div class="sc-value" :class="pingOk === true ? 'ok' : pingOk === false ? 'bad' : ''">
          {{ pingOk === true ? '正常' : pingOk === false ? '异常' : '未检测' }}
        </div>
        <div class="sc-hint">点击查看详情</div>
      </button>
      <button type="button" class="status-card" @click="openBalanceDialog">
        <div class="sc-label">ZovoCard 可消费余额</div>
        <div class="sc-value mono">{{ spendableDisplay }}</div>
        <div class="sc-hint">扣除 20U 保证金后可用</div>
      </button>
      <button type="button" class="status-card" @click="pingZzshu">
        <div class="sc-label">备用通道 (zzshu) 剩余点数</div>
        <div class="sc-value mono" :class="zzshuPoints !== null ? 'ok' : ''">
          {{ zzshuPoints !== null ? `${zzshuPoints} 点` : '点击查询' }}
        </div>
        <div class="sc-hint">协议多元化自动容灾</div>
      </button>
    </div>

    <!-- 2. 协议多元化 · 备用直充通道 (card.zzshu.pro) -->
    <div class="card space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 class="text-xl font-bold text-ink">协议多元化 · 备用直充通道 (card.zzshu.pro)</h2>
          <p class="text-sm text-muted mt-1">
            当 ZovoCard 直充失败但名下虚拟卡内仍有余额时，自动提取该虚拟卡要素切换 <code class="mono">orderType=direct</code> 完成开通（成功才扣点）
          </p>
        </div>
        <div class="flex items-center gap-3">
          <el-tag v-if="hints.zzshu_api_key_configured" type="success" effect="plain">Key 已存 {{ hints.zzshu_api_key_hint }}</el-tag>
          <el-tag v-else type="info" effect="plain">Key 未配置</el-tag>
          <el-switch v-model="form.zzshu_enabled" active-text="启用备用通道" />
        </div>
      </div>

      <el-form label-position="top" class="max-w-xl" @submit.prevent>
        <div class="flex flex-wrap gap-2 mb-3">
          <el-button size="small" round @click="form.zzshu_api_base = 'https://card.zzshu.pro'">
            生产 · card.zzshu.pro
          </el-button>
        </div>
        <el-form-item label="备用通道 Base URL">
          <el-input v-model="form.zzshu_api_base" placeholder="https://card.zzshu.pro" size="large" clearable />
        </el-form-item>
        <el-form-item label="备用通道 X-API-Key（固定 API Key 或卡密）">
          <el-input
            v-model="secrets.zzshu_api_key"
            type="password"
            show-password
            clearable
            size="large"
            :placeholder="hints.zzshu_api_key_configured ? `已配置 ${hints.zzshu_api_key_hint || ''}` : '粘贴 card.zzshu.pro 的 API Key 或卡密'"
            autocomplete="off"
          />
        </el-form-item>
        <div class="grid gap-3 sm:grid-cols-2">
          <el-form-item label="触发容灾最低卡内余额 (USD)">
            <el-input v-model="form.zzshu_min_card_balance" placeholder="20" size="large" />
          </el-form-item>
          <el-form-item label="主通道失败自动无感切换">
            <el-switch v-model="form.zzshu_auto_failover" active-text="自动切换" inactive-text="仅手动" />
          </el-form-item>
        </div>
        <div class="flex flex-wrap gap-2">
          <el-button type="primary" :loading="saving" @click="save">保存备用通道配置</el-button>
          <el-button type="success" plain :loading="loadingZzshu" @click="pingZzshu">检测连通与剩余点数</el-button>
          <el-button :loading="loadingFailover" @click="loadFailoverData">刷新可容灾卡池与切换记录</el-button>
        </div>
      </el-form>

      <div v-if="fundedCards.length" class="text-xs text-muted">
        当前 ZovoCard 可用于容灾直充的有钱虚拟卡：
        <el-tag v-for="c in fundedCards" :key="c.id" size="small" type="success" class="mr-2">
          #{{ c.id }} (尾号 {{ c.last4 }} · ${{ c.balance_usd }})
        </el-tag>
      </div>

      <div v-if="failoverOrders.length" class="overflow-x-auto">
        <div class="text-xs font-semibold text-muted mb-2">最近自动容灾切换订单（ZovoCard 失败 -> card.zzshu.pro 接管）</div>
        <table class="w-full text-xs border-collapse">
          <thead>
            <tr class="border-b border-gray-500/20 text-left text-muted">
              <th class="py-2 pr-3">CDK</th>
              <th class="py-2 pr-3">复用卡尾号</th>
              <th class="py-2 pr-3">zzshu 单号 / Key</th>
              <th class="py-2 pr-3">状态</th>
              <th class="py-2 pr-3">触发原因</th>
              <th class="py-2">时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="fo in failoverOrders" :key="fo.id" class="border-b border-gray-500/10">
              <td class="py-2 pr-3 mono">{{ fo.cdk_code }}</td>
              <td class="py-2 pr-3 mono">{{ fo.card_last4 }} (${{ fo.card_balance_usd }})</td>
              <td class="py-2 pr-3 mono">{{ fo.zzshu_order_no }} / {{ fo.zzshu_card_key }}</td>
              <td class="py-2 pr-3">
                <el-tag size="small" :type="fo.status === 'success' ? 'success' : fo.status === 'failed' ? 'danger' : 'warning'">
                  {{ fo.status }}
                </el-tag>
              </td>
              <td class="py-2 pr-3 text-subtle">{{ fo.trigger_reason }}</td>
              <td class="py-2 mono text-subtle">{{ fo.updated_at }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 3. OKX Agentic Wallet 收付款与自动补款金库 -->
    <div class="card space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 class="text-xl font-bold text-ink">OKX Agentic Wallet · 智能钱包收付款与自动补款</h2>
          <p class="text-sm text-muted mt-1">
            收款地址用于商城 USDT 归集与 <code class="mono">a2a-pay</code> 支付链接；付款守护监控卡台 <code class="mono">spendable_balance</code> 自动向白名单地址补仓（强制 ≥50U）
          </p>
        </div>
        <div class="flex items-center gap-2">
          <el-tag :type="okxCli.logged_in ? 'success' : okxCli.installed ? 'warning' : 'info'" effect="plain">
            {{ okxCli.logged_in ? 'TEE 会话已激活' : okxCli.installed ? 'CLI 已装(待 login)' : '待安装 onchainos CLI' }}
          </el-tag>
          <el-switch v-model="form.okx_auto_topup_enabled" active-text="开启自动补款" />
        </div>
      </div>

      <el-form label-position="top" class="max-w-xl" @submit.prevent>
        <el-form-item label="OKX 收款地址 · EVM (X Layer 0-Gas / Arbitrum / BSC / Base)">
          <el-input v-model="form.okx_receive_evm" placeholder="0x..." size="large" clearable class="mono" />
        </el-form-item>
        <el-form-item label="OKX 收款地址 · TRON (TRC20 USDT)">
          <el-input v-model="form.okx_receive_trc20" placeholder="T..." size="large" clearable class="mono" />
        </el-form-item>
        <el-form-item label="ZovoCard 专属充值白名单地址（自动付款唯一锁定目标）">
          <el-input v-model="form.okx_zovocard_deposit_addr" placeholder="填入卡台网页端生成的专属 USDT 充值地址" size="large" clearable class="mono" />
        </el-form-item>
        <div class="grid gap-3 sm:grid-cols-4">
          <el-form-item label="打款公链">
            <el-select v-model="form.okx_zovocard_deposit_chain" size="large">
              <el-option label="TRON (TRC20)" value="tron" />
              <el-option label="X Layer" value="xlayer" />
              <el-option label="Arbitrum" value="arbitrum" />
              <el-option label="BSC" value="bsc" />
              <el-option label="Ethereum" value="ethereum" />
            </el-select>
          </el-form-item>
          <el-form-item label="触发水位 (USD)">
            <el-input v-model="form.okx_topup_trigger_usd" placeholder="30" size="large" />
          </el-form-item>
          <el-form-item label="单笔补款 (≥50U)">
            <el-input v-model="form.okx_topup_amount_usd" placeholder="50" size="large" />
          </el-form-item>
          <el-form-item label="单日上限 (USD)">
            <el-input v-model="form.okx_daily_limit_usd" placeholder="200" size="large" />
          </el-form-item>
        </div>
        <div class="flex flex-wrap gap-2">
          <el-button type="primary" :loading="saving" @click="save">保存 OKX 金库配置</el-button>
          <el-button type="warning" plain :loading="loadingOkx" @click="runOkxTopup(true)">
            演练检测补款 (Dry Run)
          </el-button>
          <el-button :loading="loadingOkx" @click="loadOkxStatus">刷新 CLI 状态与审计日志</el-button>
        </div>
      </el-form>

      <div v-if="okxLogs.length" class="overflow-x-auto">
        <div class="text-xs font-semibold text-muted mb-2">OKX 金库审计记录（今日已自动出金 ${{ okxCli.today_used_usd || 0 }}）</div>
        <table class="w-full text-xs border-collapse">
          <thead>
            <tr class="border-b border-gray-500/20 text-left text-muted">
              <th class="py-2 pr-3">动作</th>
              <th class="py-2 pr-3">公链</th>
              <th class="py-2 pr-3">金额</th>
              <th class="py-2 pr-3">状态</th>
              <th class="py-2 pr-3">说明</th>
              <th class="py-2">时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="lg in okxLogs" :key="lg.id" class="border-b border-gray-500/10">
              <td class="py-2 pr-3 mono">{{ lg.action }}</td>
              <td class="py-2 pr-3 uppercase">{{ lg.chain }}</td>
              <td class="py-2 pr-3 mono">${{ lg.amount_usd }}</td>
              <td class="py-2 pr-3">
                <el-tag size="small" :type="lg.status === 'success' || lg.status === 'simulated' ? 'success' : 'danger'">
                  {{ lg.status }}
                </el-tag>
              </td>
              <td class="py-2 pr-3 text-subtle">{{ lg.detail }}</td>
              <td class="py-2 mono text-subtle">{{ lg.created_at }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="flex flex-wrap gap-2">
      <router-link class="btn-primary" to="/ops/cdkeys">去发码</router-link>
      <router-link class="btn-secondary" to="/ops/webhooks">Webhook</router-link>
      <router-link class="btn-secondary" to="/ops/appearance">整站主题</router-link>
    </div>

    <!-- 连通详情弹窗 -->
    <el-dialog v-model="dlgStatus" title="连通检测" width="480px" align-center destroy-on-close>
      <el-result
        :icon="pingOk ? 'success' : 'error'"
        :title="pingOk ? '卡台可达' : '探测失败'"
        :sub-title="pingMsg || ''"
      />
      <div class="result-grid mt-2">
        <div v-for="(v, k) in pingTiles" :key="k" class="result-tile">
          <div class="k">{{ k }}</div>
          <div class="v mono">{{ v }}</div>
        </div>
      </div>
      <template #footer>
        <el-button @click="dlgStatus = false">关闭</el-button>
        <el-button type="primary" :loading="pinging" @click="ping">重新探测</el-button>
      </template>
    </el-dialog>

    <!-- 余额弹窗 -->
    <el-dialog v-model="dlgBal" title="账户余额" width="420px" align-center destroy-on-close>
      <div class="result-grid">
        <div class="result-tile">
          <div class="k">可消费 spendable</div>
          <div class="v mono text-lg">{{ bal.spendable ?? '—' }}</div>
        </div>
        <div class="result-tile">
          <div class="k">总余额 balance</div>
          <div class="v mono text-lg">{{ bal.balance ?? '—' }}</div>
        </div>
        <div class="result-tile">
          <div class="k">风险保证金 reserve</div>
          <div class="v mono text-lg">{{ bal.reserve ?? '—' }}</div>
        </div>
      </div>
      <p class="text-xs text-muted mt-3">主动消费只能用 spendable（总余额含 20U 保证金）</p>
      <template #footer>
        <el-button @click="dlgBal = false">关闭</el-button>
        <el-button type="primary" :loading="loadingBal" @click="loadBalance">刷新</el-button>
      </template>
    </el-dialog>

    <!-- 价格弹窗 -->
    <el-dialog v-model="dlgPlans" title="实时服务费" width="520px" align-center destroy-on-close>
      <p class="text-sm text-muted mb-3">
        GET /openapi/v1/gpt-direct/plans · version {{ plansVersion ?? '—' }}
      </p>
      <div class="grid gap-3 sm:grid-cols-3">
        <div v-for="p in planCards" :key="p.key" class="result-tile text-center">
          <div class="k">{{ p.label }}</div>
          <div class="v mono text-2xl" style="color: var(--primary)">${{ p.fee }}</div>
          <div class="text-xs text-subtle mt-1">/ 张 · 可购</div>
          <div v-if="p.checkout" class="text-xs text-subtle">兑换垫付 {{ p.checkout }}</div>
          <div v-if="p.minor != null" class="text-xs text-subtle">minor {{ p.minor }}</div>
        </div>
      </div>
      <el-empty v-if="!planCards.length" description="卡台未开放任何可发码档位" :image-size="60" />
      <template #footer>
        <el-button @click="dlgPlans = false">关闭</el-button>
        <el-button type="primary" :loading="loadingPlans" @click="loadPlans">刷新价格</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { authFetch } from '../../lib/api'
import { dialog } from '../../lib/dialog'

const PRESETS: Record<string, string> = {
  prod: 'https://zovocard.com',
  sandbox: 'https://sandbox.zovocard.com',
}

const form = reactive({
  card_api_base: PRESETS.sandbox,
  zzshu_enabled: true,
  zzshu_auto_failover: true,
  zzshu_api_base: 'https://card.zzshu.pro',
  zzshu_min_card_balance: '20',
  mock_simulate_zovo_fail: false,
  okx_receive_trc20: '',
  okx_receive_evm: '',
  okx_receive_solana: '',
  okx_auto_topup_enabled: false,
  okx_zovocard_deposit_addr: '',
  okx_zovocard_deposit_chain: 'tron',
  okx_topup_trigger_usd: '30',
  okx_topup_amount_usd: '50',
  okx_daily_limit_usd: '200',
})

const secrets = reactive({
  card_api_key: '',
  webhook_secret: '',
  agent_swap_password: '',
  zzshu_api_key: '',
})
const hints = reactive<Record<string, any>>({})
const saving = ref(false)
const busy = ref(false)
const pinging = ref(false)
const loadingPlans = ref(false)
const loadingBal = ref(false)
const loadingNet = ref(false)
const loadingZzshu = ref(false)
const loadingFailover = ref(false)
const loadingOkx = ref(false)
const enablingMock = ref(false)

const egressIp = ref('')
const pingOk = ref<boolean | null>(null)
const pingMsg = ref('')
const pingTiles = ref<Record<string, string>>({})
const bal = reactive<{ spendable?: string; balance?: string; reserve?: string }>({})
const plansRaw = ref<Record<string, any>>({})
const plansVersion = ref<number | null>(null)

const zzshuPoints = ref<number | null>(null)
const fundedCards = ref<any[]>([])
const failoverOrders = ref<any[]>([])
const okxCli = reactive<Record<string, any>>({ installed: false, logged_in: false, today_used_usd: 0 })
const okxLogs = ref<any[]>([])

const dlgStatus = ref(false)
const dlgBal = ref(false)
const dlgPlans = ref(false)

const keyHint = computed(() =>
  hints.card_api_key_configured ? `已配置 ${hints.card_api_key_hint || ''}`.trim() : '粘贴 sk_…',
)
const swapPwHint = computed(() =>
  hints.agent_swap_password_configured ? '已设置（留空保存不修改）' : '设置代理换码密码（至少 6 位）',
)
const agentSwapUrl = computed(() => {
  if (typeof window === 'undefined') return '/partner/swap'
  return `${window.location.origin}/partner/swap`
})
const webhookCallbackUrl = computed(() => {
  if (typeof window === 'undefined') return '/api/v1/webhooks/zovocard'
  return `${window.location.origin}/api/v1/webhooks/zovocard`
})

const siteRoot = computed(() => {
  let b = (form.card_api_base || '').trim().replace(/\/+$/, '')
  b = b.replace(/\/openapi\/v1$/i, '').replace(/\/openapi$/i, '')
  return b || PRESETS.sandbox
})
const resolvedOpenapi = computed(() => siteRoot.value + '/openapi/v1')
const resolvedCdk = computed(() => siteRoot.value + '/api/v1/cdk')
const isMockMode = computed(() => siteRoot.value.includes('/mock-sandbox/zovocard'))
const presetActive = computed(() => {
  const b = siteRoot.value.toLowerCase()
  if (b === PRESETS.prod) return 'prod'
  if (b === PRESETS.sandbox) return 'sandbox'
  return ''
})

const planRegistry = ref<any[]>([])
const planCards = computed(() =>
  planRegistry.value.map((p: any) => {
    const minor = p.serviceFeeUsdMinor ?? p.service_fee_usd_minor
    const fee =
      minor != null && minor !== ''
        ? (Number(minor) / 100).toFixed(2)
        : p.service_fee_usd != null
          ? Number(p.service_fee_usd).toFixed(2)
          : '—'
    const checkout = p.checkout_amount_minor
      ? `${p.checkout_currency || 'PHP'} ${(Number(p.checkout_amount_minor) / 100).toFixed(2)}`
      : ''
    return { key: p.key, label: p.label || p.key, fee, minor, checkout, enabled: true }
  }),
)
const spendableDisplay = computed(() => bal.spendable ?? '—')

function applyPreset(id: string) {
  form.card_api_base = PRESETS[id] || PRESETS.sandbox
}
function normalizeBase() {
  form.card_api_base = siteRoot.value
}
async function copyText(t: string) {
  try {
    await navigator.clipboard.writeText(t)
    dialog.toast('已复制', 'ok')
  } catch {
    dialog.toast('复制失败', 'err')
  }
}

async function loadNetwork() {
  loadingNet.value = true
  try {
    const r = await authFetch('/api/v1/admin/network/egress')
    const d = await r.json().catch(() => ({}))
    egressIp.value = d.egress_ip || ''
  } finally {
    loadingNet.value = false
  }
}

const asBool = (v: any, def = false) => {
  if (v === undefined || v === null || v === '') return def
  return String(v).toLowerCase() === 'true' || String(v) === '1'
}

async function loadSettings() {
  const r = await authFetch('/api/v1/admin/settings')
  if (!r.ok) return
  const d = await r.json()
  form.card_api_base = d.card_api_base || PRESETS.sandbox
  form.zzshu_enabled = asBool(d.zzshu_enabled, true)
  form.zzshu_auto_failover = asBool(d.zzshu_auto_failover, true)
  form.zzshu_api_base = d.zzshu_api_base || 'https://card.zzshu.pro'
  form.zzshu_min_card_balance = d.zzshu_min_card_balance || '20'
  form.mock_simulate_zovo_fail = asBool(d.mock_simulate_zovo_fail, false)
  form.okx_receive_trc20 = d.okx_receive_trc20 || ''
  form.okx_receive_evm = d.okx_receive_evm || ''
  form.okx_receive_solana = d.okx_receive_solana || ''
  form.okx_auto_topup_enabled = asBool(d.okx_auto_topup_enabled, false)
  form.okx_zovocard_deposit_addr = d.okx_zovocard_deposit_addr || ''
  form.okx_zovocard_deposit_chain = d.okx_zovocard_deposit_chain || 'tron'
  form.okx_topup_trigger_usd = d.okx_topup_trigger_usd || '30'
  form.okx_topup_amount_usd = d.okx_topup_amount_usd || '50'
  form.okx_daily_limit_usd = d.okx_daily_limit_usd || '200'
  Object.assign(hints, d)
  normalizeBase()
}

async function save() {
  saving.value = true
  try {
    normalizeBase()
    if (Number(form.okx_topup_amount_usd || 50) < 50) {
      dialog.toast('ZovoCard 链上充值最低 50U，已自动修正为 50', 'warn')
      form.okx_topup_amount_usd = '50'
    }
    const body: Record<string, string> = {
      card_api_base: form.card_api_base.trim(),
      zzshu_enabled: String(form.zzshu_enabled),
      zzshu_auto_failover: String(form.zzshu_auto_failover),
      zzshu_api_base: form.zzshu_api_base.trim(),
      zzshu_min_card_balance: String(form.zzshu_min_card_balance || '20').trim(),
      mock_simulate_zovo_fail: String(form.mock_simulate_zovo_fail),
      okx_receive_trc20: form.okx_receive_trc20.trim(),
      okx_receive_evm: form.okx_receive_evm.trim(),
      okx_receive_solana: form.okx_receive_solana.trim(),
      okx_auto_topup_enabled: String(form.okx_auto_topup_enabled),
      okx_zovocard_deposit_addr: form.okx_zovocard_deposit_addr.trim(),
      okx_zovocard_deposit_chain: form.okx_zovocard_deposit_chain,
      okx_topup_trigger_usd: String(form.okx_topup_trigger_usd || '30').trim(),
      okx_topup_amount_usd: String(form.okx_topup_amount_usd || '50').trim(),
      okx_daily_limit_usd: String(form.okx_daily_limit_usd || '200').trim(),
    }
    if (secrets.card_api_key.trim()) body.card_api_key = secrets.card_api_key.trim()
    if (secrets.webhook_secret.trim()) body.webhook_secret = secrets.webhook_secret.trim()
    if (secrets.agent_swap_password.trim()) body.agent_swap_password = secrets.agent_swap_password.trim()
    if (secrets.zzshu_api_key.trim()) body.zzshu_api_key = secrets.zzshu_api_key.trim()

    const r = await authFetch('/api/v1/admin/settings', { method: 'PUT', body: JSON.stringify(body) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return false
    }
    Object.assign(hints, d)
    secrets.card_api_key = ''
    secrets.webhook_secret = ''
    secrets.agent_swap_password = ''
    secrets.zzshu_api_key = ''
    dialog.toast('配置已保存', 'ok')
    return true
  } finally {
    saving.value = false
  }
}

async function enableMockSandbox(simFail = false) {
  enablingMock.value = true
  try {
    const r = await authFetch('/api/v1/admin/multiprotocol/enable-mock', {
      method: 'POST',
      body: JSON.stringify({ port: 8081, simulate_zovo_fail: simFail }),
    })
    if (!r.ok) {
      dialog.toast('启用内置联调沙箱失败', 'err')
      return
    }
    await loadSettings()
    await ping()
    await loadBalance()
    await pingZzshu()
    await loadFailoverData()
    dialog.toast('已载入内置双通道联调沙箱！可直接去发码和体验容灾切换', 'ok')
  } finally {
    enablingMock.value = false
  }
}

async function pingZzshu() {
  loadingZzshu.value = true
  try {
    await save()
    const r = await authFetch('/api/v1/admin/multiprotocol/zzshu/ping')
    const d = await r.json().catch(() => ({}))
    if (!r.ok || !d.ok) {
      dialog.toast(d.error || '备用通道探测失败', 'err')
      return
    }
    zzshuPoints.value = Number(d.points ?? 0)
    dialog.toast(`备用通道连通正常！剩余点数: ${zzshuPoints.value}`, 'ok')
  } finally {
    loadingZzshu.value = false
  }
}

async function loadFailoverData() {
  loadingFailover.value = true
  try {
    const [r1, r2] = await Promise.all([
      authFetch('/api/v1/admin/multiprotocol/failover-orders'),
      authFetch('/api/v1/admin/multiprotocol/funded-cards'),
    ])
    if (r1.ok) {
      const d1 = await r1.json().catch(() => ({}))
      failoverOrders.value = d1.list || []
    }
    if (r2.ok) {
      const d2 = await r2.json().catch(() => ({}))
      fundedCards.value = d2.list || []
    }
  } finally {
    loadingFailover.value = false
  }
}

async function loadOkxStatus() {
  loadingOkx.value = true
  try {
    const r = await authFetch('/api/v1/admin/okx/status')
    if (!r.ok) return
    const d = await r.json().catch(() => ({}))
    if (d.cli) Object.assign(okxCli, d.cli)
    okxLogs.value = d.logs || []
  } finally {
    loadingOkx.value = false
  }
}

async function runOkxTopup(dryRun: boolean) {
  loadingOkx.value = true
  try {
    await save()
    const r = await authFetch('/api/v1/admin/okx/topup', {
      method: 'POST',
      body: JSON.stringify({ dry_run: dryRun }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok || d.ok === false) {
      dialog.toast(d.error || '补款检测未通过', 'err')
    } else {
      dialog.toast(d.message || '补款指令已执行', 'ok')
    }
    await loadOkxStatus()
  } finally {
    loadingOkx.value = false
  }
}

async function ping() {
  pinging.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/ping')
    const d = await r.json().catch(() => ({}))
    pingOk.value = !!r.ok && !!d.ok
    pingMsg.value = d.message || d.error || ''
    if (d.egress_ip) egressIp.value = d.egress_ip
    pingTiles.value = {
      site: d.site_base || siteRoot.value,
      openapi: d.openapi_base || resolvedOpenapi.value,
      cdk: d.public_cdk_base || resolvedCdk.value,
      probed: d.probed || '—',
      http: String(d.status ?? '—'),
      egress: d.egress_ip || egressIp.value || '—',
    }
    if (d.status === 403) dialog.toast('403：请将出口 IP 加入白名单', 'warn')
    else if (pingOk.value) dialog.toast('连通正常', 'ok')
    else dialog.toast(pingMsg.value || '探测失败', 'err')
  } finally {
    pinging.value = false
  }
}

async function loadPlans() {
  loadingPlans.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/plans')
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '价格拉取失败', 'err')
      return
    }
    plansRaw.value = d.plans || {}
    planRegistry.value = d.registry || []
    plansVersion.value = d.version ?? null
    dialog.toast('价格已更新', 'ok')
    dlgPlans.value = true
  } finally {
    loadingPlans.value = false
  }
}

async function loadBalance() {
  loadingBal.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/balance')
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '余额失败', 'err')
      return
    }
    bal.spendable = String(d.spendable_balance ?? '—')
    bal.balance = String(d.balance ?? '—')
    bal.reserve = String(d.account_reserve_amount ?? '—')
    dialog.toast('余额已刷新', 'ok')
    dlgBal.value = true
  } finally {
    loadingBal.value = false
  }
}

async function runAllChecks() {
  busy.value = true
  try {
    await save()
    await ping()
    await loadBalance()
    await loadPlans()
    dlgStatus.value = true
  } finally {
    busy.value = false
  }
}

function openStatusDialog() {
  dlgStatus.value = true
  if (pingOk.value === null) ping()
}
function openBalanceDialog() {
  dlgBal.value = true
  if (bal.spendable == null) loadBalance()
}

onMounted(async () => {
  await loadSettings()
  await loadNetwork()
  await loadFailoverData()
  await loadOkxStatus()
})
</script>

<style scoped>
.egress-hero {
  background: linear-gradient(120deg, var(--primary-soft), transparent 55%);
  border-color: var(--brd-2);
}
.path-chips { display: flex; flex-direction: column; gap: 8px; }
.path-chip {
  display: flex; flex-wrap: wrap; align-items: center; gap: 10px;
  padding: 10px 12px; border-radius: var(--radius-md);
  background: var(--surface-2); border: 1px solid var(--brd);
}
.path-chip .k {
  font-size: 11px; font-weight: 600; color: var(--ink-3);
  text-transform: uppercase; letter-spacing: .04em; min-width: 72px;
}
.path-chip .v { font-size: 12px; color: var(--ink); word-break: break-all; }

.status-card {
  text-align: left;
  padding: 16px;
  border-radius: var(--radius-lg);
  border: 1px solid var(--brd);
  background: var(--surface);
  cursor: pointer;
  transition: .18s ease;
}
.status-card:hover {
  border-color: var(--primary);
  box-shadow: var(--shadow-sm);
  transform: translateY(-2px);
}
.sc-label { font-size: 12px; color: var(--ink-3); }
.sc-value { margin-top: 6px; font-size: 20px; font-weight: 700; color: var(--ink); }
.sc-value.ok { color: var(--good); }
.sc-value.bad { color: var(--err); }
.sc-hint { margin-top: 6px; font-size: 11px; color: var(--ink-3); }
.mono { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
</style>
