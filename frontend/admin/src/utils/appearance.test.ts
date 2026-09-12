import { describe, expect, it } from 'vitest'
import {
  ATELIER_TEMPLATES,
  ADMIN_THEME_CLASSES,
  STOREFRONT_TEMPLATE_OPTIONS,
  STOREFRONT_TEMPLATES,
  applyAdminAppearance,
  isAtelierTemplate,
  normalizeStorefrontTemplate,
} from './appearance'

describe('storefront appearance', () => {
  it('keeps the picker list aligned with the whitelist', () => {
    expect(STOREFRONT_TEMPLATE_OPTIONS.map((item) => item.id)).toEqual([...STOREFRONT_TEMPLATES])
    expect(ADMIN_THEME_CLASSES).toEqual(ATELIER_TEMPLATES.map((id) => `theme-${id}`))
  })

  it('accepts every registered template and falls back on unknown values', () => {
    for (const id of STOREFRONT_TEMPLATES) {
      expect(normalizeStorefrontTemplate(` ${id} `)).toBe(id)
    }
    expect(normalizeStorefrontTemplate('VAULT')).toBe('classic')
    expect(normalizeStorefrontTemplate('unknown')).toBe('classic')
    expect(normalizeStorefrontTemplate('')).toBe('classic')
  })

  it('only treats atelier skins as admin theme classes', () => {
    expect(isAtelierTemplate('inkstone')).toBe(true)
    expect(isAtelierTemplate('nocturne')).toBe(true)
    expect(isAtelierTemplate('vault')).toBe(false)
    expect(isAtelierTemplate('classic')).toBe(false)
  })

  it('replaces the previous admin theme class instead of stacking them', () => {
    const root = document.documentElement
    root.className = 'theme-inkstone dark'
    applyAdminAppearance('nocturne')
    expect(root.classList.contains('theme-nocturne')).toBe(true)
    expect(root.classList.contains('theme-inkstone')).toBe(false)
    expect(root.classList.contains('dark')).toBe(true)

    applyAdminAppearance('vault')
    expect(ADMIN_THEME_CLASSES.some((cls) => root.classList.contains(cls))).toBe(false)
    expect(root.classList.contains('dark')).toBe(true)
  })
})
