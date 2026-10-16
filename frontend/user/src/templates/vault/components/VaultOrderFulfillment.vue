<template>
  <div class="grid gap-2.5">
    <h3 v-if="title" class="text-[15px] font-bold">{{ title }}</h3>

    <div
      class="rounded-lg p-4"
      :class="delivered ? 'border-2 border-primary/60 bg-primary/5 shadow-sm' : 'border bg-secondary'"
    >
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="flex items-center gap-2 text-base font-bold" :class="delivered ? 'text-primary' : 'text-foreground'">
            <KeyRound class="h-5 w-5 flex-none" />
            <span>{{ t('orderDetail.deliveryContentTitle') }}</span>
          </div>
          <p v-if="delivered" class="mt-1 text-[13px] text-muted-foreground">{{ t('orderDetail.deliveryContentHint') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button
            v-if="isFulfillmentTruncated(fulfillment)"
            type="button"
            size="sm"
            class="rounded-full"
            :disabled="downloading"
            @click="emit('download', orderNo)"
          >
            <Download /> {{ downloading ? t('orderDetail.fulfillmentDownloading') : t('orderDetail.fulfillmentDownload') }}
          </Button>
          <Button
            v-if="delivered && !isFulfillmentTruncated(fulfillment)"
            type="button"
            size="sm"
            class="rounded-full"
            :class="fulfillmentCopied ? 'bg-[color:var(--teal-strong)] text-white hover:bg-[color:var(--teal-strong)]/90' : ''"
            @click="handleCopyFulfillment(fulfillment)"
          >
            <component :is="fulfillmentCopied ? Check : Copy" /> {{ fulfillmentCopied ? t('orderDetail.fulfillmentCopied') : t('orderDetail.fulfillmentCopyAll') }}
          </Button>
        </div>
      </div>

      <template v-if="isFulfillmentTruncated(fulfillment)">
        <div class="mt-3 text-xs text-muted-foreground">{{ t('orderDetail.fulfillmentTotalLines', { count: fulfillment.payload_line_count }) }}</div>
        <div class="mt-1.5 rounded-sm bg-warning/10 px-2.5 py-2 text-xs font-semibold text-warning">{{ t('orderDetail.fulfillmentTruncatedHint') }}</div>
        <pre class="mt-2 max-h-[260px] overflow-y-auto whitespace-pre-wrap break-all rounded-md border bg-background p-3 font-mono text-[14px] font-semibold text-foreground">{{ fulfillment.payload }}</pre>
      </template>
      <div v-else-if="displayLines.length" class="mt-3 grid gap-2">
        <div
          v-for="(line, index) in displayLines"
          :key="index"
          class="flex items-center justify-between gap-3 rounded-md border bg-background px-3 py-2.5"
        >
          <div class="flex min-w-0 items-center gap-2">
            <span
              v-if="regionText"
              class="inline-flex flex-none items-center rounded-full border border-primary/30 bg-primary/10 px-2 py-0.5 text-[11px] font-bold text-primary"
              :title="t('orderDetail.paymentRegion')"
            >{{ regionText }}</span>
            <span class="min-w-0 select-all whitespace-pre-wrap break-all font-mono text-[15px] font-semibold leading-relaxed text-foreground">{{ line }}</span>
          </div>
          <div v-if="delivered" class="flex flex-none items-center gap-1">
            <Button
              v-if="redeemHref(line)"
              as-child
              type="button"
              size="sm"
              class="h-8 rounded-full px-2.5"
            >
              <a :href="redeemHref(line)" target="_blank" rel="noopener noreferrer">
                <ExternalLink class="h-4 w-4" />
                {{ t('orderDetail.redeem') }}
              </a>
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              class="h-8 rounded-full px-2.5 text-primary"
              :aria-label="t('orderDetail.fulfillmentCopy')"
              @click="handleCopyLine(line, index)"
            >
              <component :is="copiedLineIndex === index ? Check : Copy" class="h-4 w-4" />
              <span class="max-[480px]:hidden">{{ copiedLineIndex === index ? t('orderDetail.fulfillmentCopied') : t('orderDetail.fulfillmentCopyLine') }}</span>
            </Button>
          </div>
        </div>
      </div>
      <div v-else class="mt-3 text-[13px] text-muted-foreground">{{ fulfillmentStatusLabelText(fulfillment.status) }}</div>
    </div>

    <div class="flex flex-wrap gap-x-5 gap-y-1 text-[12.5px] text-muted-foreground">
      <span>{{ t('orderDetail.fulfillmentType') }}：<span class="font-semibold text-foreground">{{ fulfillmentTypeLabelText(fulfillment.type) }}</span></span>
      <span>{{ t('orderDetail.fulfillmentStatus') }}：<span class="font-semibold text-foreground">{{ fulfillmentStatusLabelText(fulfillment.status) }}</span></span>
    </div>

    <div v-if="delivered && instructionBlocks(items).length" class="mt-1 grid gap-2.5">
      <div v-for="(block, bi) in instructionBlocks(items)" :key="bi" class="rounded-md border border-primary/20 bg-primary/10 p-3.5">
        <div class="mb-2 flex items-center gap-2 text-[13px] font-bold text-primary"><Info class="h-4 w-4" /> {{ t('orderDetail.instructionsTitle') }}</div>
        <div class="prose prose-sm max-w-none dark:prose-invert prose-a:text-primary prose-img:rounded-sm" v-html="block.html"></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check, Download, ExternalLink, Info, KeyRound } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { useOrderDisplayHelpers } from '../../../composables/useOrderDisplayHelpers'
import { orderPaymentRegions, regionLabelKey } from '../../../utils/cdkRegion'

const props = defineProps<{
  title?: string
  fulfillment: any
  items?: any[]
  orderNo: string
  downloading: boolean
}>()

const emit = defineEmits<{
  (e: 'download', orderNo: string): void
}>()

const { t } = useI18n()

const {
  isFulfillmentTruncated, fulfillmentDeliveryLines, instructionBlocks,
  fulfillmentTypeLabelText, fulfillmentStatusLabelText, fulfillmentCopied, handleCopyFulfillment,
  copiedLineIndex, handleCopyLine,
} = useOrderDisplayHelpers(ref<any>(null))

const delivered = computed(() => props.fulfillment?.status === 'delivered')

const displayLines = computed(() => {
  const deliveryLines = fulfillmentDeliveryLines(props.fulfillment)
  if (deliveryLines.length) return deliveryLines
  return String(props.fulfillment?.payload || '')
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
})

// 实时发码卡密显示付款地区（一个交付只对应一个地区时才显示，避免误标）。
const regionText = computed(() => {
  const regions = orderPaymentRegions(props.items)
  if (regions.length !== 1) return ''
  const code = regions[0]!
  const key = regionLabelKey(code)
  return key ? `${code} ${t(key)}` : code
})

const redeemTemplate = computed(() => {
  const items = Array.isArray(props.items) ? props.items : []
  for (const item of items) {
    const url = String(item?.redeem_url || '').trim()
    if (url.includes('{code}')) return url
  }
  return ''
})

function redeemHref(line: string) {
  const template = redeemTemplate.value
  const code = String(line || '').trim()
  if (!template || !code) return ''
  return template.replace('{code}', encodeURIComponent(code))
}
</script>
