<template>
  <div class="grid gap-2.5">
    <div v-if="state === 'issuing'" class="rounded-lg border-2 border-primary/40 bg-primary/5 p-4" role="status" aria-live="polite">
      <div class="flex items-center gap-2 text-base font-bold text-primary">
        <Loader2 class="h-5 w-5 flex-none animate-spin" />
        <span>{{ t('orderDetail.issuingTitle') }}</span>
      </div>
      <p class="mt-1 text-[13px] text-muted-foreground">{{ t('orderDetail.issuingHint') }}</p>
      <div class="mt-3 grid gap-2">
        <div v-for="index in skeletonRows" :key="index" class="h-11 rounded-md border bg-background px-3 py-2.5">
          <div class="h-full w-2/3 rounded theme-skeleton"></div>
        </div>
      </div>
    </div>

    <div v-else class="rounded-lg border border-warning/40 bg-warning/10 p-4" role="status">
      <div class="flex items-center gap-2 text-base font-bold text-foreground">
        <Hourglass class="h-5 w-5 flex-none text-warning" />
        <span>{{ t('orderDetail.delayedTitle') }}</span>
      </div>
      <p class="mt-1 text-[13px] text-muted-foreground">{{ t('orderDetail.delayedHint') }}</p>
      <div class="mt-3 flex flex-wrap gap-2">
        <Button type="button" size="sm" variant="outline" class="rounded-full" :disabled="refreshing" @click="handleRefresh">
          <RefreshCw class="h-4 w-4" :class="refreshing ? 'animate-spin' : ''" />
          {{ t('orderDetail.delayedRefresh') }}
        </Button>
        <Button as-child size="sm" variant="ghost" class="rounded-full">
          <router-link to="/about">{{ t('orderDetail.delayedContact') }}</router-link>
        </Button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Hourglass, Loader2, RefreshCw } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const props = defineProps<{
  state: 'issuing' | 'delayed'
  items?: any[]
  refresh?: () => Promise<void>
}>()

const { t } = useI18n()
const refreshing = ref(false)

// 骨架行数与购买数量一致，最多 5 行。
const skeletonRows = computed(() => {
  const total = (props.items || []).reduce((sum, item) => sum + Math.max(Number(item?.quantity) || 0, 0), 0)
  return Math.min(Math.max(total, 1), 5)
})

const handleRefresh = async () => {
  if (!props.refresh || refreshing.value) return
  refreshing.value = true
  try {
    await props.refresh()
  } catch {
    // 保持当前提示
  } finally {
    refreshing.value = false
  }
}
</script>
