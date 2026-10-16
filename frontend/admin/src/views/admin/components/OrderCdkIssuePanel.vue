<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminCdkIssueRequest } from '@/api/types'
import { Button } from '@/components/ui/button'
import { confirmAction } from '@/utils/confirm'
import { copyText } from '@/utils/clipboard'
import { formatDate, getLocalizedText } from '@/utils/format'
import { notifyError, notifySuccess } from '@/utils/notify'
import { cdkIssueNeedsAttention, cdkIssueStatusClass, cdkIssueStatusLabel, formatUsdMinor } from '@/utils/cdkIssue'
import { useCdkIssueRetry } from '@/composables/useCdkIssueRetry'

const props = defineProps<{
  orderId: number
}>()

const emit = defineEmits<{
  (e: 'changed'): void
}>()

const { t } = useI18n()
const requests = ref<AdminCdkIssueRequest[]>([])
const disabling = ref(false)

const load = async () => {
  if (!props.orderId) {
    requests.value = []
    return
  }
  try {
    const res = await adminAPI.getOrderCdkIssueRequests(props.orderId)
    requests.value = (res.data?.data as AdminCdkIssueRequest[]) || []
  } catch {
    // 无权限或非实时发码订单时不显示面板
    requests.value = []
  }
}

const { retryingId, retry } = useCdkIssueRetry(async () => {
  await load()
  emit('changed')
})

const canDisable = computed(() => requests.value.some((item) => item.can_disable_codes))

const disableCodes = async () => {
  const confirmed = await confirmAction({
    title: t('admin.cdkIssue.order.disableTitle'),
    description: t('admin.cdkIssue.order.disableConfirm'),
    confirmText: t('admin.cdkIssue.order.disable'),
    variant: 'destructive',
  })
  if (!confirmed) return
  disabling.value = true
  try {
    const res = await adminAPI.disableOrderCdkIssueCodes(props.orderId)
    const data = res.data?.data as { disabled?: number[]; failed?: number[] } | undefined
    notifySuccess(t('admin.cdkIssue.order.disableResult', {
      disabled: data?.disabled?.length || 0,
      failed: data?.failed?.length || 0,
    }))
    await load()
  } catch (err: any) {
    notifyError(err?.response?.data?.message || err?.message)
  } finally {
    disabling.value = false
  }
}

const copyKey = async (key: string) => {
  try {
    await copyText(key)
    notifySuccess(t('admin.cdkIssue.order.copied'))
  } catch {
    notifyError(t('admin.common.copyFailed'))
  }
}

watch(() => props.orderId, load, { immediate: true })

defineExpose({ reload: load })
</script>

<template>
  <div v-if="requests.length" class="rounded-xl border border-violet-200 bg-violet-50/40 p-4 dark:border-violet-500/30 dark:bg-violet-500/5">
    <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-sm font-semibold text-violet-800 dark:text-violet-300">{{ t('admin.cdkIssue.order.title') }}</h3>
      <Button v-if="canDisable" size="xs" variant="outline" :disabled="disabling" @click="disableCodes">
        {{ disabling ? t('admin.cdkIssue.order.disabling') : t('admin.cdkIssue.order.disable') }}
      </Button>
    </div>

    <div class="divide-y divide-border overflow-hidden rounded-lg border border-border bg-background">
      <div v-for="item in requests" :key="item.id" class="space-y-2 p-3 text-sm">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="min-w-0">
            <div class="truncate font-medium text-foreground">
              {{ getLocalizedText(item.item_title) || `#${item.product_id}` }}
              <span v-if="item.chunk_index > 0" class="text-xs text-muted-foreground">· {{ t('admin.cdkIssue.requests.chunk', { n: item.chunk_index + 1 }) }}</span>
            </div>
            <div class="mt-0.5 font-mono text-xs text-muted-foreground">
              {{ item.plan }} × {{ item.count }} · {{ item.payment_country }}
            </div>
          </div>
          <div class="flex items-center gap-3 text-xs">
            <span class="rounded-full border px-2 py-0.5" :class="cdkIssueStatusClass(item.status)">{{ cdkIssueStatusLabel(t, item.status) }}</span>
            <span class="text-muted-foreground">{{ t('admin.cdkIssue.requests.attempts', { n: item.attempts }) }}</span>
            <span class="text-muted-foreground">{{ formatUsdMinor(item.fee_amount_minor) }}</span>
          </div>
        </div>

        <div v-if="item.last_error_code || item.last_error_msg" class="text-xs" :class="cdkIssueNeedsAttention(item.status) ? 'text-red-700' : 'text-amber-700'">
          <span class="font-mono">{{ item.last_error_code || '-' }}</span>
          <span v-if="item.last_http_status"> (HTTP {{ item.last_http_status }})</span>
          <span v-if="item.last_error_msg"> · {{ item.last_error_msg }}</span>
        </div>

        <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <div class="flex min-w-0 items-center gap-1">
            <span>{{ t('admin.cdkIssue.order.idempotencyKey') }}</span>
            <span class="truncate font-mono text-foreground">{{ item.idempotency_key }}</span>
            <button type="button" class="text-primary hover:underline" @click="copyKey(item.idempotency_key)">{{ t('admin.common.copy') }}</button>
          </div>
          <div class="flex items-center gap-2">
            <span v-if="item.codes_disabled_at" class="text-muted-foreground">{{ t('admin.cdkIssue.order.codesDisabled') }}</span>
            <span>{{ t('admin.cdkIssue.order.updatedAt', { time: formatDate(item.updated_at) }) }}</span>
            <Button
              v-if="item.retry_mode"
              size="xs"
              :variant="item.retry_mode === 'same_key' ? 'destructive' : 'outline'"
              :disabled="retryingId === item.id"
              @click="retry(item)"
            >
              {{ item.retry_mode === 'same_key' ? t('admin.cdkIssue.retry.sameKey') : t('admin.cdkIssue.retry.newKey') }}
            </Button>
          </div>
        </div>

        <div v-if="item.status === 'unknown'" class="rounded-md border border-orange-200 bg-orange-50 px-2.5 py-1.5 text-xs text-orange-800">
          {{ t('admin.cdkIssue.order.unknownWarning') }}
        </div>
      </div>
    </div>
  </div>
</template>
