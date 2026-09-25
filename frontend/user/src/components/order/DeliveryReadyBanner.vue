<template>
  <button
    v-if="visible"
    type="button"
    class="flex w-full items-center gap-3 rounded-xl border-2 border-primary/50 bg-primary/10 px-[18px] py-3.5 text-left transition-colors hover:bg-primary/15"
    @click="scrollToDelivery"
  >
    <KeyRound class="h-6 w-6 flex-none text-primary" />
    <div class="min-w-0 flex-1">
      <div class="font-bold text-primary">{{ t('orderDetail.deliveryReadyTitle') }}</div>
      <div class="text-[13px] text-muted-foreground">{{ t('orderDetail.deliveryReadyHint') }}</div>
    </div>
    <ChevronDown class="h-5 w-5 flex-none text-primary" />
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, KeyRound } from 'lucide-vue-next'

const props = defineProps<{
  order: any
}>()

const { t } = useI18n()

const isDelivered = (target: any) => target?.fulfillment?.status === 'delivered'

const visible = computed(() => {
  const order = props.order
  if (!order) return false
  if (isDelivered(order)) return true
  return Array.isArray(order.children) && order.children.some(isDelivered)
})

const scrollToDelivery = () => {
  const target = document.querySelector('[data-delivery-anchor]')
  target?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
</script>
