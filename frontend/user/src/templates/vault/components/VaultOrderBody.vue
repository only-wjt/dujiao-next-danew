<template>
  <div class="grid gap-[18px]">
    <DeliveryReadyBanner :order="order" />

    <!-- 子订单（含卡密） -->
    <section v-if="order.children && order.children.length > 0" class="rounded-xl border bg-card p-[22px]">
      <h2 class="mb-4 text-lg font-bold">{{ t('orderDetail.childOrdersTitle') }}</h2>
      <div class="grid gap-4">
        <div v-for="child in order.children" :key="child.id || child.order_no" class="rounded-lg border p-4">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <div class="text-[13px] text-muted-foreground">{{ t('orderDetail.childOrderNo') }}：{{ child.order_no }}</div>
              <div class="text-[13px] text-muted-foreground">{{ t('orderDetail.childOrderAmount') }}：<span class="font-semibold text-foreground">{{ formatMoney(orderPaidAmount(child), child.currency || order.currency) }}</span></div>
            </div>
            <Badge :variant="statusVariant(resolvedChildStatus(child))" size="sm" class="rounded-full">{{ statusLabel(resolvedChildStatus(child)) }}</Badge>
          </div>

          <div class="mt-4 scroll-mt-24" :data-delivery-anchor="child.fulfillment?.status === 'delivered' ? '' : undefined">
            <VaultOrderFulfillment
              v-if="child.fulfillment"
              :fulfillment="child.fulfillment"
              :items="child.items"
              :order-no="child.order_no || order.order_no"
              :downloading="fulfillmentDownloading"
              @download="emit('download', $event)"
            />
            <div v-else class="rounded-md border border-dashed px-3 py-2.5 text-[13px] text-muted-foreground">{{ t('orderDetail.childFulfillmentEmpty') }}</div>
          </div>

          <h3 class="my-2.5 mt-5 border-t pt-4 text-sm font-bold">{{ t('orderDetail.childItemsTitle') }}</h3>
          <div v-if="child.items && child.items.length" class="grid">
            <VaultOrderItem
              v-for="(item, cidx) in child.items"
              :key="cidx"
              :item="item"
              :currency="child.currency || order.currency"
              :invoice-fee="child.items.length === 1 ? child.invoice_fee_amount : undefined"
              :payment-fee="child.items.length === 1 ? child.payment_fee_amount : undefined"
            />
          </div>
          <div v-else class="text-[13px] text-muted-foreground">{{ t('orderDetail.noItems') }}</div>
        </div>
      </div>
    </section>

    <!-- 主订单发货（含卡密） -->
    <section v-if="order.fulfillment" class="scroll-mt-24 rounded-xl border bg-card p-[22px]" :data-delivery-anchor="order.fulfillment.status === 'delivered' ? '' : undefined">
      <h2 class="mb-4 text-lg font-bold">{{ t('orderDetail.fulfillmentTitle') }}</h2>
      <VaultOrderFulfillment
        :fulfillment="order.fulfillment"
        :items="order.items"
        :order-no="order.order_no"
        :downloading="fulfillmentDownloading"
        @download="emit('download', $event)"
      />
    </section>

    <!-- 商品 -->
    <section class="rounded-xl border bg-card p-[22px]">
      <h2 class="mb-4 text-lg font-bold">{{ t('orderDetail.itemsTitle') }}</h2>
      <div v-if="order.items && order.items.length > 0" class="grid">
        <VaultOrderItem v-for="(item, idx) in order.items" :key="idx" :item="item" :currency="order.currency" />
      </div>
      <div v-else class="text-[13px] text-muted-foreground">{{ t('orderDetail.noItems') }}</div>
    </section>

    <!-- 金额明细 -->
    <section class="rounded-xl border bg-card p-[22px]">
      <h2 class="mb-4 text-lg font-bold">{{ t('orderDetail.amountTitle') }}</h2>
      <div class="grid gap-3 grid-cols-[repeat(auto-fill,minmax(180px,1fr))]">
        <div class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.amountOriginal') }}</div>
          <div class="mt-1.5 font-bold tabular-nums">{{ formatMoney(order.original_amount, order.currency) }}</div>
        </div>
        <div class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.amountDiscount') }}</div>
          <div class="mt-1.5 font-bold tabular-nums" :class="{ 'text-destructive': hasDiscountAmount(order.discount_amount) }">{{ formatDiscountMoney(order.discount_amount, order.currency) }}</div>
        </div>
        <div v-if="hasAmount(order.invoice_fee_amount)" class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.invoiceFeeLabel') }}</div>
          <div class="mt-1.5 font-bold tabular-nums">+{{ formatMoney(order.invoice_fee_amount, order.currency) }}</div>
        </div>
        <div v-if="hasAmount(order.payment_fee_amount)" class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.paymentFeeLabel') }}</div>
          <div class="mt-1.5 font-bold tabular-nums">+{{ formatMoney(order.payment_fee_amount, order.currency) }}</div>
        </div>
        <div class="rounded-md border border-primary/40 bg-primary/5 px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.amountTotal') }}</div>
          <div class="mt-1.5 font-bold tabular-nums">{{ formatMoney(orderPaidAmount(order), order.currency) }}</div>
        </div>
        <template v-if="variant === 'user'">
          <div v-if="hasAmount(order.wallet_paid_amount)" class="rounded-md border bg-secondary px-3.5 py-3">
            <div class="text-xs text-muted-foreground">{{ t('orderDetail.amountWalletPaid') }}</div>
            <div class="mt-1.5 font-bold tabular-nums">{{ formatMoney(order.wallet_paid_amount, order.currency) }}</div>
          </div>
          <div v-if="hasAmount(order.online_paid_amount)" class="rounded-md border bg-secondary px-3.5 py-3">
            <div class="text-xs text-muted-foreground">{{ t('orderDetail.amountOnlinePaid') }}</div>
            <div class="mt-1.5 font-bold tabular-nums">{{ formatMoney(order.online_paid_amount, order.currency) }}</div>
          </div>
          <div v-if="hasAmount(order.refunded_amount)" class="rounded-md border bg-secondary px-3.5 py-3">
            <div class="text-xs text-muted-foreground">{{ t('orderDetail.amountRefunded') }}</div>
            <div class="mt-1.5 font-bold tabular-nums">{{ formatMoney(order.refunded_amount, order.currency) }}</div>
          </div>
        </template>
        <div v-if="hasDiscountAmount(order.member_discount_amount)" class="rounded-md border border-[color:var(--gold-strong)] bg-secondary px-3.5 py-3 text-[color:var(--gold-strong)]">
          <div class="text-xs">{{ t('orderDetail.amountMemberDiscount') }}</div>
          <div class="mt-1.5 font-bold tabular-nums">{{ formatDiscountMoney(order.member_discount_amount, order.currency) }}</div>
        </div>
        <div v-if="hasDiscountAmount(order.wholesale_discount_amount)" class="rounded-md border border-[color:var(--teal-strong)] bg-secondary px-3.5 py-3 text-[color:var(--teal-strong)]">
          <div class="text-xs">{{ t('orderDetail.amountWholesaleDiscount') }}</div>
          <div class="mt-1.5 font-bold tabular-nums">{{ formatDiscountMoney(order.wholesale_discount_amount, order.currency) }}</div>
        </div>
      </div>
    </section>

    <!-- 开票状态 -->
    <section v-if="order.invoice_requested" class="rounded-xl border bg-card p-[22px]">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 class="text-lg font-bold">{{ t('orderDetail.invoiceTitle') }}</h2>
        <a
          v-if="order.invoice_pdf_url"
          :href="order.invoice_pdf_url"
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex items-center rounded-md border bg-secondary px-3.5 py-2 text-sm font-bold hover:opacity-80"
        >{{ t('orderDetail.invoiceDownload') }}</a>
      </div>
      <div class="grid gap-3 grid-cols-[repeat(auto-fill,minmax(180px,1fr))]">
        <div class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.invoiceIssueStatus') }}</div>
          <div class="mt-1.5 font-bold">
            {{ order.invoice_status === 'issued' || order.invoice_no ? t('orderDetail.invoiceIssued') : t('orderDetail.invoiceNotIssued') }}
          </div>
        </div>
        <div v-if="order.invoice_no" class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.invoiceNo') }}</div>
          <div class="mt-1.5 font-bold tabular-nums break-all">{{ order.invoice_no }}</div>
        </div>
        <div v-if="order.invoice_title" class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.invoiceName') }}</div>
          <div class="mt-1.5 font-bold break-all">{{ order.invoice_title }}</div>
        </div>
        <div v-if="order.invoice_email" class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.invoiceEmail') }}</div>
          <div class="mt-1.5 font-bold break-all">{{ order.invoice_email }}</div>
        </div>
      </div>
    </section>

    <!-- 退款记录 -->
    <section v-if="showRefundRecordsCard" class="rounded-xl border bg-card p-[22px]">
      <h2 class="mb-4 text-lg font-bold">{{ t('orderDetail.refundRecordsTitle') }}</h2>
      <div v-if="refundRecords.length > 0" class="overflow-hidden rounded-md border">
        <div class="grid grid-cols-[1.2fr_1fr_1.6fr] gap-3 bg-secondary px-3.5 py-2.5 text-xs font-bold text-muted-foreground max-[640px]:hidden">
          <span>{{ t('orderDetail.refundRecordTime') }}</span>
          <span>{{ t('orderDetail.refundRecordAmount') }}</span>
          <span>{{ t('orderDetail.refundRecordReason') }}</span>
        </div>
        <div v-for="(record, idx) in refundRecords" :key="`refund-${idx}`" class="grid grid-cols-[1.2fr_1fr_1.6fr] gap-3 border-t px-3.5 py-2.5 text-[13px] max-[640px]:grid-cols-1 max-[640px]:gap-1">
          <span class="text-muted-foreground">{{ formatDate(record.created_at) }}</span>
          <span class="tabular-nums">{{ formatMoney(record.amount, record.currency || order.currency) }}</span>
          <span class="whitespace-pre-wrap break-words text-muted-foreground">{{ refundReasonText(record.remark) }}</span>
        </div>
      </div>
      <div v-else class="text-[13px] text-muted-foreground">{{ t('orderDetail.refundRecordsEmpty') }}</div>
    </section>

    <!-- 时间信息 -->
    <section v-if="showTimeCard" class="rounded-xl border bg-card p-[22px]">
      <h2 class="mb-4 text-lg font-bold">{{ t('orderDetail.timeTitle') }}</h2>
      <div class="grid gap-3 grid-cols-[repeat(auto-fill,minmax(180px,1fr))]">
        <div class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.createdAtLabel') }}</div>
          <div class="mt-1.5 font-bold">{{ formatDate(order.created_at) }}</div>
        </div>
        <div v-if="order.paid_at" class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.paidAtLabel') }}</div>
          <div class="mt-1.5 font-bold">{{ formatDate(order.paid_at) }}</div>
        </div>
        <div v-if="order.expires_at" class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.expiresAtLabel') }}</div>
          <div class="mt-1.5 font-bold">{{ formatDate(order.expires_at) }}</div>
        </div>
        <div v-if="order.canceled_at" class="rounded-md border bg-secondary px-3.5 py-3">
          <div class="text-xs text-muted-foreground">{{ t('orderDetail.canceledAtLabel') }}</div>
          <div class="mt-1.5 font-bold">{{ formatDate(order.canceled_at) }}</div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { toRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui/badge'
import DeliveryReadyBanner from '../../../components/order/DeliveryReadyBanner.vue'
import VaultOrderItem from './VaultOrderItem.vue'
import VaultOrderFulfillment from './VaultOrderFulfillment.vue'
import { useOrderDisplayHelpers } from '../../../composables/useOrderDisplayHelpers'

const props = defineProps<{
  order: any
  variant: 'user' | 'guest'
  fulfillmentDownloading: boolean
}>()

const emit = defineEmits<{
  (e: 'download', orderNo: string): void
}>()

const { t } = useI18n()

const {
  showTimeCard, showRefundRecordsCard, refundRecords, resolvedChildStatus,
  statusLabel, statusVariant, formatDate, refundReasonText,
  formatMoney, formatDiscountMoney, hasDiscountAmount, hasAmount, orderPaidAmount,
} = useOrderDisplayHelpers(toRef(props, 'order'))
</script>
