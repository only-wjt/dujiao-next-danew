import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '../stores/app'
import { sanitizeRichTextHtml } from '../utils/content'
import { usePageSeo } from './usePageSeo'

/**
 * 条款/隐私页共享逻辑（classic + vault 双模板共用）。
 * 完整保留原 views/Legal.vue 的行为，仅抽离为 composable。
 * type 以 getter 传入以保持响应性（来源于组件 props）。
 */
export function useLegal(type: () => 'terms' | 'privacy') {
  const { t } = useI18n()
  const appStore = useAppStore()

  usePageSeo({
    title: () => type() === 'terms' ? t('footer.terms') : t('footer.privacy'),
    canonicalPath: () => type() === 'terms' ? '/terms' : '/privacy',
  })

  const loading = computed(() => appStore.loading)
  const locale = computed(() => appStore.locale)

  const title = computed(() => {
    return type() === 'terms' ? t('footer.terms') : t('footer.privacy')
  })

  // 条款与隐私政策由后台富文本编辑器维护，前端直接 v-html 渲染（classic 与 vault
  // 模板共用本 composable），因此在这里统一净化，两个模板都无需各自处理。
  const content = computed(() => {
    const config = appStore.config
    if (!config?.legal) return ''

    const legal = config.legal
    const lang = locale.value

    if (type() === 'terms' && legal.terms) {
      return sanitizeRichTextHtml(legal.terms[lang] || '')
    } else if (type() === 'privacy' && legal.privacy) {
      return sanitizeRichTextHtml(legal.privacy[lang] || '')
    }
    return ''
  })

  return {
    loading,
    title,
    content,
  }
}
