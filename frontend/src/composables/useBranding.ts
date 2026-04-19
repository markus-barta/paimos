/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

/**
 * useBranding — reactive branding config loaded from the backend.
 *
 * Fetches /api/branding (or a user-selected file via localStorage)
 * and exposes the config as reactive refs. Applies CSS custom properties
 * and updates document title/favicon on load.
 *
 * Singleton: all consumers share the same reactive state.
 */

import { ref, readonly } from 'vue'

export interface BrandingConfig {
  name: string
  company: string
  product: string
  tagline: string
  website: string
  logo: string
  favicon: string
  colors: {
    primary: string
    primaryDark: string
    primaryLight: string
    primaryPale: string
    accent: string
    sidebarBg: string
    sidebarText: string
    loginBg: string
    loginPattern: string
    typeEpic: string
    typeTicket: string
    typeTask: string
    tableRowBorder: string
    tableRowAlt: string
    accrualsAccent?: string
  }
  pageTitle: string
}

const LS_KEY = 'paimos:branding-file'

const defaults: BrandingConfig = {
  name: 'PAIMOS',
  company: 'PAIMOS',
  product: 'PAIMOS',
  tagline: 'Project Management Online',
  website: 'https://paimos.com',
  logo: '/logo.png',
  favicon: '/favicon.png',
  colors: {
    primary: '#2e6da4',
    primaryDark: '#1f4d75',
    primaryLight: '#4a8fc2',
    primaryPale: '#dce9f4',
    accent: '#16a34a',
    sidebarBg: '#1a2d42',
    sidebarText: '#c8d5e2',
    loginBg: '#1a2d42',
    loginPattern: '#243650',
    typeEpic: '#5e35b1',
    typeTicket: '#1f4d75',
    typeTask: '#2e7d32',
    tableRowBorder: '#e8eaed',
    tableRowAlt: '#f8f9fa',
    accrualsAccent: '#006497',
  },
  pageTitle: 'PAIMOS',
}

const branding = ref<BrandingConfig>({ ...defaults })
const loaded = ref(false)

// Mix #rrggbb with white at given alpha → rgba() string
function hexWithAlpha(hex: string, alpha: number): string {
  const m = /^#([0-9a-f]{6})$/i.exec(hex)
  if (!m) return hex
  const n = parseInt(m[1], 16)
  return `rgba(${(n>>16)&255},${(n>>8)&255},${n&255},${alpha})`
}
// Darken/lighten a hex color by `pct` percent (-100..100)
function shadeHex(hex: string, pct: number): string {
  const m = /^#([0-9a-f]{6})$/i.exec(hex)
  if (!m) return hex
  const n = parseInt(m[1], 16)
  const r = (n>>16)&255, g = (n>>8)&255, b = n&255
  const f = (c: number) => Math.max(0, Math.min(255, Math.round(c + (pct/100) * (pct < 0 ? c : 255 - c))))
  return '#' + [f(r), f(g), f(b)].map(c => c.toString(16).padStart(2,'0')).join('')
}

function applyToDOM(cfg: BrandingConfig) {
  const root = document.documentElement.style
  root.setProperty('--bp-blue', cfg.colors.primary)
  root.setProperty('--bp-blue-dark', cfg.colors.primaryDark)
  root.setProperty('--bp-blue-light', cfg.colors.primaryLight)
  root.setProperty('--bp-blue-pale', cfg.colors.primaryPale)
  root.setProperty('--bp-green', cfg.colors.accent)
  root.setProperty('--sidebar-text', cfg.colors.sidebarText)
  root.setProperty('--type-epic', localStorage.getItem('paimos:type-color-epic') || cfg.colors.typeEpic)
  root.setProperty('--type-ticket', localStorage.getItem('paimos:type-color-ticket') || cfg.colors.typeTicket)
  root.setProperty('--type-task', localStorage.getItem('paimos:type-color-task') || cfg.colors.typeTask)
  root.setProperty('--table-row-border', localStorage.getItem('paimos:table-row-border-color') || cfg.colors.tableRowBorder)
  root.setProperty('--table-row-alt', localStorage.getItem('paimos:table-row-stripe-color') || cfg.colors.tableRowAlt)
  // Accruals accent: localStorage override → branding default → fallback
  const accAccent = localStorage.getItem('paimos:accruals-accent') || cfg.colors.accrualsAccent || '#006497'
  root.setProperty('--accruals-accent', accAccent)
  root.setProperty('--accruals-accent-soft', hexWithAlpha(accAccent, 0.10))
  root.setProperty('--accruals-accent-dark', shadeHex(accAccent, -25))

  document.title = cfg.pageTitle

  // Update favicon
  const faviconEl = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (faviconEl) faviconEl.href = cfg.favicon
  const touchEl = document.querySelector<HTMLLinkElement>('link[rel="apple-touch-icon"]')
  if (touchEl) touchEl.href = cfg.logo
}

let initPromise: Promise<void> | null = null

// Shared fetch+apply used by both init (first paint) and refresh (after
// admin edits in the Branding settings tab). Keeps the merge rules for
// `defaults` in one place.
async function fetchAndApply(): Promise<void> {
  try {
    const file = localStorage.getItem(LS_KEY) || ''
    const url = file ? `/api/branding?file=${encodeURIComponent(file)}` : '/api/branding'
    const resp = await fetch(url, { cache: 'no-store' })
    if (resp.ok) {
      const data = await resp.json()
      branding.value = { ...defaults, ...data, colors: { ...defaults.colors, ...data.colors } }
    }
  } catch { /* use defaults */ }
  applyToDOM(branding.value)
  loaded.value = true
}

async function init() {
  if (initPromise) return initPromise
  initPromise = fetchAndApply()
  return initPromise
}

// refresh: re-fetch the branding document and re-apply to the DOM. Used by
// the admin Branding tab after a successful PUT so edits show up live
// without a page reload. Intentionally bypasses the init singleton so it
// always re-reads from the server.
async function refresh(): Promise<void> {
  await fetchAndApply()
}

export function useBranding() {
  return {
    branding: readonly(branding),
    loaded: readonly(loaded),
    init,
    refresh,
    /** Set a branding file preference and reload the page */
    switchBranding(file: string | null) {
      if (file) {
        localStorage.setItem(LS_KEY, file)
      } else {
        localStorage.removeItem(LS_KEY)
      }
      // Clear sidebar and type color overrides so the new brand's defaults apply
      localStorage.removeItem('sidebar-color-bg')
      localStorage.removeItem('sidebar-color-pattern')
      localStorage.removeItem('paimos:type-color-epic')
      localStorage.removeItem('paimos:type-color-ticket')
      localStorage.removeItem('paimos:type-color-task')
      localStorage.removeItem('paimos:table-row-border-color')
      localStorage.removeItem('paimos:table-row-stripe-color')
      localStorage.removeItem('paimos:accruals-accent')
      window.location.reload()
    },
    /** Currently selected branding file (from localStorage) */
    selectedFile(): string {
      return localStorage.getItem(LS_KEY) || 'branding.json'
    },
  }
}
