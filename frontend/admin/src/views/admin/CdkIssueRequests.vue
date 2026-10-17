<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDebounceFn } from '@vueuse/core'
import { adminAPI } from '@/api/admin'
import type { AdminCdkIssueRequest, AdminCdkIssueStats } from '@/api/types'
import TableSkeleton from '@/components/TableSkeleton.vue'
import ListPagination from '@/components/ListPagination.vue'
import { useListRefresh, type ListFetchOptions } from '@/composables/useListRefresh'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { adminUrl } from '@/utils/adminBase'
import { formatDate, getLocalizedText, toRFC3339 } from '@/utils/format'
import {
  CDK_ISSUE_STATUSES,
  cdkIssueNeedsAttention,
  cdkIssueStatusClass,
  cdkIssueStatusLabel,
  formatUsdMinor,
} from '@/utils/cdkIssue'
import { useCdkIssueRetry } from '@/composables/useCdkIssueRetry'

const { t } = useI18n()
const loading = ref(true)
const { refreshing, refreshList } = useListRefresh()
const rows = ref<AdminCdkIssueRequest[]>([])
const pagination = reactive({ page: 1, page_size: 20, total: 0, total_page: 1 })
const pageSizeOptions = [10, 20, 50, 100]
const filters = reactive({
  status: '__all__',
  plan: '',
  order_no: '',
  created_from: '',
  created_to: '',
})
const stats = reactive<AdminCdkIssueStats>({ total: 0, pending: 0, succeeded: 0, failed: 0, unknown: 0 })

const statCards = [
  { value: '__all__', key: 'total', tone: 'neutral' },
  { value: 'pending', key: 'pending', tone: 'amber' },
  { value: 'succeeded', key: 'succeeded', tone: 'emerald' },
  { value: 'failed', key: 'failed', tone: 'red' },
  { value: 'unknown', key: 'unknown', tone: 'orange' },
] as const

const statCardClass = (tone: string, selected: boolean) => {
  const map: Record<string, [string, string]> = {
    neutral: ['border-border', 'border-foreground/40 ring-1 ring-foreground/30'],
    amber: ['border-amber-200 bg-amber-50/50', 'border-amber-400 bg-amber-50/50 ring-1 ring-amber-300'],
    emerald: ['border-emerald-200 bg-emerald-50/50', 'border-emerald-400 bg-emerald-50/50 ring-1 ring-emerald-300'],
    red: ['border-red-200 bg-red-50/50', 'border-red-400 bg-red-50/50 ring-1 ring-red-300'],
    orange: ['border-orange-200 bg-orange-50/50', 'border-orange-400 bg-orange-50/50 ring-1 ring-orange-300'],
  }
  const pair = map[tone] || map.neutral!
  return selected ? pair[1] : pair[0]
}

const statTextClass = (tone: string) => ({
  neutral: 'text-foreground',
  amber: 'text-amber-700',
  emerald: 'text-emerald-700',
  red: 'text-red-700',
  orange: 'text-orange-700',
}[tone] || 'text-foreground')

const buildParams = (withStatus: boolean) => {
  const params: Record<string, unknown> = {}
  if (withStatus && filters.status !== '__all__') params.status = filters.status
  if (filters.plan.trim()) params.plan = filters.plan.trim()
  if (filters.order_no.trim()) params.order_no = filters.order_no.trim()
  const from = toRFC3339(filters.created_from)
  if (from) params.created_from = from
  const to = toRFC3339(filters.created_to)
  if (to) params.created_to = to
  return params
}

const fetchStats = async () => {
  try {
    const res = await adminAPI.getCdkIssueStats(buildParams(false))
    Object.assign(stats, res.data?.data || {})
  } catch {
    // 统计失败不影响列表
  }
}

