<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminCdkIssueBinding, AdminCdkIssueConfig, AdminCdkIssueOverview, AdminCdkIssuePlan } from '@/api/types'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { adminUrl } from '@/utils/adminBase'
import { cdkIssuePlanOptionLabel, cdkIssueRegionCodes } from '@/utils/cdkIssue'

type SKURow = {
  id: number
  sku_code: string
  spec_values: Record<string, string>
  is_active: boolean
}

const props = defineProps<{
  productId: number | null
  skus: SKURow[]
  currentLang: string
}>()

const { t } = useI18n()

const FOLLOW = '__follow__'

const loading = ref(false)
const config = ref<AdminCdkIssueConfig | null>(null)
const overview = ref<AdminCdkIssueOverview | null>(null)
// 后端原有绑定是否启用：切回库存时需要显式关闭。
const existingEnabled = ref(false)

const state = reactive({
  source: 'stock' as 'stock' | 'realtime',
  plan: '',
  payment_country: '',
  // 以 sku_code 为键，新建 SKU 在商品保存后才有 ID。
  overrides: {} as Record<string, { plan: string; payment_country: string }>,
})

const plans = computed<AdminCdkIssuePlan[]>(() => overview.value?.plans || [])
const regionCodes = computed(() => cdkIssueRegionCodes(overview.value?.payment_regions))
const configured = computed(() => Boolean(config.value?.app_id && config.value?.has_secret))
const defaultCountry = computed(() => config.value?.default_payment_country || 'PH')
const realtime = computed(() => state.source === 'realtime')

type OverrideField = 'plan' | 'payment_country'

// 模板只读取，避免渲染期间修改响应式数据。
const overrideValue = (code: string, field: OverrideField) => state.overrides[code]?.[field] || FOLLOW

const setOverride = (code: string, field: OverrideField, value: unknown) => {
  if (!code) return
  const current = state.overrides[code] || { plan: '', payment_country: '' }
  state.overrides[code] = { ...current, [field]: value === FOLLOW ? '' : String(value) }
}

const skuSpecText = (sku: SKURow) => {
  const values = sku.spec_values || {}
  return values[props.currentLang] || values['zh-CN'] || Object.values(values).find(Boolean) || ''
}

// 服务端按 SKU ID 返回的覆盖，等商品 SKU 加载后再转成 sku_code 键。
const serverOverrides = new Map<number, { plan: string; payment_country: string }>()

const resolveServerOverrides = () => {
  for (const [skuId, value] of serverOverrides) {
    const sku = props.skus.find((row) => row.id === skuId)
    if (!sku?.sku_code) continue
    if (!state.overrides[sku.sku_code]) state.overrides[sku.sku_code] = { ...value }
    serverOverrides.delete(skuId)
  }
}

const reset = () => {
  serverOverrides.clear()
  existingEnabled.value = false
  state.source = 'stock'
  state.plan = ''
  state.payment_country = ''
  state.overrides = {}
}

const loadReferenceData = async () => {
  try {
    const res = await adminAPI.getCdkIssueConfig()
    config.value = res.data?.data as AdminCdkIssueConfig
  } catch {
    config.value = null
  }
  if (!configured.value) {
    overview.value = null
    return
  }
  try {
    const res = await adminAPI.getCdkIssueOverview()
    overview.value = res.data?.data as AdminCdkIssueOverview
  } catch {
    overview.value = null
  }
}

const load = async () => {
  reset()
  loading.value = true
  try {
    await loadReferenceData()
    if (!props.productId) return
    const res = await adminAPI.getCdkIssueBinding(props.productId)
    const binding = res.data?.data as AdminCdkIssueBinding | undefined
    if (!binding) return
    existingEnabled.value = Boolean(binding.enabled)
    state.source = binding.enabled ? 'realtime' : 'stock'
    state.plan = binding.plan || ''
    state.payment_country = binding.payment_country || ''
    for (const item of binding.skus || []) {
      serverOverrides.set(item.sku_id, { plan: item.plan || '', payment_country: item.payment_country || '' })
    }
    resolveServerOverrides()
  } catch {
    // 无权限或接口不可用时保持「本站库存」，不影响商品编辑
  } finally {
    loading.value = false
  }
}

