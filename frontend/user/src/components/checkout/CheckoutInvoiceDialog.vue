<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="fixed inset-0 z-[110] flex items-center justify-center p-4"
      @click.self="emit('close')"
    >
      <div class="fixed inset-0 bg-black/40 backdrop-blur-sm" aria-hidden="true" />
      <div
        role="dialog"
        aria-modal="true"
        class="relative z-10 w-full max-w-md rounded-2xl border bg-card p-6 text-card-foreground shadow-2xl"
      >
        <h3 class="text-lg font-bold text-foreground">{{ t('checkout.invoice.title') }}</h3>
        <p class="mt-2 text-sm leading-relaxed text-muted-foreground">{{ t('checkout.invoice.subtitle') }}</p>

        <label class="mt-5 flex items-center gap-2 text-sm text-foreground">
          <input v-model="form.requested" type="checkbox" class="h-4 w-4 accent-primary" />
          <span>{{ t('checkout.invoice.needInvoice') }}</span>
        </label>
        <p class="mt-2 text-xs text-muted-foreground">{{ t('checkout.invoice.feeHint') }}</p>

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
        </div>

        <div class="mt-5 space-y-1.5 rounded-xl border bg-muted/30 px-4 py-3 text-sm">
          <div v-if="form.requested && hasPositiveAmount(invoiceFee)" class="flex items-center justify-between text-muted-foreground">
            <span>{{ t('checkout.invoice.feeLabel') }}</span>
            <span class="font-mono">+{{ formatPrice(invoiceFee, currency) }}</span>
          </div>
          <div class="flex items-center justify-between font-semibold text-foreground">
            <span>{{ t('checkout.invoice.payableLabel') }}</span>
            <span class="font-mono text-lg">{{ formatPrice(total, currency) }}</span>
          </div>
        </div>

        <p v-if="error" class="mt-3 text-sm text-destructive">{{ error }}</p>

        <div class="mt-6 flex items-center justify-end gap-3">
          <Button variant="secondary" :disabled="submitting" @click="emit('close')">
            {{ t('checkout.invoice.back') }}
          </Button>
          <Button :disabled="submitting" @click="emit('confirm')">
            {{ submitting ? t('checkout.submitting') : t('checkout.invoice.pay') }}
          </Button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

defineProps<{
  open: boolean
  form: {
    requested: boolean
    title: string
    note: string
    email: string
  }
  currency: string
  total: string
  invoiceFee: string
  submitting: boolean
  error: string
  formatPrice: (amount: any, currency?: string) => string
  hasPositiveAmount: (amount: any) => boolean
}>()

const emit = defineEmits<{
  close: []
  confirm: []
}>()

const { t } = useI18n()
</script>