const fetchRows = async (page = 1, options: ListFetchOptions = {}) => {
  if (!options.preserveRows) loading.value = true
  try {
    const res = await adminAPI.getCdkIssueRequests({ ...buildParams(true), page, page_size: pagination.page_size })
    rows.value = (res.data?.data as AdminCdkIssueRequest[]) || []
    const p = res.data?.pagination
    if (p) Object.assign(pagination, p)
  } catch {
    if (!options.preserveRows) rows.value = []
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  fetchRows(1, { preserveRows: true })
  fetchStats()
}
const debouncedSearch = useDebounceFn(handleSearch, 300)

const selectStatus = (status: string) => {
  filters.status = filters.status === status ? '__all__' : status
  fetchRows(1, { preserveRows: true })
}

const changePage = (page: number) => {
  if (page < 1 || page > pagination.total_page) return
  fetchRows(page)
}

const changePageSize = (size: number) => {
  if (size === pagination.page_size) return
  pagination.page_size = size
  fetchRows(1)
}

const refresh = () => {
  refreshList(async () => {
    await Promise.all([fetchRows(pagination.page, { preserveRows: true }), fetchStats()])
  })
}

const { retryingId, retry } = useCdkIssueRetry(async () => {
  await Promise.all([fetchRows(pagination.page, { preserveRows: true }), fetchStats()])
})

const orderLink = (row: AdminCdkIssueRequest) => adminUrl(`/orders?order_id=${row.root_order_id || row.order_id}`)

onMounted(() => {
  fetchRows()
  fetchStats()
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('admin.cdkIssue.requests.title') }}</h1>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('admin.cdkIssue.requests.subtitle') }}</p>
      </div>
      <Button size="sm" variant="outline" class="w-full sm:w-auto" :disabled="refreshing" @click="refresh">
        {{ t('admin.common.refresh') }}
      </Button>
    </div>

    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
      <button
        v-for="card in statCards"
        :key="card.value"
        type="button"
        class="rounded-xl border bg-card p-4 text-left transition-all hover:shadow-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        :class="statCardClass(card.tone, filters.status === card.value)"
        @click="selectStatus(card.value)"
      >
        <div class="text-xs font-medium" :class="statTextClass(card.tone)">
          {{ card.value === '__all__' ? t('admin.cdkIssue.requests.statTotal') : cdkIssueStatusLabel(t, card.value) }}
        </div>
        <div class="mt-1 text-2xl font-bold" :class="statTextClass(card.tone)">{{ stats[card.key] }}</div>
      </button>
    </div>

    <div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-end">
      <div class="w-full sm:w-auto">
        <label class="mb-1.5 block text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.requests.columns.status') }}</label>
        <Select v-model="filters.status" @update:model-value="handleSearch">
          <SelectTrigger class="h-9 w-full sm:w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{{ t('admin.cdkIssue.requests.allStatus') }}</SelectItem>
            <SelectItem v-for="status in CDK_ISSUE_STATUSES" :key="status" :value="status">{{ cdkIssueStatusLabel(t, status) }}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="w-full sm:w-auto">
        <label class="mb-1.5 block text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.requests.columns.plan') }}</label>
        <Input v-model="filters.plan" class="h-9 w-full sm:w-36" placeholder="pro_5x" @update:model-value="debouncedSearch" />
      </div>
      <div class="w-full sm:w-auto">
        <label class="mb-1.5 block text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.requests.columns.orderNo') }}</label>
        <Input v-model="filters.order_no" class="h-9 w-full sm:w-44" :placeholder="t('admin.cdkIssue.requests.orderNoPlaceholder')" @update:model-value="debouncedSearch" @keyup.enter="handleSearch" />
      </div>
      <div class="w-full sm:w-auto">
        <label class="mb-1.5 block text-xs font-medium text-muted-foreground">{{ t('admin.cdkIssue.requests.dateRange') }}</label>
        <div class="flex flex-col gap-1.5 sm:flex-row sm:items-center">
          <Input v-model="filters.created_from" type="datetime-local" class="h-9 w-full sm:w-44" @change="handleSearch" />
          <span class="hidden text-xs text-muted-foreground sm:inline">—</span>
          <Input v-model="filters.created_to" type="datetime-local" class="h-9 w-full sm:w-44" @change="handleSearch" />
        </div>
      </div>
      <Button size="sm" class="h-9 w-full sm:w-auto" @click="handleSearch">{{ t('admin.cdkIssue.requests.search') }}</Button>
    </div>

    <div class="overflow-hidden rounded-xl border border-border bg-card">
      <TableSkeleton v-if="loading" :columns="8" :rows="5" />
      <div v-else-if="!rows.length" class="p-12 text-center text-muted-foreground">{{ t('admin.cdkIssue.requests.empty') }}</div>
      <div v-else class="overflow-x-auto">
        <Table class="min-w-[1080px]">
          <TableHeader class="bg-muted/40 text-xs text-muted-foreground">
            <TableRow>
              <TableHead class="px-4 py-3">{{ t('admin.cdkIssue.requests.columns.orderNo') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cdkIssue.requests.columns.provider') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cdkIssue.requests.columns.item') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cdkIssue.requests.columns.plan') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cdkIssue.requests.columns.count') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cdkIssue.requests.columns.status') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cdkIssue.requests.columns.error') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cdkIssue.requests.columns.fee') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cdkIssue.requests.columns.updatedAt') }}</TableHead>
              <TableHead class="px-4 py-3 text-right">{{ t('admin.cdkIssue.requests.columns.actions') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody class="divide-y divide-border">
            <TableRow
              v-for="row in rows"
              :key="row.id"
              :class="cdkIssueNeedsAttention(row.status) ? 'bg-red-50/40 hover:bg-red-50/60' : 'hover:bg-muted/30'"
            >
              <TableCell class="px-4 py-3">
                <a :href="orderLink(row)" target="_blank" rel="noopener" class="font-mono text-xs text-primary underline-offset-4 hover:underline">
                  {{ row.order_no }} ↗
                </a>
              </TableCell>
              <TableCell class="px-4 py-3 text-xs">
                <div>{{ t(`admin.cdkIssue.providers.${row.provider || 'zovocard'}`) }}</div>
                <div
                  v-if="row.provider_request_id"
                  class="font-mono text-[11px] text-muted-foreground"
                  :title="t('admin.cdkIssue.requests.providerRequestId')"
                >
                  {{ row.provider_request_id }}
                </div>
              </TableCell>
              <TableCell class="max-w-[200px] px-4 py-3 text-xs">
                <div class="truncate">{{ getLocalizedText(row.item_title) || `#${row.product_id}` }}</div>
                <div v-if="row.chunk_index > 0" class="text-muted-foreground">{{ t('admin.cdkIssue.requests.chunk', { n: row.chunk_index + 1 }) }}</div>
              </TableCell>
              <TableCell class="px-4 py-3 font-mono text-xs">{{ row.plan }} · {{ row.payment_country }}</TableCell>
              <TableCell class="px-4 py-3 text-xs">{{ row.count }}</TableCell>
              <TableCell class="px-4 py-3">
                <span class="rounded-full border px-2 py-0.5 text-xs" :class="cdkIssueStatusClass(row.status)">{{ cdkIssueStatusLabel(t, row.status) }}</span>
                <div class="mt-1 text-[11px] text-muted-foreground">{{ t('admin.cdkIssue.requests.attempts', { n: row.attempts }) }}</div>
              </TableCell>
              <TableCell class="max-w-[240px] px-4 py-3 text-xs">
                <template v-if="row.last_error_code || row.last_error_msg">
                  <div class="font-mono text-red-700">{{ row.last_error_code || '-' }}<span v-if="row.last_http_status"> (HTTP {{ row.last_http_status }})</span></div>
                  <div class="truncate text-muted-foreground" :title="row.last_error_msg">{{ row.last_error_msg }}</div>
                </template>
                <span v-else class="text-muted-foreground">-</span>
              </TableCell>
              <TableCell class="px-4 py-3 text-xs">{{ formatUsdMinor(row.fee_amount_minor) }}</TableCell>
              <TableCell class="px-4 py-3 text-xs text-muted-foreground">{{ formatDate(row.updated_at) }}</TableCell>
              <TableCell class="px-4 py-3 text-right">
                <Button
                  v-if="row.retry_mode"
                  size="xs"
                  :variant="row.retry_mode === 'same_key' ? 'destructive' : 'outline'"
                  :disabled="retryingId === row.id"
                  @click="retry(row)"
                >
                  {{ row.retry_mode === 'same_key' ? t('admin.cdkIssue.retry.sameKey') : t('admin.cdkIssue.retry.newKey') }}
                </Button>
                <span v-else class="text-xs text-muted-foreground">-</span>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </div>

    <ListPagination
      :page="pagination.page"
      :total-page="pagination.total_page"
      :total="pagination.total"
      :page-size="pagination.page_size"
      :page-size-options="pageSizeOptions"
      @change-page="changePage"
      @change-page-size="changePageSize"
    />
  </div>
</template>
