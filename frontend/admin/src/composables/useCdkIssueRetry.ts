import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminCdkIssueRequest } from '@/api/types'
import { confirmAction } from '@/utils/confirm'
import { notifyError, notifySuccess } from '@/utils/notify'

/**
 * 发码重试：方式由后端按状态决定（retry_mode），界面只负责确认。
 * - new_key：卡台明确拒绝、未扣费，换新幂等键重发。
 * - same_key：结果不明，沿用原键，卡台若已发码会原样返回，不会重复扣费。
 */
export const useCdkIssueRetry = (onDone: () => Promise<void> | void) => {
  const { t } = useI18n()
  const retryingId = ref<number | null>(null)

  const retry = async (request: AdminCdkIssueRequest) => {
    const mode = request.retry_mode
    if (!mode) return
    const confirmed = await confirmAction({
      title: mode === 'same_key' ? t('admin.cdkIssue.retry.sameKeyTitle') : t('admin.cdkIssue.retry.newKeyTitle'),
      description: mode === 'same_key'
        ? t('admin.cdkIssue.retry.sameKeyConfirm', { key: request.idempotency_key })
        : t('admin.cdkIssue.retry.newKeyConfirm', { code: request.last_error_code || '-' }),
      confirmText: mode === 'same_key' ? t('admin.cdkIssue.retry.sameKey') : t('admin.cdkIssue.retry.newKey'),
      variant: mode === 'same_key' ? 'destructive' : 'default',
    })
    if (!confirmed) return
    retryingId.value = request.id
    try {
      await adminAPI.retryCdkIssueRequest(request.id, mode)
      notifySuccess(t('admin.cdkIssue.retry.queued'))
      await onDone()
    } catch (err: any) {
      notifyError(err?.response?.data?.message || err?.message || t('admin.cdkIssue.retry.failed'))
    } finally {
      retryingId.value = null
    }
  }

  return { retryingId, retry }
}
