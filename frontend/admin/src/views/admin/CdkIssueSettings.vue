<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminCdkIssueConfig, AdminCdkIssueOverview } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { notifyError, notifySuccess } from '@/utils/notify'
import { formatDate } from '@/utils/format'
import { cdkIssueRegionCodes, formatUsd, formatUsdMinor } from '@/utils/cdkIssue'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const overview = ref<AdminCdkIssueOverview | null>(null)
const overviewError = ref('')

const saved = reactive({
  active: false,
  queue_enabled: true,
  has_secret: false,
  secret_hint: '',
})

const form = reactive({
  enabled: false,
  environment: 'sandbox' as 'sandbox' | 'production',
  app_id: '',
  app_secret: '',
  default_payment_country: 'PH',
  min_spendable_balance: 50 as number | string,
})

const baseUrl = computed(() =>
  form.environment === 'production'
    ? 'https://zovocard.com/openapi/v1'
    : 'https://sandbox.zovocard.com/openapi/v1',
)

const regionOptions = computed(() => cdkIssueRegionCodes(overview.value?.payment_regions))

const spendableBelowThreshold = computed(() => {
  if (!overview.value) return false
  return Number(overview.value.balance.spendable_balance) < Number(form.min_spendable_balance || 0)
})

const applyConfig = (data: AdminCdkIssueConfig) => {
  form.enabled = Boolean(data.enabled)
  form.environment = data.environment === 'production' ? 'production' : 'sandbox'
  form.app_id = data.app_id || ''
  form.app_secret = ''
  form.default_payment_country = data.default_payment_country || 'PH'
  form.min_spendable_balance = Number(data.min_spendable_balance ?? 50)
  saved.active = Boolean(data.active)
  saved.queue_enabled = data.queue_enabled !== false
  saved.has_secret = Boolean(data.has_secret)
  saved.secret_hint = data.secret_hint || ''
}

const buildPayload = () => {
  const payload: Record<string, unknown> = {
    enabled: form.enabled,
    environment: form.environment,
    app_id: form.app_id.trim(),
    default_payment_country: form.default_payment_country,
    min_spendable_balance: Number(form.min_spendable_balance) || 0,
  }
  if (form.app_secret.trim()) payload.app_secret = form.app_secret.trim()
  return payload
}

const errorMessage = (err: any, fallback: string) => err?.response?.data?.message || err?.message || fallback

const loadOverview = async (refresh = false) => {
  if (!saved.has_secret || !form.app_id) {
    overview.value = null
    return
  }
  overviewError.value = ''
  try {
    const res = await adminAPI.getCdkIssueOverview(refresh)
    overview.value = res.data?.data as AdminCdkIssueOverview
  } catch (err: any) {
    overview.value = null
    overviewError.value = errorMessage(err, t('admin.cdkIssue.settings.overviewFailed'))
  }
}

const fetchConfig = async () => {
  loading.value = true
  try {
    const res = await adminAPI.getCdkIssueConfig()
    applyConfig(res.data?.data as AdminCdkIssueConfig)
    await loadOverview()
  } catch (err: any) {
    notifyError(errorMessage(err, t('admin.settings.alerts.loadFailed')))
  } finally {
    loading.value = false
  }
}

const saveConfig = async () => {
  saving.value = true
  try {
    const res = await adminAPI.updateCdkIssueConfig(buildPayload())
    applyConfig(res.data?.data as AdminCdkIssueConfig)
    notifySuccess(t('admin.settings.alerts.saveSuccess'))
    await loadOverview(true)
  } catch (err: any) {
    notifyError(errorMessage(err, t('admin.settings.alerts.saveFailed')))
  } finally {
    saving.value = false
  }
}

// 用表单当前值测试，无需先保存。
const testConnection = async () => {
  testing.value = true
  overviewError.value = ''
  try {
    const res = await adminAPI.testCdkIssueConfig(buildPayload())
    overview.value = res.data?.data as AdminCdkIssueOverview
    notifySuccess(t('admin.cdkIssue.settings.testSuccess'))
  } catch (err: any) {
    overview.value = null
    overviewError.value = errorMessage(err, t('admin.cdkIssue.settings.testFailed'))
    notifyError(overviewError.value)
  } finally {
    testing.value = false
  }
}

