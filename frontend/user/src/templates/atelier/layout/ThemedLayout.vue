<template>
  <div :class="`${theme}-scope`">
    <header class="sticky top-0 z-50 border-b border-[color:var(--border)] bg-[color:var(--surface)]/92 backdrop-blur-md">
      <div class="mx-auto flex h-[68px] w-full max-w-[1180px] items-center gap-3 px-4 sm:gap-6 sm:px-6">
        <RouterLink class="inline-flex min-w-0 items-center gap-2.5 text-[20px] font-bold tracking-[0.04em] text-foreground" style="font-family: var(--font-head)" to="/" :title="brandName">
          <img v-if="brandLogo" :src="brandLogo" :alt="brandName" class="h-8 max-w-[120px] object-contain sm:max-w-[160px]" />
          <span v-else class="truncate">{{ brandName }}</span>
        </RouterLink>

        <nav class="flex gap-1 max-[900px]:hidden">
          <template v-for="item in menuItems" :key="item.key">
            <RouterLink
              v-if="item.type === 'route'"
              :to="item.path"
              class="whitespace-nowrap border-b-2 border-transparent px-2.5 py-2 text-[14px] font-medium text-muted-foreground transition-colors hover:text-foreground"
              active-class="!border-[color:var(--red)] !text-primary"
            >{{ item.label }}</RouterLink>
            <a
              v-else
              :href="item.path"
              :target="item.target"
              rel="noopener noreferrer"
              class="whitespace-nowrap border-b-2 border-transparent px-2.5 py-2 text-[14px] font-medium text-muted-foreground transition-colors hover:text-foreground"
            >{{ item.label }}</a>
          </template>
        </nav>

        <div class="ml-auto flex items-center gap-1.5">
          <RouterLink class="grid h-9 w-9 flex-none place-items-center rounded-sm text-muted-foreground transition-colors hover:bg-secondary hover:text-primary" to="/products" :aria-label="t('nav.products')"><Search class="h-[17px] w-[17px]" /></RouterLink>
          <RouterLink v-if="!userAuthStore.isAuthenticated" class="grid h-9 w-9 flex-none place-items-center rounded-sm text-muted-foreground transition-colors hover:bg-secondary hover:text-primary" to="/guest/orders" :aria-label="t('navbar.guestOrders')" :title="t('navbar.guestOrders')"><ClipboardList class="h-[17px] w-[17px]" /></RouterLink>
          <button class="grid h-9 w-9 flex-none place-items-center rounded-sm text-muted-foreground transition-colors hover:bg-secondary hover:text-primary" type="button" :aria-label="t('resellerConsole.common.toggleTheme')" @click="toggleTheme">
            <Sun v-if="themeMode === 'dark'" class="h-[17px] w-[17px]" />
            <Moon v-else class="h-[17px] w-[17px]" />
          </button>
          <RouterLink class="relative grid h-9 w-9 flex-none place-items-center rounded-sm text-muted-foreground transition-colors hover:bg-secondary hover:text-primary" to="/cart" :aria-label="t('navbar.cart')">
            <ShoppingCart class="h-[17px] w-[17px]" />
            <span v-if="cartCount > 0" class="absolute -right-0.5 -top-0.5 grid h-[17px] min-w-[17px] place-items-center rounded-sm bg-primary px-1 text-[10px] font-bold text-primary-foreground">{{ cartCount }}</span>
          </RouterLink>

          <div class="relative max-[900px]:hidden" ref="langEl">
            <button class="grid h-9 w-9 flex-none place-items-center rounded-sm text-muted-foreground transition-colors hover:bg-secondary hover:text-primary" type="button" :aria-label="t('navbar.selectLanguage')" @click="toggleLang">
              <Languages class="h-[17px] w-[17px]" />
            </button>
            <div v-if="langOpen" class="absolute right-0 top-[calc(100%+8px)] z-[60] flex min-w-[168px] flex-col gap-0.5 rounded-sm border bg-card p-2 shadow-[var(--shadow-lg)]">
              <span class="px-3 pb-0.5 pt-1.5 text-[11px] font-bold uppercase tracking-[0.08em] text-muted-foreground">{{ t('navbar.selectLanguage') }}</span>
              <button v-for="lang in languages" :key="lang.code" class="flex w-full items-center justify-between gap-2.5 rounded-sm px-3 py-2.5 text-left text-sm font-medium transition-colors hover:bg-secondary hover:text-foreground" :class="appStore.locale === lang.code ? 'text-primary' : 'text-muted-foreground'" @click="changeLanguage(lang.code)">
                {{ lang.name }}
                <span v-if="appStore.locale === lang.code" class="h-[6px] w-[6px] rounded-sm bg-primary"></span>
              </button>
            </div>
          </div>

          <template v-if="userAuthStore.isAuthenticated">
            <RouterLink class="inline-flex items-center gap-2 rounded-sm border border-hairline-strong px-3 py-1.5 text-[13px] font-medium text-foreground transition-colors hover:border-primary max-[900px]:hidden" to="/me"><User class="h-[16px] w-[16px]" /> {{ t('navbar.personalCenter') }}</RouterLink>
            <button type="button" class="grid h-9 w-9 flex-none place-items-center rounded-sm text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive max-[900px]:hidden" :aria-label="t('navbar.logout')" :title="t('navbar.logout')" @click="userAuthStore.logout()"><LogOut class="h-[17px] w-[17px]" /></button>
          </template>
          <RouterLink v-else class="inline-flex items-center gap-2 rounded-sm bg-primary px-3.5 py-1.5 text-[13px] font-medium text-primary-foreground transition-colors hover:bg-primary/90 max-[900px]:hidden" to="/auth/login">{{ t('navbar.login') }}</RouterLink>

          <div class="relative hidden max-[900px]:block" ref="moreEl">
            <button class="grid h-9 w-9 flex-none place-items-center rounded-sm text-muted-foreground transition-colors hover:bg-secondary hover:text-primary" type="button" :aria-label="t('navbar.more')" @click="toggleMore">
              <Menu v-if="!moreOpen" class="h-[17px] w-[17px]" />
              <X v-else class="h-[17px] w-[17px]" />
            </button>
            <div v-if="moreOpen" class="absolute right-0 top-[calc(100%+8px)] z-[60] flex min-w-[168px] flex-col gap-0.5 rounded-sm border bg-card p-2 shadow-[var(--shadow-lg)]">
              <template v-for="item in menuItems" :key="`m-${item.key}`">
                <RouterLink v-if="item.type === 'route'" :to="item.path" class="flex w-full items-center gap-2.5 rounded-sm px-3 py-2.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground" @click="moreOpen = false">{{ item.label }}</RouterLink>
                <a v-else :href="item.path" :target="item.target" rel="noopener noreferrer" class="flex w-full items-center gap-2.5 rounded-sm px-3 py-2.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground" @click="moreOpen = false">{{ item.label }}</a>
              </template>
              <RouterLink v-if="!userAuthStore.isAuthenticated" to="/guest/orders" class="flex w-full items-center gap-2.5 rounded-sm px-3 py-2.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground" @click="moreOpen = false">{{ t('navbar.guestOrders') }}</RouterLink>
              <RouterLink v-if="userAuthStore.isAuthenticated" to="/me" class="flex w-full items-center gap-2.5 rounded-sm px-3 py-2.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground" @click="moreOpen = false">{{ t('navbar.personalCenter') }}</RouterLink>
              <RouterLink v-else to="/auth/login" class="flex w-full items-center gap-2.5 rounded-sm px-3 py-2.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground" @click="moreOpen = false">{{ t('navbar.login') }}</RouterLink>
              <button v-if="userAuthStore.isAuthenticated" class="flex w-full items-center gap-2.5 rounded-sm px-3 py-2.5 text-left text-sm font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground" @click="userAuthStore.logout(); moreOpen = false">{{ t('navbar.logout') }}</button>
              <span class="px-3 pb-0.5 pt-1.5 text-[11px] font-bold uppercase tracking-[0.08em] text-muted-foreground">{{ t('navbar.selectLanguage') }}</span>
              <button v-for="lang in languages" :key="`ml-${lang.code}`" class="flex w-full items-center justify-between gap-2.5 rounded-sm px-3 py-2.5 text-left text-sm font-medium transition-colors hover:bg-secondary hover:text-foreground" :class="appStore.locale === lang.code ? 'text-primary' : 'text-muted-foreground'" @click="changeLanguage(lang.code); moreOpen = false">
                {{ lang.name }}
                <span v-if="appStore.locale === lang.code" class="h-[6px] w-[6px] rounded-sm bg-primary"></span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </header>

    <main class="flex-1">
      <slot />
    </main>

    <footer class="mt-[var(--gap-block)] border-t border-[color:var(--border-strong)] bg-[color:var(--bg-warm)]">
      <div class="mx-auto grid w-full max-w-[1180px] grid-cols-1 gap-[30px] px-6 pb-9 pt-[48px] sm:grid-cols-2 lg:grid-cols-[1.7fr_repeat(3,1fr)]">
        <div>
          <RouterLink class="inline-flex items-center gap-2.5 text-[19px] font-bold tracking-[0.04em] text-foreground" style="font-family: var(--font-head)" to="/">
            <img v-if="brandLogo" :src="brandLogo" :alt="brandName" class="h-8 max-w-[160px] object-contain" />
            <span v-else>{{ brandName }}</span>
          </RouterLink>
          <p class="mt-3 max-w-[36ch] text-[14.5px] text-muted-foreground">{{ brandDescription || t('vault.footer.tagline') }}</p>
        </div>
        <div>
          <h4 class="mb-3 text-sm font-bold">{{ t('vault.footer.shop') }}</h4>
          <RouterLink v-if="!isListMode" to="/products" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary">{{ t('products.allCategories') }}</RouterLink>
          <RouterLink v-if="noticeEnabled" to="/notice" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary">{{ t('nav.notice') }}</RouterLink>
          <RouterLink v-if="blogEnabled" to="/blog" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary">{{ t('nav.blog') }}</RouterLink>
          <RouterLink to="/me" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary">{{ t('navbar.personalCenter') }}</RouterLink>
        </div>
        <div>
          <h4 class="mb-3 text-sm font-bold">{{ t('vault.footer.support') }}</h4>
          <RouterLink v-if="aboutEnabled" to="/about" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary"><Info class="h-4 w-4" /> {{ t('nav.about') }}</RouterLink>
          <RouterLink v-if="!userAuthStore.isAuthenticated" to="/guest/orders" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary"><ClipboardList class="h-4 w-4" /> {{ t('navbar.guestOrders') }}</RouterLink>
          <a v-if="contact?.telegram" :href="contact.telegram" target="_blank" rel="noopener noreferrer" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary"><Send class="h-4 w-4" /> Telegram</a>
          <a v-if="contact?.whatsapp" :href="contact.whatsapp" target="_blank" rel="noopener noreferrer" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary"><MessageCircle class="h-4 w-4" /> WhatsApp</a>
        </div>
        <div>
          <h4 class="mb-3 text-sm font-bold">{{ t('vault.footer.legal') }}</h4>
          <RouterLink to="/terms" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary">{{ t('footer.terms') }}</RouterLink>
          <RouterLink to="/privacy" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary">{{ t('footer.privacy') }}</RouterLink>
          <a v-for="link in footerLinks" :key="link.name" :href="link.url || 'javascript:void(0)'" :target="link.url ? '_blank' : undefined" rel="noopener noreferrer" class="flex items-center gap-[7px] py-[5px] text-[14.5px] text-muted-foreground hover:text-primary">{{ link.name }}</a>
        </div>
      </div>
      <div class="mx-auto flex w-full max-w-[1180px] flex-wrap items-center justify-between gap-3.5 border-t border-[color:var(--border)] px-6 pb-[30px] pt-[18px] text-[13.5px] text-muted-foreground">
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span>© {{ year }} {{ brandName }}</span>
        </div>
        <span>简体中文 · 繁體 · English</span>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Search, Moon, Sun, ShoppingCart, Languages, Menu, X, User, Info, ClipboardList, LogOut,
  LayoutGrid, Send, MessageCircle,
} from 'lucide-vue-next'
import { useAppStore } from '../../../stores/app'
import { useCartStore } from '../../../stores/cart'
import { useUserAuthStore } from '../../../stores/userAuth'
import { useNavConfig, type NavItem } from '../../../composables/useNavConfig'
import { useTheme } from '../../../utils/theme'
import { getImageUrl } from '../../../utils/image'
import { getLocalizedText } from '../../../utils/resellerSiteConfig'
import { ATELIER_TOKEN_CLASSES, getActiveTemplate, isAtelierTemplate, type AtelierTemplate } from '../../registry'
import '../../inkstone/styles/inkstone.css'
import '../styles/atelier.css'
import '../styles/nocturne.css'
import '../styles/celadon.css'
import '../styles/ember.css'

