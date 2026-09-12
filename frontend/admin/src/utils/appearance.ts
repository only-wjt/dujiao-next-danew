export const ATELIER_TEMPLATES = ['inkstone', 'nocturne', 'celadon', 'ember'] as const
export type AtelierTemplate = (typeof ATELIER_TEMPLATES)[number]

export const STOREFRONT_TEMPLATES = ['classic', 'vault', ...ATELIER_TEMPLATES] as const
export type StorefrontTemplate = (typeof STOREFRONT_TEMPLATES)[number]

export const ADMIN_THEME_CLASSES = ATELIER_TEMPLATES.map((id) => `theme-${id}`)

export const STOREFRONT_TEMPLATE_OPTIONS: { id: StorefrontTemplate; swatch: [string, string, string] }[] = [
  { id: 'classic', swatch: ['#f4f4f5', '#18181b', '#0071e3'] },
  { id: 'vault', swatch: ['#1c1b2e', '#4f46e5', '#f59e0b'] },
  { id: 'inkstone', swatch: ['#f3eee4', '#b23a2e', '#b0893a'] },
  { id: 'nocturne', swatch: ['#e8edf5', '#3d5a80', '#a89b6e'] },
  { id: 'celadon', swatch: ['#eef3ee', '#3d6b58', '#b8a56a'] },
  { id: 'ember', swatch: ['#f3eee8', '#b85c38', '#c4893a'] },
]

export const isAtelierTemplate = (value: string): value is AtelierTemplate =>
  (ATELIER_TEMPLATES as readonly string[]).includes(value)

export const normalizeStorefrontTemplate = (value: unknown): StorefrontTemplate => {
  const raw = String(value || '').trim()
  if ((STOREFRONT_TEMPLATES as readonly string[]).includes(raw)) {
    return raw as StorefrontTemplate
  }
  return 'classic'
}

export const applyAdminAppearance = (template: unknown) => {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  const normalized = normalizeStorefrontTemplate(template)
  for (const cls of ADMIN_THEME_CLASSES) root.classList.remove(cls)
  if (isAtelierTemplate(normalized)) {
    root.classList.add(`theme-${normalized}`)
  }
}
