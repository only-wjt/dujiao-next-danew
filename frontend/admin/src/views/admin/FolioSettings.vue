<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { notifyError, notifySuccess } from '@/utils/notify'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)

const form = reactive({
  base_url: '',
  secret: '',
  has_secret: false,
  enabled: false,
})

const statusText = computed(() => {
  if (form.enabled) return t('admin.settings.folio.statusEnabled')
  return t('admin.settings.folio.statusDisabled')
})

const fetchSettings = async () => {
  loading.value = true
  try {
    const res = await adminAPI.getFolioSettings()
    const data = res.data?.data as Record<string, unknown> | undefined
    if (data) {
      form.base_url = String(data.base_url || '')
      form.secret = ''
      form.has_secret = Boolean(data.has_secret)
      form.enabled = Boolean(data.enabled)
    }
  } catch (err: any) {
    notifyError(err?.response?.data?.message || t('admin.settings.alerts.loadFailed'))
  } finally {
    loading.value = false
  }
}

const saveSettings = async () => {
  saving.value = true
  try {
    const payload: Record<string, unknown> = {
      base_url: form.base_url.trim(),
    }
    if (form.secret.trim()) {
      payload.secret = form.secret.trim()
    }
    const res = await adminAPI.updateFolioSettings(payload)
    const data = res.data?.data as Record<string, unknown> | undefined
    if (data) {
      form.base_url = String(data.base_url || '')
      form.secret = ''
      form.has_secret = Boolean(data.has_secret)
      form.enabled = Boolean(data.enabled)
    }
    notifySuccess(t('admin.settings.alerts.saveSuccess'))
  } catch (err: any) {
    notifyError(err?.response?.data?.message || t('admin.settings.alerts.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(fetchSettings)
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('admin.settings.folio.title') }}</h1>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('admin.settings.folio.subtitle') }}</p>
      </div>
      <Button size="sm" class="w-full sm:w-auto" :disabled="loading || saving" @click="saveSettings">
        {{ saving ? t('admin.settings.actions.saving') : t('admin.settings.actions.save') }}
      </Button>
    </div>

    <div class="rounded-xl border border-border bg-card">
      <div class="space-y-6 p-6">
        <div class="rounded-lg border border-border bg-muted/30 px-4 py-3 text-sm">
          <span class="text-muted-foreground">{{ t('admin.settings.folio.statusLabel') }}：</span>
          <span :class="form.enabled ? 'text-emerald-600' : 'text-muted-foreground'">{{ statusText }}</span>
        </div>

        <div class="rounded-lg border border-amber-500/30 bg-amber-500/5 p-4">
          <p class="text-xs text-amber-600 dark:text-amber-400">{{ t('admin.settings.folio.hint') }}</p>
        </div>

        <div class="grid grid-cols-1 gap-6">
          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.folio.baseUrl') }}</label>
            <Input
              v-model="form.base_url"
              type="text"
              :disabled="loading || saving"
              :placeholder="t('admin.settings.folio.baseUrlPlaceholder')"
            />
            <p class="text-xs text-muted-foreground">{{ t('admin.settings.folio.baseUrlHint') }}</p>
          </div>

          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.folio.secret') }}</label>
            <Input
              v-model="form.secret"
              type="password"
              autocomplete="new-password"
              :disabled="loading || saving"
              :placeholder="form.has_secret ? t('admin.settings.folio.secretKeepPlaceholder') : t('admin.settings.folio.secretPlaceholder')"
            />
            <p class="text-xs text-muted-foreground">
              {{ form.has_secret ? t('admin.settings.folio.secretConfigured') : t('admin.settings.folio.secretHint') }}
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