const { t } = useI18n()
const appStore = useAppStore()
const cartStore = useCartStore()
const userAuthStore = useUserAuthStore()
const { theme: themeMode, toggleTheme } = useTheme()

const theme = computed<AtelierTemplate>(() => {
  const active = getActiveTemplate()
  return isAtelierTemplate(active) ? active : 'inkstone'
})

const syncBodyTokens = (name: AtelierTemplate) => {
  for (const cls of ATELIER_TOKEN_CLASSES) document.body.classList.remove(cls)
  document.body.classList.add(`${name}-tokens`)
}

const langOpen = ref(false)
const moreOpen = ref(false)
const langEl = ref<HTMLElement | null>(null)
const moreEl = ref<HTMLElement | null>(null)

const year = new Date().getFullYear()

const brandName = computed(() => String(appStore.config?.brand?.site_name || '').trim() || 'Dujiao-Next')
const brandLogo = computed(() => {
  const raw = String(appStore.config?.brand?.site_logo || '').trim()
  return raw ? getImageUrl(raw) : ''
})
const brandDescription = computed(() => {
  const desc = appStore.config?.brand?.site_description
  if (desc && typeof desc === 'object') {
    const val = (desc as Record<string, string>)[appStore.locale] || (desc as Record<string, string>)['zh-CN'] || ''
    return typeof val === 'string' ? val.trim() : ''
  }
  return ''
})