onMounted(fetchConfig)
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('admin.cdkIssue.settings.title') }}</h1>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('admin.cdkIssue.settings.subtitle') }}</p>
      </div>
      <div class="flex flex-col gap-2 sm:flex-row">
        <Button size="sm" variant="outline" class="w-full sm:w-auto" :disabled="loading || testing" @click="testConnection">
          {{ testing ? t('admin.cdkIssue.settings.testing') : t('admin.cdkIssue.settings.test') }}
        </Button>
        <Button size="sm" class="w-full sm:w-auto" :disabled="loading || saving" @click="saveConfig">
          {{ saving ? t('admin.settings.actions.saving') : t('admin.settings.actions.save') }}
        </Button>
      </div>
    </div>

    <div class="rounded-xl border border-border bg-card">
      <div class="space-y-6 p-6">
        <div class="flex flex-wrap items-center gap-3 rounded-lg border border-border bg-muted/30 px-4 py-3 text-sm">
          <span class="text-muted-foreground">{{ t('admin.cdkIssue.settings.statusLabel') }}：</span>
          <span :class="saved.active ? 'text-emerald-600' : 'text-muted-foreground'">
            ● {{ saved.active ? t('admin.cdkIssue.settings.statusActive') : t('admin.cdkIssue.settings.statusInactive') }}
          </span>
          <span class="text-muted-foreground">{{ t('admin.cdkIssue.settings.environment') }}：</span>
          <span
            class="rounded-full border px-2 py-0.5 text-xs"
            :class="form.environment === 'production' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-amber-200 bg-amber-50 text-amber-700'"
          >
            {{ form.environment === 'production' ? t('admin.cdkIssue.settings.envProduction') : t('admin.cdkIssue.settings.envSandbox') }}
          </span>
        </div>

        <div v-if="!saved.queue_enabled" class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          <p class="font-medium">{{ t('admin.cdkIssue.settings.queueDisabledTitle') }}</p>
          <p class="mt-1 text-xs">{{ t('admin.cdkIssue.settings.queueDisabledHint') }}</p>
        </div>

        <div class="rounded-lg border border-amber-500/30 bg-amber-500/5 p-4">
          <p class="text-sm font-medium text-amber-700 dark:text-amber-400">{{ t('admin.cdkIssue.settings.securityTitle') }}</p>
          <p class="mt-1 text-xs text-amber-600 dark:text-amber-400">{{ t('admin.cdkIssue.settings.securityHint') }}</p>
        </div>

        <div class="grid grid-cols-1 gap-6 md:grid-cols-2">
          <div class="space-y-2 md:col-span-2">
            <div class="inline-flex items-center gap-2">
              <Switch id="cdk-issue-enabled" v-model="form.enabled" :disabled="loading || saving" />
              <Label for="cdk-issue-enabled" class="text-sm">{{ t('admin.cdkIssue.settings.enabled') }}</Label>
            </div>
            <p class="text-xs text-muted-foreground">{{ t('admin.cdkIssue.settings.enabledHint') }}</p>
          </div>

          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.settings.environment') }}</label>
            <div class="flex gap-2">
              <button
                v-for="env in (['sandbox', 'production'] as const)"
                :key="env"
                type="button"
                class="flex-1 rounded-lg border px-3 py-2 text-sm transition-colors"
                :class="form.environment === env ? 'border-primary bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:border-primary/40'"
                :disabled="loading || saving"
                @click="form.environment = env"
              >
                {{ env === 'production' ? t('admin.cdkIssue.settings.envProduction') : t('admin.cdkIssue.settings.envSandbox') }}
              </button>
            </div>
            <p class="text-xs text-muted-foreground">{{ t('admin.cdkIssue.settings.environmentHint') }}</p>
          </div>

          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.settings.baseUrl') }}</label>
            <Input :model-value="baseUrl" readonly class="font-mono text-xs" />
          </div>

          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">App ID</label>
            <Input v-model="form.app_id" placeholder="ak_xxxxxxxxxxxx" :disabled="loading || saving" />
          </div>

          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">App Secret</label>
            <Input
              v-model="form.app_secret"
              type="password"
              autocomplete="new-password"
              :disabled="loading || saving"
              :placeholder="saved.has_secret ? t('admin.cdkIssue.settings.secretKeepPlaceholder') : 'sk_xxxxxxxxxxxx'"
            />
            <p class="text-xs text-muted-foreground">
              {{ saved.has_secret ? t('admin.cdkIssue.settings.secretSaved', { hint: saved.secret_hint }) : t('admin.cdkIssue.settings.secretHint') }}
            </p>
          </div>

          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.settings.defaultCountry') }}</label>
            <Select v-model="form.default_payment_country" :disabled="loading || saving">
              <SelectTrigger class="h-9 w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="code in regionOptions" :key="code" :value="code">
                  {{ code }} · {{ t(`admin.cdkIssue.countries.${code}`, code) }}
                </SelectItem>
                <SelectItem v-if="!regionOptions.includes(form.default_payment_country)" :value="form.default_payment_country">
                  {{ form.default_payment_country }}
                </SelectItem>
              </SelectContent>
            </Select>
            <p class="text-xs text-muted-foreground">{{ t('admin.cdkIssue.settings.defaultCountryHint') }}</p>
          </div>

          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.settings.minBalance') }}</label>
            <Input v-model.number="form.min_spendable_balance" type="number" min="0" step="0.01" :disabled="loading || saving" />
            <p class="text-xs text-muted-foreground">{{ t('admin.cdkIssue.settings.minBalanceHint') }}</p>
          </div>
        </div>
      </div>
    </div>

    <div class="rounded-xl border border-border bg-card">
      <div class="space-y-5 p-6">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h2 class="text-base font-semibold">{{ t('admin.cdkIssue.settings.overviewTitle') }}</h2>
          <span v-if="overview" class="text-xs text-muted-foreground">
            {{ t('admin.cdkIssue.settings.checkedAt', { time: formatDate(overview.checked_at) }) }}
          </span>
        </div>

        <div v-if="overviewError" class="rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-700">{{ overviewError }}</div>
        <div v-else-if="!overview" class="rounded-lg border border-dashed border-border p-4 text-xs text-muted-foreground">
          {{ t('admin.cdkIssue.settings.overviewEmpty') }}
        </div>

        <template v-if="overview">
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div class="rounded-lg border border-border bg-muted/20 p-4">
              <div class="text-xs text-muted-foreground">{{ t('admin.cdkIssue.settings.balance') }}</div>
              <div class="mt-1 text-xl font-semibold">{{ formatUsd(overview.balance.balance) }} U</div>
            </div>
            <div
              class="rounded-lg border p-4"
              :class="spendableBelowThreshold ? 'border-red-200 bg-red-50/60' : 'border-border bg-muted/20'"
            >
              <div class="text-xs" :class="spendableBelowThreshold ? 'text-red-700' : 'text-muted-foreground'">{{ t('admin.cdkIssue.settings.spendable') }}</div>
              <div class="mt-1 text-xl font-semibold" :class="spendableBelowThreshold ? 'text-red-700' : ''">{{ formatUsd(overview.balance.spendable_balance) }} U</div>
              <div v-if="spendableBelowThreshold" class="mt-1 text-xs text-red-700">{{ t('admin.cdkIssue.settings.belowThreshold') }}</div>
            </div>
            <div class="rounded-lg border border-border bg-muted/20 p-4">
              <div class="text-xs text-muted-foreground">{{ t('admin.cdkIssue.settings.reserve') }}</div>
              <div class="mt-1 text-xl font-semibold">{{ formatUsd(overview.balance.account_reserve_amount) }} U</div>
            </div>
          </div>

          <div>
            <h3 class="mb-2 text-sm font-medium">{{ t('admin.cdkIssue.settings.plansTitle') }}</h3>
            <div class="overflow-x-auto rounded-lg border border-border">
              <Table class="min-w-[560px]">
                <TableHeader class="bg-muted/40 text-xs text-muted-foreground">
                  <TableRow>
                    <TableHead class="px-4 py-2">{{ t('admin.cdkIssue.settings.planKey') }}</TableHead>
                    <TableHead class="px-4 py-2">{{ t('admin.cdkIssue.settings.planLabel') }}</TableHead>
                    <TableHead class="px-4 py-2">{{ t('admin.cdkIssue.settings.planStatus') }}</TableHead>
                    <TableHead class="px-4 py-2">{{ t('admin.cdkIssue.settings.planFee') }}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow v-for="plan in overview.plans" :key="plan.key">
                    <TableCell class="px-4 py-2 font-mono text-xs">{{ plan.key }}</TableCell>
                    <TableCell class="px-4 py-2 text-sm">{{ plan.label || '-' }}</TableCell>
                    <TableCell class="px-4 py-2">
                      <span
                        class="rounded-full border px-2 py-0.5 text-xs"
                        :class="plan.purchasable ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-border bg-muted/30 text-muted-foreground'"
                      >
                        {{ plan.purchasable ? t('admin.cdkIssue.settings.planOnSale') : t('admin.cdkIssue.settings.planOffSale') }}
                      </span>
                    </TableCell>
                    <TableCell class="px-4 py-2 text-sm">{{ formatUsdMinor(plan.service_fee_usd_minor) }}</TableCell>
                  </TableRow>
                  <TableRow v-if="!overview.plans.length">
                    <TableCell colspan="4" class="px-4 py-4 text-center text-xs text-muted-foreground">-</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
            <p class="mt-2 text-xs text-muted-foreground">
              {{ t('admin.cdkIssue.settings.regions') }}：{{ regionOptions.join(' / ') }}
            </p>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>
