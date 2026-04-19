<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 See <https://www.gnu.org/licenses/> for license details.
-->
<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { api, errMsg } from '@/api/client'
import { useBranding, type BrandingConfig } from '@/composables/useBranding'

const { branding, refresh } = useBranding()

// Editable copy of the current branding — separate from the readonly
// reactive `branding` so the user's typing isn't visible to the rest of
// the app until Save. On Save we PUT, then call refresh() which updates
// the shared branding ref and re-applies CSS custom props.
type ColorKeys = keyof BrandingConfig['colors']
const COLOR_KEYS: { key: ColorKeys; label: string; hint?: string }[] = [
  { key: 'primary',        label: 'Primary',          hint: 'Main brand colour (links, primary buttons)' },
  { key: 'primaryDark',    label: 'Primary — dark',   hint: 'Hover, pressed states' },
  { key: 'primaryLight',   label: 'Primary — light',  hint: 'Subtle fills' },
  { key: 'primaryPale',    label: 'Primary — pale',   hint: 'Backgrounds, selection highlights' },
  { key: 'accent',         label: 'Accent',           hint: 'Secondary CTAs, success states' },
  { key: 'sidebarBg',      label: 'Sidebar — bg',     hint: 'Default sidebar background' },
  { key: 'sidebarText',    label: 'Sidebar — text',   hint: 'Sidebar foreground' },
  { key: 'loginBg',        label: 'Login — bg',       hint: 'Login page background' },
  { key: 'loginPattern',   label: 'Login — pattern',  hint: 'Login page geometric overlay' },
  { key: 'typeEpic',       label: 'Type: Epic',       hint: 'Epic issue label' },
  { key: 'typeTicket',     label: 'Type: Ticket',     hint: 'Ticket issue label' },
  { key: 'typeTask',       label: 'Type: Task',       hint: 'Task issue label' },
  { key: 'tableRowBorder', label: 'Table row border', hint: 'Horizontal rule between rows' },
  { key: 'tableRowAlt',    label: 'Table stripe',     hint: 'Alt-row background' },
  { key: 'accrualsAccent', label: 'Accruals accent',  hint: 'Vorräte report accent' },
]

const form = reactive<BrandingConfig>(clone(branding.value))
const saving = ref(false)
const saveError = ref('')
const saveOK = ref(false)
const uploadError = ref('')
const uploading = ref<'logo' | 'favicon' | null>(null)

// File input refs — used to clear the native input after upload so the
// same file can be picked again without renaming.
const logoInput = ref<HTMLInputElement | null>(null)
const faviconInput = ref<HTMLInputElement | null>(null)

function clone(b: BrandingConfig): BrandingConfig {
  return JSON.parse(JSON.stringify(b)) as BrandingConfig
}

// Keep form in sync with branding on first mount (and after refresh). We
// don't watch branding for deep changes on purpose: the form is the user's
// working copy, and the refresh() call happens *because* we just saved.
onMounted(() => { Object.assign(form, clone(branding.value)) })

async function onSave() {
  saving.value = true
  saveError.value = ''
  saveOK.value = false
  try {
    await api.put('/branding', form)
    await refresh() // re-fetch + re-apply CSS vars, document title, favicon
    saveOK.value = true
    // Auto-hide the success banner
    setTimeout(() => { saveOK.value = false }, 3000)
  } catch (e) {
    saveError.value = errMsg(e, 'Save failed')
  } finally {
    saving.value = false
  }
}

function onReset() {
  if (!confirm('Reset all fields to the active branding? Unsaved changes will be lost.')) return
  Object.assign(form, clone(branding.value))
}

async function onUpload(kind: 'logo' | 'favicon', ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = kind
  uploadError.value = ''
  try {
    const fd = new FormData()
    fd.append('file', file)
    const res = await api.upload<{ path: string }>(`/branding/${kind}`, fd)
    // Put the new path into the form so the user sees the preview and it
    // persists when they hit Save. Save is still required — that's the
    // model here, not auto-apply on upload.
    if (kind === 'logo') form.logo = res.path
    else form.favicon = res.path
  } catch (e) {
    uploadError.value = errMsg(e, 'Upload failed')
  } finally {
    uploading.value = null
    // Clear the input so selecting the same file again re-triggers change
    if (input) input.value = ''
  }
}

