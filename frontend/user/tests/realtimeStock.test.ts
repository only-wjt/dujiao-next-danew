import test from 'node:test'
import assert from 'node:assert/strict'
import {
  normalizeAutoStockSnapshot,
  resolveSkuAvailableStock,
  resolveSkuStockDisplay,
} from '../src/utils/publicStock.ts'
import { isOrderIssuing } from '../src/composables/useDeliveryPolling.ts'

test('realtime auto stock (-1) is unlimited on the product page', () => {
  const product = { fulfillment_type: 'auto', stock_display_mode: 'exact' }
  const sku = { auto_stock_available: -1, stock_status: 'unlimited', stock_display: 'exact' }

  assert.equal(resolveSkuAvailableStock(product, sku), null)
  assert.deepEqual(resolveSkuStockDisplay(product, sku), { kind: 'unlimited' })
})

test('cart snapshot keeps -1 for unlimited auto stock and clamps the rest', () => {
  assert.equal(normalizeAutoStockSnapshot(-1), -1)
  assert.equal(normalizeAutoStockSnapshot(-5), -1)
  assert.equal(normalizeAutoStockSnapshot(3.7), 3)
  assert.equal(normalizeAutoStockSnapshot('abc'), 0)
})

test('order is issuing when the order or any child is issuing', () => {
  assert.equal(isOrderIssuing(null), false)
  assert.equal(isOrderIssuing({ delivery_state: 'issuing' }), true)
  assert.equal(isOrderIssuing({ delivery_state: 'delayed' }), false)
  assert.equal(isOrderIssuing({ children: [{}, { delivery_state: 'issuing' }] }), true)
  assert.equal(isOrderIssuing({ children: [{ delivery_state: 'delayed' }] }), false)
})

test('payment region is read from the realtime issue snapshot only', async () => {
  const { itemPaymentRegion, orderPaymentRegions, regionLabelKey } = await import('../src/utils/cdkRegion.ts')
  const ph = { sku_snapshot: { cdk_issue: { plan: 'plus', payment_country: 'ph' } } }
  const stock = { sku_snapshot: { sku_code: 'A' } }
  assert.equal(itemPaymentRegion(ph), 'PH')
  assert.equal(itemPaymentRegion(stock), '')
  assert.deepEqual(orderPaymentRegions([ph, ph, stock]), ['PH'])
  assert.deepEqual(orderPaymentRegions(null), [])
  assert.equal(regionLabelKey('US'), 'orderDetail.regions.US')
  assert.equal(regionLabelKey('XX'), null)
})
