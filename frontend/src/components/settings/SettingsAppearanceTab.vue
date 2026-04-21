<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { api } from '@/api/client'
import { useSidebarColors, resetSidebarToDefaults } from '@/composables/useSidebarColors'
import { useBranding } from '@/composables/useBranding'
import { useIssueDisplay, TYPE_SVGS } from '@/composables/useIssueDisplay'
import { useTableAppearance, resetTableAppearance } from '@/composables/useTableAppearance'
import {
  LS_TYPE_COLOR_EPIC,
  LS_TYPE_COLOR_TICKET,
  LS_TYPE_COLOR_TASK,
  LS_ACCRUALS_ACCENT,
} from '@/constants/storage'

// ── Issue Display ────────────────────────────────────────────────────────────
const { _rawIcon: typeIcon, _rawText: typeText } = useIssueDisplay()
function onTypeIconChange(e: Event) { const v = (e.target as HTMLInputElement).checked; if (!v && !typeText.value) return; typeIcon.value = v }
function onTypeTextChange(e: Event) { const v = (e.target as HTMLInputElement).checked; if (!v && !typeIcon.value) return; typeText.value = v }

// ── Sidebar Colors ───────────────────────────────────────────────────────────
const { bgColor, patternColor } = useSidebarColors()
function resetSidebarColors() { resetSidebarToDefaults() }