// Cache-busted preview URL: when a freshly uploaded asset overwrites an
// existing file, the browser would otherwise show the cached old bytes.
const logoPreviewURL = computed(() =>
  form.logo ? `${form.logo}${form.logo.includes('?') ? '&' : '?'}v=${Date.now()}` : '',
)
const faviconPreviewURL = computed(() =>
  form.favicon ? `${form.favicon}${form.favicon.includes('?') ? '&' : '?'}v=${Date.now()}` : '',
)
</script>

<template>
  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Identity</h2>
      <p class="section-desc">Text shown throughout the app. Applies live after Save — no restart required.</p>
    </div>
    <div class="card form-grid">
      <label class="field">
        <span class="label-hint">Product name</span>
        <input type="text" v-model="form.name" placeholder="PAIMOS" />
      </label>
      <label class="field">
        <span class="label-hint">Company</span>
        <input type="text" v-model="form.company" placeholder="(optional)" />
      </label>
      <label class="field">
        <span class="label-hint">Product (internal key)</span>
        <input type="text" v-model="form.product" placeholder="PAIMOS" />
      </label>
      <label class="field">
        <span class="label-hint">Tagline</span>
        <input type="text" v-model="form.tagline" placeholder="Your Professional & Personal AI Project OS" />
      </label>
      <label class="field">
        <span class="label-hint">Website</span>
        <input type="url" v-model="form.website" placeholder="https://paimos.com" />
      </label>
      <label class="field">
        <span class="label-hint">Browser tab title</span>
        <input type="text" v-model="form.pageTitle" placeholder="PAIMOS" />
      </label>
    </div>
  </div>

  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Logo &amp; Favicon</h2>
      <p class="section-desc">
        Upload SVG (preferred), PNG or JPEG for the logo (≤ 1&nbsp;MB). Favicon accepts SVG, PNG or ICO (≤ 256&nbsp;KB).
        Uploads are stored on the server and served publicly from <code class="icode">/brand/&lt;filename&gt;</code>.
      </p>
    </div>

    <div class="card asset-card">
      <div class="asset-row">
        <div class="asset-preview">
          <img v-if="logoPreviewURL" :src="logoPreviewURL" alt="Logo preview" class="preview-logo" />
          <span v-else class="preview-empty">No logo</span>
        </div>
        <div class="asset-controls">
          <label class="label-hint">Logo</label>
          <input
            ref="logoInput"
            type="file"
            accept=".svg,image/svg+xml,.png,image/png,.jpg,.jpeg,image/jpeg"
            :disabled="uploading !== null"
            @change="onUpload('logo', $event)"
          />
          <span v-if="uploading === 'logo'" class="muted">Uploading…</span>
          <input type="text" v-model="form.logo" placeholder="/brand/logo.svg" class="url-input" />
        </div>
      </div>

      <div class="asset-row">
        <div class="asset-preview favicon-preview">
          <img v-if="faviconPreviewURL" :src="faviconPreviewURL" alt="Favicon preview" class="preview-favicon" />
          <span v-else class="preview-empty">No favicon</span>
        </div>
        <div class="asset-controls">
          <label class="label-hint">Favicon</label>
          <input
            ref="faviconInput"
            type="file"
            accept=".svg,image/svg+xml,.png,image/png,.ico,image/x-icon,image/vnd.microsoft.icon"
            :disabled="uploading !== null"
            @change="onUpload('favicon', $event)"
          />
          <span v-if="uploading === 'favicon'" class="muted">Uploading…</span>
          <input type="text" v-model="form.favicon" placeholder="/brand/favicon.svg" class="url-input" />
        </div>
      </div>

      <div v-if="uploadError" class="form-error">{{ uploadError }}</div>
    </div>
  </div>

  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Palette</h2>
      <p class="section-desc">Colours used throughout the UI. Applies to everyone — use Appearance for per-browser overrides.</p>
    </div>
    <div class="card palette-grid">
      <div v-for="c in COLOR_KEYS" :key="c.key" class="palette-row">
        <div class="palette-meta">
          <span class="palette-label">{{ c.label }}</span>
          <span v-if="c.hint" class="palette-hint">{{ c.hint }}</span>
        </div>
        <div class="color-input-group">
          <input
            type="color"
            :value="form.colors[c.key] || '#000000'"
            class="color-picker"
            @input="(e) => (form.colors[c.key] = (e.target as HTMLInputElement).value)"
          />
          <input
            type="text"
            :value="form.colors[c.key] || ''"
            class="hex-input"
            placeholder="#rrggbb"
            @input="(e) => (form.colors[c.key] = (e.target as HTMLInputElement).value)"
          />
        </div>
      </div>
    </div>
  </div>

  <div class="save-bar">
    <div v-if="saveError" class="form-error">{{ saveError }}</div>
    <div v-if="saveOK" class="ok-banner">Branding saved — changes are live.</div>
    <button class="btn btn-ghost" :disabled="saving" @click="onReset">Reset</button>
    <button class="btn btn-primary" :disabled="saving" @click="onSave">
      {{ saving ? 'Saving…' : 'Save branding' }}
    </button>
  </div>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 1rem 1.5rem;
}
.field { display: flex; flex-direction: column; gap: .3rem; }
.field input { padding: .4rem .55rem; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-card); color: var(--text); font-size: 13px; }
.label-hint { font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }

