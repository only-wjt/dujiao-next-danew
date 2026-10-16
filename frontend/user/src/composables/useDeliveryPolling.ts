import { onUnmounted, watch, type Ref } from 'vue'

export const DELIVERY_POLL_INTERVAL_MS = 3000
export const DELIVERY_POLL_MAX_MS = 2 * 60 * 1000

/** 订单（含子订单）是否仍在实时生成卡密。 */
export const isOrderIssuing = (order: any): boolean => {
  if (!order) return false
  if (order.delivery_state === 'issuing') return true
  return Array.isArray(order.children) && order.children.some((child: any) => child?.delivery_state === 'issuing')
}

/**
 * 实时发码订单在生成卡密期间静默轮询订单详情。
 * - 每 3 秒刷新一次，卡密出现或状态变化后自动停止；
 * - 2 分钟仍未完成时停止轮询，界面切换为「发放延迟」提示（后端也会返回 delayed）。
 */
export function useDeliveryPolling(order: Ref<any>, refresh: () => Promise<void>) {
  let timer: ReturnType<typeof setTimeout> | null = null
  let startedAt = 0
  let stopped = false
  let finalRefreshDone = false

  const clear = () => {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
  }

  const schedule = () => {
    clear()
    if (stopped || !isOrderIssuing(order.value)) {
      startedAt = 0
      return
    }
    if (!startedAt) startedAt = Date.now()
    if (Date.now() - startedAt >= DELIVERY_POLL_MAX_MS) {
      // 最后再刷新一次：后端超过宽限期会返回 delayed，避免界面停在「生成中」。
      if (!finalRefreshDone) {
        finalRefreshDone = true
        timer = setTimeout(() => {
          timer = null
          refresh().catch(() => {})
        }, DELIVERY_POLL_INTERVAL_MS)
      }
      return
    }
    timer = setTimeout(async () => {
      timer = null
      if (typeof document !== 'undefined' && document.hidden) {
        schedule()
        return
      }
      try {
        await refresh()
      } catch {
        // 刷新失败时下一轮再试
      }
      schedule()
    }, DELIVERY_POLL_INTERVAL_MS)
  }

  watch(() => isOrderIssuing(order.value), (issuing, wasIssuing) => {
    if (issuing && !timer) schedule()
    if (!issuing && wasIssuing) {
      clear()
      startedAt = 0
      // 卡密到账后滚动到交付区域
      if (order.value && typeof document !== 'undefined') {
        requestAnimationFrame(() => {
          document.querySelector('[data-delivery-anchor]')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
        })
      }
    }
  })

  onUnmounted(() => {
    stopped = true
    clear()
  })
}
