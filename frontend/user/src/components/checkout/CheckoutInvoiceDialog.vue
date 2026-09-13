<template>
  <section class="rounded-xl border bg-card p-5">
    <h2 class="text-lg font-bold text-foreground">{{ t('checkout.invoice.title') }}</h2>
    <p class="mt-1.5 text-sm leading-relaxed text-muted-foreground">{{ t('checkout.invoice.feeHint') }}</p>

    <label class="mt-4 flex min-h-11 cursor-pointer items-center gap-3 rounded-lg border bg-secondary px-3 text-base font-medium text-foreground">
      <input v-model="form.requested" type="checkbox" class="h-5 w-5 accent-primary" />
      <span>{{ t('checkout.invoice.needInvoice') }}</span>
    </label>

    <div v-if="form.requested" class="mt-4 space-y-3">
      <div class="space-y-1.5">
        <label class="text-xs font-medium text-muted-foreground">{{ t('checkout.invoice.nameLabel') }}</label>
        <Input v-model="form.title" :placeholder="t('checkout.invoice.namePlaceholder')" />
      </div>
      <div class="space-y-1.5">
        <label class="text-xs font-medium text-muted-foreground">{{ t('checkout.invoice.emailLabel') }}</label>
        <Input v-model="form.email" type="email" :placeholder="t('checkout.invoice.emailPlaceholder')" />
      </div>
      <div class="space-y-1.5">
        <label class="text-xs font-medium text-muted-foreground">{{ t('checkout.invoice.noteLabel') }}</label>
        <Textarea v-model="form.note" rows="3" :placeholder="t('checkout.invoice.notePlaceholder')" />
      </div>
      <div v-if="feeVisible" class="flex items-center justify-between text-sm text-muted-foreground">
        <span>{{ t('checkout.invoice.feeLabel') }}</span>
        <span class="font-mono">+{{ formatPrice(invoiceFee, currency) }}</span>
      </div>
      <div v-if="channelFeeVisible" class="flex items-center justify-between text-sm text-muted-foreground">
        <span>{{ t('checkout.invoice.channelFeeLabel') }}</span>
        <span class="font-mono">+{{ formatPrice(channelFee, currency) }}</span>
      </div>
      <div v-if="payableVisible" class="flex items-center justify-between text-sm font-medium text-foreground">
        <span>{{ t('checkout.invoice.amountLabel') }}</span>
        <span class="font-mono">{{ formatPrice(payable, currency) }}</span>
      </div>
    </div>

    <p v-if="error" class="mt-3 text-sm text-destructive">{{ error }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

const props = defineProps<{
  form: {
    requested: boolean
    title: string
    note: string
    email: string
  }
  currency: string
  invoiceFee: string
  channelFee?: string
  payable?: string
  error: string
  formatPrice: (amount: any, currency?: string) => string
  hasPositiveAmount?: (amount: any) => boolean
}>()

const { t } = useI18n()
const isPositive = (value?: string) => {
  if (props.hasPositiveAmount) return props.hasPositiveAmount(value)
  const amount = Number(value)
  return Number.isFinite(amount) && amount > 0
}
const feeVisible = computed(() => props.form.requested && isPositive(props.invoiceFee))
const channelFeeVisible = computed(() => props.form.requested && isPositive(props.channelFee))
const payableVisible = computed(() => props.form.requested && Boolean(props.payable))
</script>