const planOf = (code: string) => state.overrides[code]?.plan || state.plan

/** 保存商品前的本地校验；返回错误文案或空字符串。 */
const validate = (): string => {
  if (!realtime.value) return ''
  if (!configured.value) return t('admin.cdkIssue.product.notConfigured')
  const activeSkus = props.skus.filter((sku) => sku.is_active)
  if (!activeSkus.length) {
    return state.plan ? '' : t('admin.cdkIssue.product.planRequired')
  }
  const missing = activeSkus.find((sku) => !planOf(sku.sku_code))
  if (missing) return t('admin.cdkIssue.product.skuPlanRequired', { code: missing.sku_code })
  const offSale = activeSkus.map((sku) => planOf(sku.sku_code)).find((key) => {
    const plan = plans.value.find((item) => item.key === key)
    return plan && !plan.purchasable
  })
  if (offSale) return t('admin.cdkIssue.product.planOffSale', { plan: offSale })
  return ''
}

/** 商品保存成功后调用：用保存后的 SKU ID 写入绑定。 */
const save = async (productId: number, savedSkus: { id: number; sku_code: string }[]) => {
  if (!realtime.value && !existingEnabled.value) return
  const skuIdByCode = new Map(savedSkus.map((sku) => [sku.sku_code.toLowerCase(), sku.id]))
  const payload: AdminCdkIssueBinding = {
    enabled: realtime.value,
    plan: state.plan,
    payment_country: state.payment_country,
    skus: [],
  }
  for (const [code, value] of Object.entries(state.overrides)) {
    const id = skuIdByCode.get(code.toLowerCase())
    if (!id || (!value.plan && !value.payment_country)) continue
    payload.skus.push({ sku_id: id, plan: value.plan, payment_country: value.payment_country })
  }
  await adminAPI.saveCdkIssueBinding(productId, payload)
  existingEnabled.value = payload.enabled
}

/** 交付类型改为人工后，强制回到本站库存（保存时会关闭已有绑定）。 */
const forceStock = () => {
  state.source = 'stock'
}

watch(() => props.productId, () => { void load() })
// 商品详情异步加载，SKU 列表到达后再把按 ID 存储的覆盖映射到 sku_code。
watch(() => props.skus.map((sku) => `${sku.id}:${sku.sku_code}`).join(','), resolveServerOverrides)
onMounted(() => { void load() })

defineExpose({ load, validate, save, forceStock, realtime })

const settingsLink = adminUrl('/settings/cdk-issue')
</script>

