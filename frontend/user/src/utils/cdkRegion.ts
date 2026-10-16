/** 订单项快照中实时发码付款地区的读取与展示。 */

const KNOWN_REGIONS = ['PH', 'US', 'JP', 'CL', 'EG'] as const

/** 读取订单项上 ZovoCard 实时发码的付款地区；非实时发码返回空。 */
export const itemPaymentRegion = (item: any): string => {
  const snapshot = item?.sku_snapshot?.cdk_issue
  if (!snapshot || typeof snapshot !== 'object') return ''
  return String(snapshot.payment_country || '').trim().toUpperCase()
}

/** 交付内容对应的付款地区（一个交付对应一个子订单；多个地区时返回去重后的列表）。 */
export const orderPaymentRegions = (items?: any[] | null): string[] => {
  const regions = (items || []).map(itemPaymentRegion).filter(Boolean)
  return Array.from(new Set(regions))
}

/** 地区文案的 i18n 键；未知地区直接显示国家代码。 */
export const regionLabelKey = (code: string): string | null =>
  (KNOWN_REGIONS as readonly string[]).includes(code) ? `orderDetail.regions.${code}` : null
