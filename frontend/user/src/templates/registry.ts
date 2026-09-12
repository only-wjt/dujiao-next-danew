import { useAppStore } from '../stores/app'

/**
 * 店面模板系统（站长全局切换 · 渐进并行迁移）
 *
 * - 当前激活模板优先级：本地预览覆盖(?template=) > 站点全局配置(storefront_template) > 默认 classic
 * - classic 沿用现有 ../views/*，vault 落在 ./vault/*
 * - atelier 系列（inkstone / nocturne / celadon / ember）共用 ThemedLayout，页面体回退 vault，再缺则 classic
 */

export const ATELIER_TEMPLATES = ['inkstone', 'nocturne', 'celadon', 'ember'] as const
export type AtelierTemplate = (typeof ATELIER_TEMPLATES)[number]

export const STOREFRONT_TEMPLATES = ['classic', 'vault', ...ATELIER_TEMPLATES] as const
export type StorefrontTemplate = (typeof STOREFRONT_TEMPLATES)[number]
export const DEFAULT_TEMPLATE: StorefrontTemplate = 'classic'

export const ATELIER_TOKEN_CLASSES = ATELIER_TEMPLATES.map((id) => `${id}-tokens`)

export const isAtelierTemplate = (value: string): value is AtelierTemplate =>
    (ATELIER_TEMPLATES as readonly string[]).includes(value)

const OVERRIDE_KEY = 'dj-storefront-template'

const isTemplate = (value: unknown): value is StorefrontTemplate =>
    typeof value === 'string' && (STOREFRONT_TEMPLATES as readonly string[]).includes(value)

const readOverride = (): StorefrontTemplate | null => {
    try {
        const value = localStorage.getItem(OVERRIDE_KEY)
        return isTemplate(value) ? value : null
    } catch {
        return null
    }
}

/**
 * 预览用：URL 带 ?template=vault / ?template=classic / ?template=inkstone 等时持久化到 localStorage，
 * ?template=reset 清除覆盖。站长正式切换走站点配置，不依赖此入口。
 * 在 app 挂载前调用一次即可。
 */
export const initTemplateOverride = (): void => {
    if (typeof window === 'undefined') return
    try {
        const param = new URLSearchParams(window.location.search).get('template')
        if (param === 'reset') {
            localStorage.removeItem(OVERRIDE_KEY)
        } else if (isTemplate(param)) {
            localStorage.setItem(OVERRIDE_KEY, param)
        }
    } catch {
        /* localStorage 不可用时忽略 */
    }
}

/** 当前激活的店面模板。 */
export const getActiveTemplate = (): StorefrontTemplate => {
    const override = readOverride()
    if (override) return override
    try {
        const appStore = useAppStore()
        const fromConfig = appStore.config?.storefront_template
        if (isTemplate(fromConfig)) return fromConfig
    } catch {
        /* pinia 尚未就绪时退回默认 */
    }
    return DEFAULT_TEMPLATE
}

const vaultViews = import.meta.glob('./vault/**/*.vue')

type ViewLoader = () => Promise<unknown>

const loadNamedView = (views: Record<string, ViewLoader>, dir: 'vault', name: string) => {
    const loader = views[`./${dir}/${name}.vue`]
    return loader ? loader() : null
}

/**
 * 路由 view 解析器：vault 与 atelier 主题优先 vault 页面，缺则回退 classic。
 * 用法：`component: templateView('Home', () => import('../views/Home.vue'))`
 */
export const templateView = (name: string, classicLoader: ViewLoader): ViewLoader => {
    return () => {
        const active = getActiveTemplate()
        if (active === 'vault' || isAtelierTemplate(active)) {
            return loadNamedView(vaultViews, 'vault', name) ?? classicLoader()
        }
        return classicLoader()
    }
}
