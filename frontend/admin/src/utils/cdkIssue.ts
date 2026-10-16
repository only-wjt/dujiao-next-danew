import type { AdminCdkIssuePlan, CdkIssueRequestStatus } from '@/api/types'
import type { TranslateFn } from './status'

export const CDK_ISSUE_STATUSES: CdkIssueRequestStatus[] = ['pending', 'succeeded', 'failed', 'unknown']

export const cdkIssueStatusLabel = (t: TranslateFn, status?: string) => {
  if (!status) return '-'
  if ((CDK_ISSUE_STATUSES as string[]).includes(status)) return t(`admin.cdkIssue.status.${status}`)
  return status
}

export const cdkIssueStatusClass = (status?: string) => {
  switch (status) {
    case 'pending':
      return 'border-amber-200 bg-amber-50 text-amber-700'
    case 'succeeded':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'failed':
      return 'border-red-200 bg-red-50 text-red-700'
    case 'unknown':
      return 'border-orange-300 bg-orange-50 text-orange-700'
    default:
      return 'border-border bg-muted/30 text-muted-foreground'
  }
}

/** 失败与结果不明的记录需要人工处理，在列表中高亮。 */
export const cdkIssueNeedsAttention = (status?: string) => status === 'failed' || status === 'unknown'

/** 卡台金额以 USD 分为单位。 */
export const formatUsdMinor = (minor?: number | null) => {
  const value = Number(minor || 0)
  if (!Number.isFinite(value) || value <= 0) return '-'
  return `$${(value / 100).toFixed(2)}`
}

export const formatUsd = (amount?: string | number | null) => {
  const value = Number(amount)
  if (!Number.isFinite(value)) return '-'
  return value.toFixed(2)
}

export const cdkIssuePlanOptionLabel = (plan: AdminCdkIssuePlan) => {
  const parts = [plan.key]
  if (plan.label && plan.label.toLowerCase() !== plan.key.toLowerCase()) parts.push(plan.label)
  const fee = formatUsdMinor(plan.service_fee_usd_minor)
  if (fee !== '-') parts.push(fee)
  return parts.join(' · ')
}

/** 卡台未下发地区清单时只支持 PH。 */
export const cdkIssueRegionCodes = (regions?: { country: string }[] | null) => {
  const codes = (regions || []).map((item) => String(item.country || '').toUpperCase()).filter(Boolean)
  return codes.length ? Array.from(new Set(codes)) : ['PH']
}