.asset-card { display: flex; flex-direction: column; gap: 1rem; }
.asset-row { display: flex; align-items: flex-start; gap: 1.25rem; }
.asset-preview {
  width: 120px; height: 120px; flex-shrink: 0;
  border: 1px solid var(--border); border-radius: var(--radius);
  background: var(--bg); display: flex; align-items: center; justify-content: center;
  overflow: hidden;
}
.favicon-preview { width: 64px; height: 64px; }
.preview-logo { max-width: 100%; max-height: 100%; object-fit: contain; }
.preview-favicon { max-width: 32px; max-height: 32px; }
.preview-empty { font-size: 11px; color: var(--text-muted); text-align: center; }
.asset-controls { flex: 1; display: flex; flex-direction: column; gap: .5rem; min-width: 0; }
.url-input { padding: .35rem .55rem; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-card); color: var(--text); font-size: 12px; font-family: 'DM Mono', monospace; }
.muted { font-size: 12px; color: var(--text-muted); }

.palette-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: .75rem 1.5rem;
}
.palette-row { display: flex; align-items: center; gap: 1rem; }
.palette-meta { display: flex; flex-direction: column; gap: .1rem; flex: 1; min-width: 0; }
.palette-label { font-size: 13px; font-weight: 600; color: var(--text); }
.palette-hint { font-size: 11px; color: var(--text-muted); }
.color-input-group { display: flex; align-items: center; gap: .5rem; flex-shrink: 0; }
.color-picker { width: 36px; height: 28px; padding: 2px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-card); cursor: pointer; }
.hex-input { width: 88px; padding: .25rem .4rem; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-card); color: var(--text); font-size: 12px; font-family: 'DM Mono', monospace; text-transform: lowercase; }

.save-bar {
  position: sticky; bottom: 0;
  display: flex; align-items: center; gap: 1rem;
  padding: 1rem 0;
  background: var(--bg);
  border-top: 1px solid var(--border);
  margin-top: 1rem;
}
.save-bar .btn { margin-left: auto; }
.save-bar .btn + .btn { margin-left: 0; }
.save-bar .form-error,
.save-bar .ok-banner { margin-right: auto; }
.icode { font-family: 'DM Mono','Fira Code',monospace; font-size: 12px; background: var(--bg); border: 1px solid var(--border); border-radius: 3px; padding: .1rem .35rem; color: var(--text); }
</style>