<template>
  <div class="col-span-1 space-y-4 rounded-xl border border-border bg-muted/20 p-4 md:col-span-2">
    <div>
      <h3 class="text-sm font-semibold text-foreground">{{ t('admin.cdkIssue.product.title') }}</h3>
      <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.cdkIssue.product.subtitle') }}</p>
    </div>

    <div class="flex flex-col gap-2 sm:flex-row">
      <button
        type="button"
        class="flex-1 rounded-lg border px-3 py-2 text-left text-sm transition-colors"
        :class="realtime ? 'border-primary bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:border-primary/40'"
        :disabled="loading"
        @click="state.source = 'realtime'"
      >
        <div class="font-medium">{{ t('admin.cdkIssue.product.sourceRealtime') }}</div>
        <div class="mt-0.5 text-xs opacity-80">{{ t('admin.cdkIssue.product.sourceRealtimeHint') }}</div>
      </button>
      <button
        type="button"
        class="flex-1 rounded-lg border px-3 py-2 text-left text-sm transition-colors"
        :class="!realtime ? 'border-primary bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:border-primary/40'"
        :disabled="loading"
        @click="state.source = 'stock'"
      >
        <div class="font-medium">{{ t('admin.cdkIssue.product.sourceStock') }}</div>
        <div class="mt-0.5 text-xs opacity-80">{{ t('admin.cdkIssue.product.sourceStockHint') }}</div>
      </button>
    </div>

    <template v-if="realtime">
      <div v-if="!configured" class="rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-700">
        {{ t('admin.cdkIssue.product.notConfigured') }}
        <a :href="settingsLink" target="_blank" rel="noopener" class="ml-1 underline">{{ t('admin.cdkIssue.product.goSettings') }}</a>
      </div>
      <div v-else-if="config && !config.enabled" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-700">
        {{ t('admin.cdkIssue.product.disabledWarning') }}
      </div>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div>
          <label class="mb-1.5 block text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.product.defaultPlan') }}</label>
          <Select v-if="plans.length" v-model="state.plan">
            <SelectTrigger class="h-9 w-full">
              <SelectValue :placeholder="t('admin.cdkIssue.product.planPlaceholder')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="plan in plans" :key="plan.key" :value="plan.key" :disabled="!plan.purchasable">
                {{ cdkIssuePlanOptionLabel(plan) }}<span v-if="!plan.purchasable"> · {{ t('admin.cdkIssue.settings.planOffSale') }}</span>
              </SelectItem>
            </SelectContent>
          </Select>
          <Input v-else v-model="state.plan" placeholder="pro_5x" />
          <p v-if="!plans.length" class="mt-1 text-xs text-muted-foreground">{{ t('admin.cdkIssue.product.plansUnavailable') }}</p>
        </div>
        <div>
          <label class="mb-1.5 block text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.product.country') }}</label>
          <Select
            :model-value="state.payment_country || FOLLOW"
            @update:model-value="(value) => { state.payment_country = value === FOLLOW ? '' : String(value) }"
          >
            <SelectTrigger class="h-9 w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem :value="FOLLOW">{{ t('admin.cdkIssue.product.followGlobal', { country: defaultCountry }) }}</SelectItem>
              <SelectItem v-for="code in regionCodes" :key="code" :value="code">{{ code }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div v-if="skus.length" class="space-y-2">
        <div class="text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.product.skuOverrides') }}</div>
        <div class="overflow-x-auto rounded-lg border border-border bg-background">
          <table class="w-full min-w-[560px] text-sm">
            <thead class="bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th class="px-3 py-2 text-left font-medium">{{ t('admin.products.form.skuCode') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('admin.cdkIssue.product.skuSpec') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('admin.cdkIssue.product.plan') }}</th>
                <th class="px-3 py-2 text-left font-medium">{{ t('admin.cdkIssue.product.country') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border">
              <tr v-for="sku in skus" :key="sku.sku_code || sku.id" :class="sku.is_active ? '' : 'opacity-50'">
                <td class="px-3 py-2 font-mono text-xs">{{ sku.sku_code || '-' }}</td>
                <td class="px-3 py-2 text-xs">{{ skuSpecText(sku) || '-' }}</td>
                <td class="px-3 py-2">
                  <Select
                    :model-value="overrideValue(sku.sku_code, 'plan')"
                    :disabled="!sku.sku_code"
                    @update:model-value="(value) => setOverride(sku.sku_code, 'plan', value)"
                  >
                    <SelectTrigger class="h-8 w-full text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem :value="FOLLOW">{{ t('admin.cdkIssue.product.followDefault') }}</SelectItem>
                      <SelectItem v-for="plan in plans" :key="plan.key" :value="plan.key" :disabled="!plan.purchasable">{{ cdkIssuePlanOptionLabel(plan) }}</SelectItem>
                    </SelectContent>
                  </Select>
                </td>
                <td class="px-3 py-2">
                  <Select
                    :model-value="overrideValue(sku.sku_code, 'payment_country')"
                    :disabled="!sku.sku_code"
                    @update:model-value="(value) => setOverride(sku.sku_code, 'payment_country', value)"
                  >
                    <SelectTrigger class="h-8 w-full text-xs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem :value="FOLLOW">{{ t('admin.cdkIssue.product.followDefault') }}</SelectItem>
                      <SelectItem v-for="code in regionCodes" :key="code" :value="code">{{ code }}</SelectItem>
                    </SelectContent>
                  </Select>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="rounded-lg border border-sky-200 bg-sky-50 p-3 text-xs text-sky-800 dark:border-sky-500/30 dark:bg-sky-500/10 dark:text-sky-300">
        {{ t('admin.cdkIssue.product.notice') }}
      </div>
    </template>
  </div>
</template>
