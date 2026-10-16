<template>
  <div v-if="visible" class="rounded-xl border px-4 py-3">
    <p class="text-xs font-semibold uppercase tracking-[0.14em] text-muted-foreground">{{ t('auth.common.captchaLabel') }}</p>
    <p class="mt-1 text-xs text-muted-foreground">{{ t('guestOrders.captchaHint') }}</p>
    <div class="mt-2">
      <ImageCaptcha
        v-if="provider === 'image'"
        ref="imageRef"
        v-model="imagePayload"
        @config-stale="handleConfigStale"
      />
      <TurnstileCaptcha
        v-else-if="provider === 'turnstile'"
        ref="turnstileRef"
        v-model="turnstileToken"
        :site-key="siteKey"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CaptchaPayload } from '../../api'
import { setGuestLookupCaptcha } from '../../api'
import ImageCaptcha from '../captcha/ImageCaptcha.vue'
import TurnstileCaptcha from '../captcha/TurnstileCaptcha.vue'
import { useAppStore } from '../../stores/app'

/**
 * 游客查单人机验证。仅在后端返回需要验证（428）后由父组件显示，
 * 验证结果通过 setGuestLookupCaptcha 写入游客请求头。
 */
const props = defineProps<{
  required: boolean
  // 每次后端再次要求验证时递增，用于刷新一次性验证码。
  attempt?: number
}>()

const { t } = useI18n()
const appStore = useAppStore()
const imagePayload = ref<CaptchaPayload>({})
const turnstileToken = ref('')
const imageRef = ref<InstanceType<typeof ImageCaptcha> | null>(null)
const turnstileRef = ref<InstanceType<typeof TurnstileCaptcha> | null>(null)

const captchaConfig = computed(() => appStore.config?.captcha || null)
const provider = computed(() => String(captchaConfig.value?.provider || 'none'))
const siteKey = computed(() => String(captchaConfig.value?.turnstile?.site_key || ''))
const visible = computed(() => props.required && provider.value !== 'none')

watch([imagePayload, turnstileToken], () => {
  if (provider.value === 'image') {
    setGuestLookupCaptcha(imagePayload.value)
  } else if (provider.value === 'turnstile') {
    setGuestLookupCaptcha({ turnstile_token: turnstileToken.value })
  }
}, { deep: true })

watch(() => props.required, (required) => {
  if (!required) setGuestLookupCaptcha(null)
})

watch(() => props.attempt, () => {
  if (props.required) reset()
})

const handleConfigStale = async () => {
  await appStore.loadConfig(true)
  imagePayload.value = {}
}

/** 每次提交后重置：验证码只能使用一次。 */
const reset = () => {
  imagePayload.value = {}
  turnstileToken.value = ''
  setGuestLookupCaptcha(null)
  imageRef.value?.refresh()
  turnstileRef.value?.reset()
}

defineExpose({ reset })
</script>