const previewStyle = computed(() => {
  const enc = (s: string) => s.replace(/#/g, '%23')
  const svg = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='28' height='49' viewBox='0 0 28 49'%3E%3Cg fill-rule='evenodd'%3E%3Cg fill='${enc(patternColor.value)}' fill-opacity='0.4' fill-rule='nonzero'%3E%3Cpath d='M13.99 9.25l13 7.5v15l-13 7.5L1 31.75v-15l12.99-7.5zM3 17.9v12.7l10.99 6.34 11-6.35V17.9l-11-6.34L3 17.9zM0 15l12.98-7.5V0h-2v6.35L0 12.69v2.3zm0 18.5L12.98 41v8h-2v-6.85L0 35.81v-2.3zM15 0v7.5L27.99 15H28v-2.31h-.01L17 6.35V0h-2zm0 49v-8l12.99-7.5H28v2.31h-.01L17 42.15V49h-2z'/%3E%3C/g%3E%3C/g%3E%3C/svg%3E")`
  return { backgroundColor: bgColor.value, backgroundImage: svg }
})

// ── Branding ──────────────────────────────────────────────────────────────────
const { branding, switchBranding, selectedFile } = useBranding()

// ── Table Appearance ─────────────────────────────────────────────────────────
const { showBorders, showStripes, borderColor, stripeColor } = useTableAppearance()
const tableBorderColor = ref(borderColor.value || branding.value.colors.tableRowBorder)
const tableStripeColor = ref(stripeColor.value || branding.value.colors.tableRowAlt)
watch(tableBorderColor, v => { borderColor.value = v; document.documentElement.style.setProperty('--table-row-border', v) })
watch(tableStripeColor, v => { stripeColor.value = v; document.documentElement.style.setProperty('--table-row-alt', v) })
function resetTableColors() {
  resetTableAppearance()
  tableBorderColor.value = branding.value.colors.tableRowBorder
  tableStripeColor.value = branding.value.colors.tableRowAlt
}

// Issue type color overrides
const typeColorEpic = ref(localStorage.getItem(LS_TYPE_COLOR_EPIC) || branding.value.colors.typeEpic)
const typeColorTicket = ref(localStorage.getItem(LS_TYPE_COLOR_TICKET) || branding.value.colors.typeTicket)
const typeColorTask = ref(localStorage.getItem(LS_TYPE_COLOR_TASK) || branding.value.colors.typeTask)

watch(typeColorEpic, v => { localStorage.setItem(LS_TYPE_COLOR_EPIC, v); document.documentElement.style.setProperty('--type-epic', v) })
watch(typeColorTicket, v => { localStorage.setItem(LS_TYPE_COLOR_TICKET, v); document.documentElement.style.setProperty('--type-ticket', v) })
watch(typeColorTask, v => { localStorage.setItem(LS_TYPE_COLOR_TASK, v); document.documentElement.style.setProperty('--type-task', v) })

function resetTypeColors() {
  localStorage.removeItem(LS_TYPE_COLOR_EPIC)
  localStorage.removeItem(LS_TYPE_COLOR_TICKET)
  localStorage.removeItem(LS_TYPE_COLOR_TASK)
  typeColorEpic.value = branding.value.colors.typeEpic
  typeColorTicket.value = branding.value.colors.typeTicket
  typeColorTask.value = branding.value.colors.typeTask
}

// ── Reports / Accruals accent ────────────────────────────────────
const ACCRUALS_DEFAULT = '#006497'
const accrualsAccent = ref(localStorage.getItem(LS_ACCRUALS_ACCENT) || branding.value.colors.accrualsAccent || ACCRUALS_DEFAULT)
function hexWithAlpha(hex: string, alpha: number): string {
  const m = /^#([0-9a-f]{6})$/i.exec(hex); if (!m) return hex
  const n = parseInt(m[1], 16)
  return `rgba(${(n>>16)&255},${(n>>8)&255},${n&255},${alpha})`
}
function shadeHex(hex: string, pct: number): string {
  const m = /^#([0-9a-f]{6})$/i.exec(hex); if (!m) return hex
  const n = parseInt(m[1], 16)
  const r = (n>>16)&255, g = (n>>8)&255, b = n&255
  const f = (c: number) => Math.max(0, Math.min(255, Math.round(c + (pct/100) * (pct < 0 ? c : 255 - c))))
  return '#' + [f(r), f(g), f(b)].map(c => c.toString(16).padStart(2,'0')).join('')
}
watch(accrualsAccent, v => {
  localStorage.setItem(LS_ACCRUALS_ACCENT, v)
  document.documentElement.style.setProperty('--accruals-accent', v)
  document.documentElement.style.setProperty('--accruals-accent-soft', hexWithAlpha(v, 0.10))
  document.documentElement.style.setProperty('--accruals-accent-dark', shadeHex(v, -25))
})
function resetAccrualsAccent() {
  localStorage.removeItem(LS_ACCRUALS_ACCENT)
  accrualsAccent.value = branding.value.colors.accrualsAccent || ACCRUALS_DEFAULT
}

const availableBrandings = ref<{ file: string; name: string }[]>([])
const selectedBrandingFile = ref(selectedFile())

async function loadBrandings() {
  try {
    availableBrandings.value = await api.get<{ file: string; name: string }[]>('/brandings')
  } catch { availableBrandings.value = [] }
}

function applyBranding() {
  const file = selectedBrandingFile.value
  switchBranding(file === 'branding.json' ? null : file)
}

// Init
loadBrandings()
</script>

<template>
  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Issue Display</h2>
      <p class="section-desc">How issue types appear in lists. At least one option must be enabled.</p>
    </div>
    <div class="card card-row" style="gap:2rem;flex-wrap:wrap;align-items:center">
      <label class="toggle-label">
        <input type="checkbox" :checked="typeIcon" @change="onTypeIconChange" :disabled="typeIcon && !typeText" />
        <span class="toggle-text"><span class="toggle-title">Show icon</span><span class="toggle-desc">SVG icon per type</span></span>
      </label>
      <label class="toggle-label">
        <input type="checkbox" :checked="typeText" @change="onTypeTextChange" :disabled="typeText && !typeIcon" />
        <span class="toggle-text"><span class="toggle-title">Show label</span><span class="toggle-desc">Epic / Ticket / Task</span></span>
      </label>
      <div class="display-preview">
        <span v-for="t in ['epic','ticket','task']" :key="t" :class="`issue-type issue-type--${t}`">
          <span v-if="typeIcon" v-html="TYPE_SVGS[t]"></span>
          <span v-if="typeText" class="type-label-text">{{ t.charAt(0).toUpperCase()+t.slice(1) }}</span>
        </span>
      </div>
    </div>
  </div>

  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Issue Type Colors</h2>
      <p class="section-desc">Customize the colors used for epic, ticket, and task labels. Saved in your browser.</p>
    </div>
    <div class="card card-row" style="align-items:center;gap:1.5rem;flex-wrap:wrap">
      <div class="color-row">
        <label class="color-label">Epic</label>
        <div class="color-input-group"><input type="color" v-model="typeColorEpic" class="color-picker" /><code class="icode">{{ typeColorEpic }}</code></div>
      </div>
      <div class="color-row">
        <label class="color-label">Ticket</label>
        <div class="color-input-group"><input type="color" v-model="typeColorTicket" class="color-picker" /><code class="icode">{{ typeColorTicket }}</code></div>
      </div>
      <div class="color-row">
        <label class="color-label">Task</label>
        <div class="color-input-group"><input type="color" v-model="typeColorTask" class="color-picker" /><code class="icode">{{ typeColorTask }}</code></div>
      </div>
      <button class="btn btn-ghost btn-sm" @click="resetTypeColors">Reset to defaults</button>
    </div>
  </div>

  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Table Appearance</h2>
      <p class="section-desc">Row borders and alternating row colors for issue tables. Saved in your browser.</p>
    </div>
    <div class="card" style="display:flex;flex-direction:column;gap:1rem">
      <div style="display:flex;gap:2rem;flex-wrap:wrap;align-items:center">
        <label class="toggle-label">
          <input type="checkbox" v-model="showBorders" />
          <span class="toggle-text"><span class="toggle-title">Show row borders</span><span class="toggle-desc">Horizontal line after each row</span></span>
        </label>
        <label class="toggle-label">
          <input type="checkbox" v-model="showStripes" />
          <span class="toggle-text"><span class="toggle-title">Alternating row colors</span><span class="toggle-desc">Zebra striping on even rows</span></span>
        </label>
      </div>
      <div style="display:flex;gap:1.5rem;flex-wrap:wrap;align-items:center">
        <div v-if="showBorders" class="color-row">
          <label class="color-label">Border color</label>
          <div class="color-input-group"><input type="color" v-model="tableBorderColor" class="color-picker" /><code class="icode">{{ tableBorderColor }}</code></div>
        </div>
        <div v-if="showStripes" class="color-row">
          <label class="color-label">Stripe color</label>
          <div class="color-input-group"><input type="color" v-model="tableStripeColor" class="color-picker" /><code class="icode">{{ tableStripeColor }}</code></div>
        </div>
        <button class="btn btn-ghost btn-sm" @click="resetTableColors">Reset to defaults</button>
      </div>
    </div>
  </div>

  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Sidebar Appearance</h2>
      <p class="section-desc">Customize the sidebar colors. Saved in your browser.</p>
    </div>
    <div class="card card-row" style="align-items:flex-start;gap:2rem;flex-wrap:wrap">
      <div class="appearance-controls">
        <div class="color-row">
          <label class="color-label">Background</label>
          <div class="color-input-group"><input type="color" v-model="bgColor" class="color-picker" /><code class="icode">{{ bgColor }}</code></div>
        </div>
        <div class="color-row">
          <label class="color-label">Pattern</label>
          <div class="color-input-group"><input type="color" v-model="patternColor" class="color-picker" /><code class="icode">{{ patternColor }}</code></div>
        </div>
        <button class="btn btn-ghost btn-sm" @click="resetSidebarColors">Reset to defaults</button>
      </div>
      <div class="appearance-preview" :style="previewStyle">
        <span class="preview-label">Preview</span>
      </div>
    </div>
  </div>

  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Reports — Accent Color</h2>
      <p class="section-desc">Accent used by the Vorräte / Accruals report (subheader, totals, ledger highlights, print view). Saved in your browser.</p>
    </div>
    <div class="card card-row" style="align-items:center;gap:1.5rem;flex-wrap:wrap">
      <div class="color-row">
        <label class="color-label">Accruals</label>
        <div class="color-input-group">
          <input type="color" v-model="accrualsAccent" class="color-picker" />
          <code class="icode">{{ accrualsAccent }}</code>
        </div>
      </div>
      <div class="accruals-accent-preview">
        <span class="aap-eyebrow">Vorräte</span>
        <span class="aap-title">Vorratsbuch</span>
        <span class="aap-rule"></span>
        <span class="aap-figure">1.247,5<span class="aap-unit">h</span></span>
      </div>
      <button class="btn btn-ghost btn-sm" @click="resetAccrualsAccent">Reset to default</button>
    </div>
  </div>

  <div v-if="availableBrandings.length > 1" class="section">
    <div class="section-header">
      <h2 class="section-title">Branding</h2>
      <p class="section-desc">Preview a different branding configuration. This only affects your browser.</p>
    </div>
    <div class="card card-row" style="align-items:center;gap:1rem">
      <select v-model="selectedBrandingFile" class="branding-select">
        <option v-for="b in availableBrandings" :key="b.file" :value="b.file">{{ b.name }}</option>
      </select>
      <button class="btn btn-primary btn-sm" @click="applyBranding" :disabled="selectedBrandingFile === selectedFile()">
        Apply &amp; Reload
      </button>
    </div>
  </div>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.appearance-controls { display: flex; flex-direction: column; gap: 1rem; min-width: 220px; }
.color-row { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
.color-label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; white-space: nowrap; }
.color-input-group { display: flex; align-items: center; gap: .6rem; }
.color-picker { width: 36px; height: 28px; padding: 2px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-card); cursor: pointer; }
.appearance-preview { width: 120px; height: 120px; flex-shrink: 0; border-radius: 8px; border: 1px solid var(--border); display: flex; align-items: center; justify-content: center; overflow: hidden; }
.preview-label { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .08em; color: rgba(255,255,255,.35); }
.branding-select { width: auto; min-width: 200px; font-size: 13px; padding: .4rem .6rem; }
.display-preview { display: flex; gap: 1rem; align-items: center; margin-left: auto; padding-left: 1.5rem; border-left: 1px solid var(--border); }
.toggle-label { display: flex; align-items: flex-start; gap: .65rem; cursor: pointer; user-select: none; }
.toggle-label input[type="checkbox"] { width: 16px; height: 16px; flex-shrink: 0; margin-top: 2px; accent-color: var(--bp-blue); cursor: pointer; }
.toggle-label input:disabled { cursor: not-allowed; opacity: .5; }
.toggle-text { display: flex; flex-direction: column; gap: .1rem; }
.toggle-title { font-size: 13px; font-weight: 600; color: var(--text); }
.toggle-desc  { font-size: 12px; color: var(--text-muted); }
.icode { font-family: 'DM Mono','Fira Code',monospace; font-size: 12px; background: var(--bg); border: 1px solid var(--border); border-radius: 3px; padding: .1rem .35rem; color: var(--text); }

/* Live preview of the accruals accent in the settings tab */
.accruals-accent-preview {
  display: inline-flex; align-items: center; gap: .55rem;
  padding: .55rem .8rem .55rem .9rem;
  background: var(--accruals-accent-soft, #e6f0f6);
  border-left: 2px solid var(--accruals-accent, #006497);
  border-radius: 2px;
  font-family: 'Bricolage Grotesque', system-ui, sans-serif;
}
.aap-eyebrow {
  font-size: 9px; font-weight: 700; letter-spacing: .14em;
  text-transform: uppercase; color: var(--accruals-accent, #006497);
  padding: .12rem .35rem; border: 1px solid var(--accruals-accent, #006497);
  border-radius: 2px; background: #fff;
}
.aap-title { font-size: 13px; font-weight: 700; color: #1a1a1a; letter-spacing: -.01em; }
.aap-rule { width: 12px; height: 1px; background: #cfd4da; }
.aap-figure {
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 13px; font-weight: 600;
  color: var(--accruals-accent, #006497);
  font-variant-numeric: tabular-nums;
}
.aap-unit { font-size: 9px; color: #8a909a; margin-left: 1px; }
</style>