const { isListMode, blogEnabled, noticeEnabled, aboutEnabled, secondaryNavItems } = useNavConfig()

const menuItems = computed<NavItem[]>(() => {
  const items: NavItem[] = []
  if (!isListMode.value) {
    items.push({ key: 'products', path: '/products', label: t('products.allCategories'), icon: LayoutGrid, type: 'route', target: '_self' })
  }
  items.push(...secondaryNavItems.value)
  return items
})

const footerLinks = computed(() => {
  const links = appStore.config?.footer_links
  if (!Array.isArray(links)) return []
  return links
    .map((item: { name?: unknown; url?: unknown }) => ({
      name: typeof item?.name === 'string' ? item.name.trim() : getLocalizedText(item?.name as Record<string, string>, appStore.locale),
      url: String(item?.url || '').trim(),
    }))
    .filter((item) => item.name)
})

const contact = computed(() => appStore.config?.contact as { telegram?: string; whatsapp?: string } | undefined)
const cartCount = computed(() => cartStore.totalItems)

const languages = [
  { code: 'zh-CN', name: '简体中文' },
  { code: 'zh-TW', name: '繁體中文' },
  { code: 'en-US', name: 'English' },
]

const changeLanguage = (code: string) => {
  appStore.setLocale(code)
  langOpen.value = false
}

const toggleLang = () => { langOpen.value = !langOpen.value; moreOpen.value = false }
const toggleMore = () => { moreOpen.value = !moreOpen.value; langOpen.value = false }

const onDocClick = (e: MouseEvent) => {
  const target = e.target as Node
  if (langOpen.value && langEl.value && !langEl.value.contains(target)) langOpen.value = false
  if (moreOpen.value && moreEl.value && !moreEl.value.contains(target)) moreOpen.value = false
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  syncBodyTokens(theme.value)
})
watch(theme, (name) => syncBodyTokens(name))
onUnmounted(() => {
  document.removeEventListener('click', onDocClick)
  for (const cls of ATELIER_TOKEN_CLASSES) document.body.classList.remove(cls)
})
</script>
