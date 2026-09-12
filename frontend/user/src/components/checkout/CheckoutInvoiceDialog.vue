<template>
  <section class="space-y-3 rounded-xl border bg-muted/20 p-4">
    <div>
      <h3 class="text-sm font-bold text-foreground">{{ t('checkout.invoice.title') }}</h3>
      <p class="mt-1 text-xs leading-relaxed text-muted-foreground">{{ t('checkout.invoice.feeHint') }}</p>
    </div>

    <label class="flex items-center gap-2 text-sm text-foreground">
      <input v-model="form.requested" type="checkbox" class="h-4 w-4 accent-primary" />
      <span>{{ t('checkout.invoice.needInvoice') }}</span>
    </label>

    <div v-if="form.requested" class="space-y-3">
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
      <div v-if="hasPositiveAmount(invoiceFee)" class="flex items-center justify-between text-xs text-muted-foreground">
        <span>{{ t('checkout.invoice.feeLabel') }}</span>
        <span class="font-mono">+{{ formatPrice(invoiceFee, currency) }}</span>
      </div>
    </div>

    <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

defineProps<{
  form: {
    requested: boolean
    title: string
    note: string
    email: string
  }
  currency: string
  invoiceFee: string
  error: string
  formatPrice: (amount: any, currency?: string) => string
  hasPositiveAmount: (amount: any) => boolean
}>()

const { t } = useI18n()
</script>
