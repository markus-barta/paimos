<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-149. Admin tab for the LLM text-optimization feature (PAI-146).

 Layout shape:
   1. Hero strip — sparkles tile + product copy + readiness pill.
      The pill is computed from form state and is the headline signal
      ("is this thing on?"). Soft blue gradient + dotted texture for
      atmosphere; explicitly NOT a marketing flourish — same palette
      as the rest of PAIMOS, just composed.
   2. Status card — the enable toggle as a real switch (mirrors
      SettingsCRMTab's `.crm-toggle`), inside a card so the whole
      tile reads as the primary control.
   3. Provider card — pill cards for OpenRouter today plus dimmed
      placeholders for the local-model providers reserved for PAI-122.
      The placeholders are intentional: they tell admins what's
      coming without pretending it ships.
   4. API key — the same "configured / replace / clear" pattern as
      the CRM tab so admins build muscle memory once.
   5. Model — free-form input plus a 5-card preset grid. Each card
      shows the model name, the slug (mono), and category tags
      (Fast / Quality / Open / Cheap). Clicking a card sets the input.
   6. Optimization instruction — textarea with mono font; a `<details>`
      disclosure listing the wrapper invariants admins CAN'T override.
      That transparency is load-bearing: PAI-146 says the safety rules
      stay product-owned, and admins should be able to see them
      without reading source.
   7. Action footer — last-saved timestamp on the left, save button
      on the right.

 What this redesign deliberately does NOT do:
   - Add a "test connection" button. That endpoint doesn't exist yet
     and would invite a half-built feature in v1. Tracked separately
     if the demand surfaces.
   - Reset the optimize_instruction to default via a button. The
     backend already returns the default when stored is empty, but
     surfacing that as a one-click "reset" requires a parallel default
     constant on the frontend; out of scope for the visual redesign.
-->
<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { api, errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import AiPaperTrailPanel from '@/components/ai/AiPaperTrailPanel.vue'

interface AISettings {
  enabled: boolean
  provider: string
  model: string
  api_key: string
  optimize_instruction: string
  updated_at: string
}

// PAI-160: tags shipped by the backend. The picker accepts any
// arbitrary string here so the backend can add new tags without a
// frontend change; the styling map below covers the common cases
// and falls back to a neutral pill for unknown tags.
type PresetTag = string

interface PickedModel {
  id: string
  name: string
  context_length: number
  pricing_prompt_per_mtok: number
  pricing_completion_per_mtok: number
  tags: string[]
}

interface ModelsResponse {
  categories: {
    free: PickedModel[]
    open_weights: PickedModel[]
    frontier: PickedModel[]
    value: PickedModel[]
    cheapest: PickedModel[]
    fastest: PickedModel[]
  }
  fetched_at: string
  stale: boolean
  fastest_unofficial: boolean
  source: string
  upstream_latency_ms?: number
}

interface ProviderOption {
  id: string
  label: string
  available: boolean
  // Reason shown on disabled providers — short, single-tone.
  pendingNote?: string
}

const PROVIDERS: ProviderOption[] = [
  { id: 'openrouter', label: 'OpenRouter', available: true },
  { id: 'ollama',     label: 'Ollama',     available: false, pendingNote: 'PAI-122' },
  { id: 'lmstudio',   label: 'LM Studio',  available: false, pendingNote: 'PAI-122' },
  { id: 'vllm',       label: 'vLLM',       available: false, pendingNote: 'PAI-122' },
  { id: 'llamacpp',   label: 'llama.cpp',  available: false, pendingNote: 'PAI-122' },
]

// PAI-160: category sections rendered in this order. The labels are
// what admins see; the keys map to the backend response shape.
const CATEGORIES: Array<{
  key: keyof ModelsResponse['categories']
  label: string
  icon: string
  hint: string
}> = [
  { key: 'frontier',     label: 'Frontier',      icon: 'sparkles',     hint: 'Top of the leaderboard right now — pick when output quality matters more than cost.' },
  { key: 'value',        label: 'Value',         icon: 'gem',          hint: 'Big context (≥128k) + tools, cheapest in the band. The default for most teams.' },
  { key: 'fastest',      label: 'Fastest',       icon: 'zap',          hint: 'Highest measured throughput. Source ranking is provided by an unofficial endpoint and can break.' },
  { key: 'cheapest',     label: 'Cheapest',      icon: 'tag',          hint: 'Lowest combined prompt + completion price. Free models are listed separately.' },
  { key: 'open_weights', label: 'Open weights',  icon: 'package',      hint: 'Models with public weights — useful when you may want to self-host (PAI-122) later.' },
  { key: 'free',         label: 'Free',          icon: 'gift',         hint: 'Cost nothing per token. Often rate-limited; great for experimentation.' },
]

// PAI-160 + PAI-178: tag styling. Falls back to a neutral pill if
// the backend adds a tag the frontend hasn't styled yet. Vendor
// tags are rendered as `vendor:anthropic` etc. — the helper
// labelForTag() rewrites them to a friendly label at render time.
const TAG_LABEL: Record<string, string> = {
  fast:           'Fast',
  fastest:        'Fastest',
  quality:        'Quality',
  frontier:       'Frontier',
  open:           'Open',
  open_weights:   'Open weights',
  cheap:          'Cheap',
  cheapest:       'Cheapest',
  value:          'Value',
  free:           'Free',
  'vendor:anthropic': 'Anthropic',
  'vendor:openai':    'OpenAI',
  'vendor:xai':       'xAI',
  'vendor:google':    'Google',
}
function labelForTag(t: string): string {
  if (TAG_LABEL[t]) return TAG_LABEL[t]
  if (t.startsWith('vendor:')) return t.slice('vendor:'.length).replace(/^./, c => c.toUpperCase())
  return t
}
function tagClass(t: string): string {
  // CSS class names can't include ":" — translate vendor:foo →
  // ai-preset-tag--vendor-foo so the existing styles per vendor
  // can target the right class.
  return 'ai-preset-tag--' + t.replace(/:/g, '-')
}

const form = reactive<AISettings>({
  enabled: false,
  provider: 'openrouter',
  model: '',
  api_key: '',
  optimize_instruction: '',
  updated_at: '',
})
const hasStoredKey = ref(false)
const replacingKey = ref(false)

const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const saveError = ref('')
const saveOK = ref(false)

// PAI-159: Test connection state. The test endpoint accepts the
// *unsaved* form values, so admins can verify a (provider, model, key)
// triple before persisting. Result is rendered as a small inline
// banner above the action footer.
interface AITestResult {
  ok: boolean
  message: string
  response_text?: string
  model?: string
  latency_ms?: number
  prompt_tokens?: number
  completion_tokens?: number
  marker?: string
}
const testing = ref(false)
const testResult = ref<AITestResult | null>(null)
function clearTestResult() { testResult.value = null }

// PAI-160: live model picker state. Loaded on mount; manual refresh
// button forces a re-fetch through the backend cache.
const modelsPayload = ref<ModelsResponse | null>(null)
const modelsLoading = ref(false)
const modelsError = ref('')
async function loadModels(force = false) {
  modelsLoading.value = true
  modelsError.value = ''
  try {
    const path = force ? '/ai/models?force=1' : '/ai/models'
    modelsPayload.value = await api.get<ModelsResponse>(path)
  } catch (e) {
    modelsError.value = errMsg(e, 'Failed to load model recommendations.')
  } finally {
    modelsLoading.value = false
  }
}

// PAI-161: today's usage. Admin-only summary of how many tokens the
// instance has spent against the daily cap. Loaded lazily; the
// section is collapsible because most admins only check it once.
interface AIUsageRow {
  user_id: number
  username: string
  is_admin: boolean
  prompt_tokens: number
  completion_tokens: number
  request_count: number
  cap_effective: number
  cap_override: number | null
  over_cap: boolean
}
interface AIUsageResponse {
  day: string
  default_cap: number
  org_prompt_tokens: number
  org_completion_tokens: number
  org_request_count: number
  users: AIUsageRow[]
}
const usagePayload = ref<AIUsageResponse | null>(null)
const usageLoading = ref(false)
const usageError = ref('')
async function loadUsage() {
  usageLoading.value = true
  usageError.value = ''
  try {
    usagePayload.value = await api.get<AIUsageResponse>('/ai/usage')
  } catch (e) {
    usageError.value = errMsg(e, 'Failed to load AI usage.')
  } finally {
    usageLoading.value = false
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const s = await api.get<AISettings>('/ai/settings')
    Object.assign(form, s)
    hasStoredKey.value = !!s.api_key
    // Never echo the saved key into the form. The "configured" pill
    // tells the admin a key exists; they replace it explicitly.
    form.api_key = ''
    replacingKey.value = !hasStoredKey.value
  } catch (e) {
    loadError.value = errMsg(e, 'Failed to load AI settings.')
  } finally {
    loading.value = false
  }
}
onMounted(() => {
  load()
  loadModels()
  loadUsage()
})

function startReplace() {
  replacingKey.value = true
  form.api_key = ''
}
function cancelReplace() {
  replacingKey.value = false
  form.api_key = ''
}
function clearKey() {
  // "Clear" sends an empty string in the payload, telling the backend
  // to wipe the stored key. Mirrors the CRM tab's "Clear" affordance.
  form.api_key = ''
  replacingKey.value = true
}

// PAI-159: Test connection. Fires a single LLM round-trip with a
// fixed prompt that asks for a witty one-liner containing the literal
// token "OK". The handler returns 200 with a structured body whether
// the call succeeded or failed — this lets us render success and
// failure with the same component instead of branching on HTTP status.
async function onTestConnection() {
  if (testing.value) return
  testing.value = true
  testResult.value = null
  // The backend treats `api_key=""` as "form is empty" (returns
  // ok=false with a friendly message). For an admin testing without
  // re-typing the saved key, we can't recover it (never echoed) — so
  // the button is disabled in that case via canTest.
  const body: Record<string, unknown> = {
    provider: form.provider,
    model: form.model.trim(),
    api_key: form.api_key,
  }
  try {
    const r = await api.post<AITestResult>('/ai/test', body)
    testResult.value = r
  } catch (e) {
    testResult.value = {
      ok: false,
      message: errMsg(e, 'Test connection failed.'),
    }
  } finally {
    testing.value = false
  }
}

async function onSave() {
  saving.value = true
  saveError.value = ''
  saveOK.value = false
  try {
    const body: Record<string, unknown> = {
      enabled: form.enabled,
      provider: form.provider,
      model: form.model.trim(),
      optimize_instruction: form.optimize_instruction,
    }
    // api_key omitted means "leave the stored value alone"; an empty
    // string clears it; a non-empty string replaces it. Distinguish
    // these three at the wire so an unrelated edit can't drop the key.
    if (replacingKey.value) {
      body.api_key = form.api_key
    }
    const updated = await api.put<AISettings>('/ai/settings', body)
    Object.assign(form, updated)
    hasStoredKey.value = !!updated.api_key
    form.api_key = ''
    replacingKey.value = !hasStoredKey.value
    saveOK.value = true
    setTimeout(() => { saveOK.value = false }, 3000)
  } catch (e) {
    saveError.value = errMsg(e, 'Save failed')
  } finally {
    saving.value = false
  }
}

// ── Computed UI state ─────────────────────────────────────────────
// PAI-159 + PAI-180: Test connection is enabled when we have something
// the backend can run a roundtrip with. Either:
//   - the form is filled in (admin typing fresh values), OR
//   - a key was previously saved (hasStoredKey) AND a model is set.
// The backend falls back to the saved settings for empty form fields,
// so requiring the form to be fully populated would gate the button
// off in the common "I just opened the page and want to verify" case.
const canTest = computed(() =>
  form.provider !== '' &&
  (form.model.trim() !== '' /* always copied into the form on load */) &&
  (form.api_key.trim() !== '' || hasStoredKey.value)
)
const testTooltip = computed(() => {
  if (!form.model.trim()) return 'Pick a model first.'
  if (!form.api_key.trim() && !hasStoredKey.value) {
    return 'Add an API key — the saved key is reused if the field is empty.'
  }
  if (!form.api_key.trim() && hasStoredKey.value) {
    return 'Pings the provider with the saved API key. Type one in the field to test a different key.'
  }
  return 'Send a one-shot ping to the provider with the form values above.'
})
const canEnable = computed(() => hasStoredKey.value && form.model.trim() !== '')
const enableHint = computed(() => {
  if (!hasStoredKey.value) return 'Add an OpenRouter API key first.'
  if (form.model.trim() === '') return 'Pick a model first.'
  return ''
})

// readiness drives the hero pill. Three colors map to four real
// states; "off" covers both "never configured" and "configured but
// switched off" because the operator-facing remediation is the same
// (flip the toggle) and a fourth pill colour adds noise.
type ReadinessTone = 'ready' | 'warn' | 'off'
const readiness = computed<{ label: string; tone: ReadinessTone }>(() => {
  if (form.enabled && hasStoredKey.value && form.model.trim() !== '') {
    return { label: 'Ready', tone: 'ready' }
  }
  if (form.enabled) {
    return { label: 'Needs configuration', tone: 'warn' }
  }
  if (hasStoredKey.value && form.model.trim() !== '') {
    return { label: 'Configured · Off', tone: 'off' }
  }
  return { label: 'Disabled', tone: 'off' }
})

function applyPreset(slug: string) {
  form.model = slug
}

// PAI-160: format a context window for the picker card. Models top
// out around 2M tokens; a compact "200k" / "1.5M" reads better than
// the full integer. Below 1k we just print the value to avoid
// rounding 200 to "0k".
function formatContext(ctx: number): string {
  if (ctx <= 0) return '?'
  if (ctx >= 1_000_000) return (ctx / 1_000_000).toFixed(ctx % 1_000_000 === 0 ? 0 : 1) + 'M'
  if (ctx >= 1_000) return Math.round(ctx / 1_000) + 'k'
  return String(ctx)
}

// Light relative-time formatter for the "last saved" stamp. Scope
// stays small: PAIMOS doesn't have a shared util for this and adding
// a date library for one timestamp is overkill.
function relTime(iso: string): string {
  if (!iso) return ''
  // SQLite returns "YYYY-MM-DD HH:MM:SS" UTC; new Date parses ISO with T.
  const norm = iso.includes('T') ? iso : iso.replace(' ', 'T') + 'Z'
  const d = new Date(norm)
  if (Number.isNaN(d.getTime())) return ''
  const diff = Date.now() - d.getTime()
  if (diff < 60_000)         return 'just now'
  if (diff < 3_600_000)      return Math.round(diff / 60_000) + ' min ago'
  if (diff < 86_400_000)     return Math.round(diff / 3_600_000) + ' h ago'
  if (diff < 7 * 86_400_000) return Math.round(diff / 86_400_000) + ' d ago'
  return d.toLocaleDateString()
}
</script>

<template>
  <div class="ai-tab">
    <!-- ── 1. HERO ───────────────────────────────────────────────── -->
    <header class="ai-hero">
      <div class="ai-hero-iconwrap" aria-hidden="true">
        <AppIcon name="sparkles" :size="26" />
      </div>
      <div class="ai-hero-text">
        <div class="ai-hero-titlerow">
          <h2 class="ai-hero-title">AI text optimization</h2>
          <span :class="['ai-status-pill', `ai-status-pill--${readiness.tone}`]">
            {{ readiness.label }}
          </span>
        </div>
        <p class="ai-hero-desc">
          Adds an inline <strong>AI</strong> action to multiline fields
          (description, acceptance criteria, notes) so authors can
          polish wording without leaving the editor. Optimized output is
          shown in a diff preview before anything is replaced —
          nothing is rewritten silently.
        </p>
      </div>
    </header>

    <div v-if="loading" class="ai-loading">Loading…</div>
    <div v-else-if="loadError" class="ai-banner ai-banner--error">{{ loadError }}</div>

    <template v-else>
      <!-- ── PAI-180: STICKY ACTION BAR ───────────────────────────
           Pinned to the top of the scroll viewport so admins can
           hit Save / Test connection without scrolling through
           the long form. Lives at the top of the loaded section
           (above the toggle card) and uses position:sticky with
           a backdrop-blurred surface so content scrolling under
           it stays legible. -->
      <footer class="ai-actions ai-actions--sticky">
        <span class="ai-actions-meta">
          <template v-if="form.updated_at">
            Last saved <time :datetime="form.updated_at">{{ relTime(form.updated_at) }}</time>
          </template>
          <template v-else>—</template>
        </span>
        <div class="ai-actions-buttons">
          <button
            type="button"
            class="btn btn-ghost ai-test-btn"
            :disabled="!canTest || testing"
            :title="testTooltip"
            @click="onTestConnection"
          >
            <AppIcon v-if="testing" name="loader-circle" :size="13" class="spin" />
            <AppIcon v-else name="plug" :size="13" />
            {{ testing ? 'Testing…' : 'Test connection' }}
          </button>
          <button
            type="button"
            class="btn btn-primary ai-save"
            :disabled="saving"
            @click="onSave"
          >
            <AppIcon v-if="saving" name="loader-circle" :size="13" class="spin" />
            {{ saving ? 'Saving…' : 'Save changes' }}
          </button>
        </div>
      </footer>

      <!-- Banners + test result sit just below the sticky bar so
           the response of clicking either button shows up where the
           user just clicked, without scrolling. -->
      <p v-if="saveError" class="ai-banner ai-banner--error">
        <AppIcon name="alert-triangle" :size="14" /> {{ saveError }}
      </p>
      <p v-if="saveOK" class="ai-banner ai-banner--ok">
        <AppIcon name="check-circle" :size="14" /> Settings saved.
      </p>
      <div
        v-if="testResult"
        :class="['ai-test-result', testResult.ok ? 'ai-test-result--ok' : 'ai-test-result--fail']"
        role="status"
      >
        <div class="ai-test-result-head">
          <AppIcon :name="testResult.ok ? 'check-circle' : 'alert-triangle'" :size="16" />
          <strong class="ai-test-result-msg">{{ testResult.message }}</strong>
          <button
            type="button"
            class="ai-test-result-x"
            aria-label="Dismiss"
            @click="clearTestResult"
          >×</button>
        </div>
        <div v-if="testResult.response_text" class="ai-test-result-body">
          <span class="ai-test-result-label">Reply</span>
          <span class="ai-test-result-quote">{{ testResult.response_text }}</span>
        </div>
        <div class="ai-test-result-meta">
          <span v-if="testResult.model" class="ai-test-result-pill">
            <AppIcon name="cpu" :size="11" /> {{ testResult.model }}
          </span>
          <span v-if="testResult.latency_ms" class="ai-test-result-pill">
            <AppIcon name="zap" :size="11" /> {{ testResult.latency_ms }} ms
          </span>
          <span v-if="testResult.prompt_tokens != null && testResult.completion_tokens != null && (testResult.prompt_tokens + testResult.completion_tokens) > 0" class="ai-test-result-pill">
            {{ testResult.prompt_tokens }}p + {{ testResult.completion_tokens }}c tokens
          </span>
          <span v-if="testResult.marker" :class="['ai-test-result-pill', `ai-test-result-pill--${testResult.marker.toLowerCase()}`]">
            marker: {{ testResult.marker }}
          </span>
        </div>
      </div>

      <!-- ── 2. STATUS / TOGGLE ──────────────────────────────────── -->
      <section class="ai-card">
        <header class="ai-card-headrow">
          <span class="ai-card-headicon"><AppIcon name="zap" :size="15" /></span>
          <h3 class="ai-card-title">Status</h3>
        </header>
        <div class="ai-toggle-row" :class="{ 'ai-toggle-row--locked': !canEnable }">
          <div class="ai-toggle-text">
            <strong class="ai-toggle-label">Enable AI optimization</strong>
            <span class="ai-toggle-hint">
              <template v-if="!canEnable">
                <AppIcon name="alert-triangle" :size="12" />
                {{ enableHint }}
              </template>
              <template v-else>
                Authors will see an <code>AI</code> pill on supported editors.
              </template>
            </span>
          </div>
          <label class="ai-switch" :title="enableHint || 'Toggle AI optimization'">
            <input type="checkbox" v-model="form.enabled" :disabled="!canEnable" />
            <span class="ai-switch-track" />
          </label>
        </div>
      </section>

      <!-- ── 3. PROVIDER ─────────────────────────────────────────── -->
      <section class="ai-card">
        <header class="ai-card-headrow">
          <span class="ai-card-headicon"><AppIcon name="plug" :size="15" /></span>
          <h3 class="ai-card-title">Provider</h3>
        </header>
        <div class="ai-providers">
          <button
            v-for="p in PROVIDERS" :key="p.id"
            type="button"
            :class="['ai-provider', {
              'ai-provider--active':   form.provider === p.id && p.available,
              'ai-provider--disabled': !p.available,
            }]"
            :disabled="!p.available"
            :aria-pressed="form.provider === p.id"
            :title="p.available ? p.label : `${p.label} — coming with PAI-122`"
            @click="p.available && (form.provider = p.id)"
          >
            <span class="ai-provider-name">{{ p.label }}</span>
            <span v-if="!p.available" class="ai-provider-tag">{{ p.pendingNote }}</span>
            <span v-else-if="form.provider === p.id" class="ai-provider-check">
              <AppIcon name="check" :size="11" />
            </span>
          </button>
        </div>
        <p class="ai-help">
          The provider seam is abstracted. Local-model backends (Ollama, LM
          Studio, vLLM, llama.cpp) appear here once
          <code>PAI-122</code> ships — no editor or settings change needed.
        </p>
      </section>

      <!-- ── 4. API KEY ──────────────────────────────────────────── -->
      <section class="ai-card">
        <header class="ai-card-headrow">
          <span class="ai-card-headicon"><AppIcon name="key-round" :size="15" /></span>
          <h3 class="ai-card-title">OpenRouter API key</h3>
        </header>
        <div v-if="!replacingKey && hasStoredKey" class="ai-key-set">
          <span class="ai-key-dots" aria-hidden="true">●●●●●●●●●●</span>
          <span class="ai-key-state">Configured</span>
          <div class="ai-key-actions">
            <button type="button" class="btn btn-ghost btn-sm" @click="startReplace">
              <AppIcon name="key-round" :size="12" /> Replace
            </button>
            <button type="button" class="btn btn-ghost btn-sm ai-key-clear" @click="clearKey">
              Clear
            </button>
          </div>
        </div>
        <div v-else class="ai-key-input-row">
          <input
            v-model="form.api_key"
            type="password"
            autocomplete="new-password"
            placeholder="sk-or-…"
            class="ai-input"
          />
          <button
            v-if="hasStoredKey"
            type="button"
            class="btn btn-ghost btn-sm"
            @click="cancelReplace"
          >Cancel</button>
        </div>
        <p class="ai-help">
          Get a key at
          <a href="https://openrouter.ai/keys" target="_blank" rel="noopener">openrouter.ai/keys</a>.
          Stored unencrypted in the PAIMOS database — keep the data
          volume on encrypted storage if your threat model needs that.
        </p>
      </section>

      <!-- ── 5. MODEL ────────────────────────────────────────────── -->
      <section class="ai-card">
        <header class="ai-card-headrow">
          <span class="ai-card-headicon"><AppIcon name="cpu" :size="15" /></span>
          <h3 class="ai-card-title">Model</h3>
        </header>
        <!-- Manual model id — always visible per PAI-160 (escape hatch). -->
        <input
          v-model="form.model"
          type="text"
          placeholder="anthropic/claude-3.5-haiku"
          class="ai-input ai-input-mono"
          spellcheck="false"
        />

        <!-- PAI-160: live picker. Six categories, top-3 each, fed by
             /api/ai/models with a 1h server-side cache. The "Refresh"
             button busts that cache when an admin knows a new model
             just dropped. Stale state is rendered honestly so admins
             know when they're looking at last-known-good. -->
        <div class="ai-presets-headrow">
          <div class="ai-presets-headrow-left">
            <span class="ai-presets-label">Recommendations</span>
            <span v-if="modelsPayload?.stale" class="ai-stale-pill" title="Showing the last cached snapshot — the upstream lookup just now failed.">
              <AppIcon name="alert-triangle" :size="11" /> stale
            </span>
            <span v-if="modelsPayload?.source === 'static-fallback'" class="ai-stale-pill" title="OpenRouter unreachable on first load — using a curated static fallback.">
              <AppIcon name="alert-triangle" :size="11" /> fallback
            </span>
          </div>
          <button
            type="button"
            class="btn btn-ghost btn-sm ai-presets-refresh"
            :disabled="modelsLoading"
            @click="loadModels(true)"
            title="Re-fetch the model list from OpenRouter, bypassing the 1-hour server cache."
          >
            <AppIcon :name="modelsLoading ? 'loader-circle' : 'refresh-cw'" :size="12" :class="{ spin: modelsLoading }" />
            {{ modelsLoading ? 'Loading…' : 'Refresh' }}
          </button>
        </div>

        <p v-if="modelsError" class="ai-banner ai-banner--error">
          <AppIcon name="alert-triangle" :size="14" /> {{ modelsError }}
        </p>

        <template v-if="modelsPayload">
          <section
            v-for="cat in CATEGORIES" :key="cat.key"
            v-show="modelsPayload.categories[cat.key]?.length"
            class="ai-cat"
          >
            <div class="ai-cat-headrow">
              <span class="ai-cat-icon"><AppIcon :name="cat.icon" :size="13" /></span>
              <strong class="ai-cat-label">{{ cat.label }}</strong>
              <span v-if="cat.key === 'fastest' && modelsPayload.fastest_unofficial" class="ai-cat-betatag" title="Source ranking comes from an undocumented OpenRouter endpoint — best-effort.">unofficial source</span>
              <span class="ai-cat-hint">{{ cat.hint }}</span>
            </div>
            <div class="ai-presets-grid">
              <button
                v-for="m in modelsPayload.categories[cat.key]" :key="m.id"
                type="button"
                :class="['ai-preset', { 'ai-preset--active': form.model.trim() === m.id }]"
                :title="m.id"
                @click="applyPreset(m.id)"
              >
                <div class="ai-preset-row">
                  <strong class="ai-preset-name">{{ m.name || m.id }}</strong>
                  <span v-if="form.model.trim() === m.id" class="ai-preset-checkdot" aria-hidden="true">
                    <AppIcon name="check" :size="10" />
                  </span>
                </div>
                <code class="ai-preset-slug">{{ m.id }}</code>
                <div class="ai-preset-meta">
                  <span class="ai-preset-meta-bit" :title="`${m.context_length.toLocaleString()} token context`">
                    {{ formatContext(m.context_length) }} ctx
                  </span>
                  <span
                    v-if="m.pricing_prompt_per_mtok || m.pricing_completion_per_mtok"
                    class="ai-preset-meta-bit"
                    title="USD per million tokens — prompt / completion"
                  >
                    ${{ m.pricing_prompt_per_mtok.toFixed(2) }} / ${{ m.pricing_completion_per_mtok.toFixed(2) }}
                  </span>
                  <span v-else class="ai-preset-meta-bit ai-preset-meta-bit--free" title="No per-token cost.">free</span>
                </div>
                <div class="ai-preset-tags">
                  <span
                    v-for="t in m.tags" :key="t"
                    :class="['ai-preset-tag', tagClass(t)]"
                  >{{ labelForTag(t) }}</span>
                </div>
              </button>
            </div>
          </section>
        </template>
        <p v-else-if="modelsLoading" class="ai-help">Loading recommendations from OpenRouter…</p>
      </section>

      <!-- ── 6. OPTIMIZATION INSTRUCTION ─────────────────────────── -->
      <section class="ai-card">
        <header class="ai-card-headrow">
          <span class="ai-card-headicon"><AppIcon name="pen-line" :size="15" /></span>
          <h3 class="ai-card-title">Optimization instruction</h3>
        </header>
        <p class="ai-help ai-help--top">
          Layered inside a fixed PAIMOS-owned wrapper that always
          preserves markdown structure, technical meaning, and
          architecture-significant intent. Use this field to add
          project-specific tone guidance — not to override the safety
          rules.
        </p>
        <details class="ai-invariants">
          <summary>
            <AppIcon name="shield" :size="13" />
            <span>What the wrapper enforces (you can’t override these)</span>
          </summary>
          <ul class="ai-invariants-list">
            <li>Preserve technical meaning, intent, and explicit decisions verbatim.</li>
            <li>Preserve markdown structure: headings, lists, checklists, code blocks, inline formatting.</li>
            <li>Preserve architecture-significant phrasing: <code>architecture change</code>, <code>breaking change</code>, <code>schema change</code>, <code>infra change</code>, <code>new component</code>, plus version + migration tokens like <code>M74</code> / <code>v1.7.0</code>.</li>
            <li>Do not add new requirements, scope, commitments, or assumptions.</li>
            <li>Do not translate to another language.</li>
            <li>Return only the rewritten field content (no preamble, no fences).</li>
          </ul>
        </details>
        <textarea
          v-model="form.optimize_instruction"
          rows="10"
          spellcheck="false"
          class="ai-textarea"
        ></textarea>
      </section>

      <!-- ── 7. PAI-161: USAGE PANEL ─────────────────────────────── -->
      <section class="ai-card">
        <header class="ai-card-headrow">
          <span class="ai-card-headicon"><AppIcon name="bar-chart-3" :size="15" /></span>
          <h3 class="ai-card-title">Usage today</h3>
          <button
            type="button"
            class="btn btn-ghost btn-sm ai-presets-refresh"
            :disabled="usageLoading"
            @click="loadUsage"
            title="Refresh usage stats."
          >
            <AppIcon :name="usageLoading ? 'loader-circle' : 'refresh-cw'" :size="12" :class="{ spin: usageLoading }" />
            {{ usageLoading ? 'Loading…' : 'Refresh' }}
          </button>
        </header>
        <p class="ai-help">
          Counts tokens against the per-user daily cap. Default cap is
          set via <code>PAIMOS_AI_DAILY_CAP_TOKENS</code>; per-user
          override is editable from the user admin panel.
        </p>
        <p v-if="usageError" class="ai-banner ai-banner--error">
          <AppIcon name="alert-triangle" :size="14" /> {{ usageError }}
        </p>
        <template v-if="usagePayload">
          <div class="ai-usage-summary">
            <span class="ai-usage-pill" title="Aggregate across all users for the current UTC day">
              <strong>{{ usagePayload.org_prompt_tokens.toLocaleString() }}p</strong> + <strong>{{ usagePayload.org_completion_tokens.toLocaleString() }}c</strong> tokens
            </span>
            <span class="ai-usage-pill">
              <strong>{{ usagePayload.org_request_count }}</strong> calls
            </span>
            <span class="ai-usage-pill">
              Default cap <strong>{{ usagePayload.default_cap.toLocaleString() }}</strong>/user/day
            </span>
            <span class="ai-usage-pill ai-usage-pill--day">UTC day {{ usagePayload.day }}</span>
          </div>
          <table v-if="usagePayload.users.length" class="ai-usage-table">
            <thead>
              <tr>
                <th>User</th>
                <th class="ai-usage-num">Tokens (p+c)</th>
                <th class="ai-usage-num">Calls</th>
                <th class="ai-usage-num">Cap</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in usagePayload.users" :key="u.user_id" :class="{ 'ai-usage-row--over': u.over_cap }">
                <td>
                  {{ u.username }}
                  <span v-if="u.is_admin" class="ai-usage-admin-tag">admin</span>
                </td>
                <td class="ai-usage-num">{{ (u.prompt_tokens + u.completion_tokens).toLocaleString() }}</td>
                <td class="ai-usage-num">{{ u.request_count }}</td>
                <td class="ai-usage-num">
                  {{ u.cap_effective.toLocaleString() }}<span v-if="u.cap_override !== null" class="ai-usage-override-tag" title="Override is set on this user">override</span>
                </td>
                <td>
                  <span v-if="u.over_cap && !u.is_admin" class="ai-usage-over-tag">over</span>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-else class="ai-help">No AI calls today yet.</p>
        </template>
      </section>

      <section class="ai-card">
        <header class="ai-card-headrow">
          <span class="ai-card-headicon"><AppIcon name="scroll-text" :size="15" /></span>
          <h3 class="ai-card-title">Paper trail</h3>
        </header>
        <p class="ai-help">
          Queryable per-call audit metadata for every AI invocation. Bodies stay out of storage by design.
        </p>
        <AiPaperTrailPanel mode="admin" />
      </section>

    </template>
  </div>
</template>

<style scoped>
/* The whole tab: capped width so wide screens don't stretch the form
   into uselessness, but inputs inside still flex to the column edge.
   PAI-178: bumped from 920 → 1200 so the four-card model picker
   row fits comfortably without cards squeezing below readable. */
.ai-tab {
  display: flex; flex-direction: column;
  gap: 1rem;
  max-width: 1200px;
}

/* ── HERO ─────────────────────────────────────────────────────── */
.ai-hero {
  position: relative;
  display: flex; align-items: flex-start; gap: 1.1rem;
  padding: 1.4rem 1.6rem;
  border: 1px solid var(--border);
  border-radius: 14px;
  /* Two-layer background: a soft brand-blue gradient for warmth, with
     a fine dot grid on top for subtle texture. The dot pattern is
     deliberately tiny — anything bigger would read as decorative
     noise and clash with the rest of the admin surface. */
  background:
    radial-gradient(circle at 1px 1px, rgba(46, 109, 164, .14) 1px, transparent 1.4px),
    linear-gradient(135deg, var(--bp-blue-pale) 0%, transparent 70%),
    var(--bg-card);
  background-size: 14px 14px, 100% 100%, 100% 100%;
  box-shadow: 0 1px 2px rgba(0,0,0,.03);
  overflow: hidden;
}
.ai-hero-iconwrap {
  flex-shrink: 0;
  width: 48px; height: 48px;
  display: flex; align-items: center; justify-content: center;
  background: white;
  border: 1px solid var(--border);
  border-radius: 12px;
  color: var(--bp-blue-dark);
  filter: drop-shadow(0 4px 10px rgba(46, 109, 164, .14));
}
.ai-hero-iconwrap :deep(svg) { animation: ai-sparkle-pulse 3.6s ease-in-out infinite; }
@keyframes ai-sparkle-pulse {
  0%, 100% { transform: scale(1); }
  50%      { transform: scale(1.07); }
}
.ai-hero-text { flex: 1 1 auto; min-width: 0; }
.ai-hero-titlerow {
  display: flex; align-items: center; gap: .75rem;
  flex-wrap: wrap;
  margin-bottom: .35rem;
}
.ai-hero-title {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  letter-spacing: -.018em;
  color: var(--text);
  font-family: 'Bricolage Grotesque', 'DM Sans', sans-serif;
}
.ai-hero-desc {
  margin: 0;
  font-size: 13px;
  line-height: 1.55;
  color: var(--text-muted);
  max-width: 680px;
}
.ai-hero-desc strong { color: var(--text); font-weight: 600; }

/* ── STATUS PILL ──────────────────────────────────────────────── */
.ai-status-pill {
  display: inline-flex; align-items: center; gap: .4rem;
  padding: .18rem .6rem .2rem;
  font-size: 10.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .08em;
  border-radius: 999px;
  font-family: 'DM Sans', sans-serif;
  white-space: nowrap;
  line-height: 1;
}
.ai-status-pill::before {
  content: '';
  width: 7px; height: 7px;
  border-radius: 50%;
  display: inline-block;
}
.ai-status-pill--ready    { background: #dcfce7; color: #166534; }
.ai-status-pill--ready::before  { background: #16a34a; box-shadow: 0 0 0 3px rgba(22,163,74,.18); }
.ai-status-pill--warn     { background: #fef3c7; color: #92400e; }
.ai-status-pill--warn::before   { background: #f59e0b; }
.ai-status-pill--off      { background: #e2e8f0; color: #475569; }
.ai-status-pill--off::before    { background: #94a3b8; }

/* ── CARDS ────────────────────────────────────────────────────── */
.ai-card {
  display: flex; flex-direction: column;
  gap: .85rem;
  padding: 1.1rem 1.25rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 1px 2px rgba(0,0,0,.02);
  transition: border-color .15s, box-shadow .15s;
}
.ai-card:hover { border-color: #d4dae0; }
.ai-card-headrow {
  display: flex; align-items: center; gap: .55rem;
}
.ai-card-headicon {
  display: inline-flex; align-items: center; justify-content: center;
  width: 26px; height: 26px;
  border-radius: 7px;
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
}
.ai-card-title {
  margin: 0;
  font-size: 11px;
  font-weight: 700;
  color: var(--text);
  text-transform: uppercase;
  letter-spacing: .075em;
  font-family: 'DM Sans', sans-serif;
}
.ai-help {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.6;
}
.ai-help--top { margin-top: -.25rem; }
.ai-help a {
  color: var(--bp-blue-dark);
  text-decoration: none;
  border-bottom: 1px dotted currentColor;
}
.ai-help a:hover { color: var(--bp-blue); border-bottom-style: solid; }
.ai-help code {
  font-family: 'DM Mono', monospace;
  font-size: 11px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0 .35rem;
  color: var(--text);
}

/* ── TOGGLE ROW ───────────────────────────────────────────────── */
.ai-toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: .9rem 1.05rem;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 10px;
  transition: background .15s, border-color .15s;
}
.ai-toggle-row--locked { opacity: .82; }
.ai-toggle-text { display: flex; flex-direction: column; gap: .2rem; min-width: 0; }
.ai-toggle-label {
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
  letter-spacing: -.005em;
}
.ai-toggle-hint {
  font-size: 12px;
  color: var(--text-muted);
  display: inline-flex; align-items: center; gap: .35rem;
}
.ai-toggle-hint code {
  font-family: 'DM Mono', monospace;
  font-size: 10.5px;
  background: white;
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0 .35rem;
  color: var(--bp-blue-dark);
}

/* Switch — bigger sibling of .crm-toggle so the primary control reads
   first when the eye scans the card. */
.ai-switch {
  position: relative;
  display: inline-block;
  width: 42px; height: 24px;
  cursor: pointer;
  flex-shrink: 0;
}
.ai-switch input { opacity: 0; width: 0; height: 0; position: absolute; }
.ai-switch-track {
  position: absolute; inset: 0;
  background: #cbd5e1;
  border-radius: 999px;
  transition: background .2s ease;
}
.ai-switch-track::before {
  content: '';
  position: absolute;
  width: 20px; height: 20px;
  left: 2px; top: 2px;
  background: white;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgba(0,0,0,.22), 0 0 0 .5px rgba(0,0,0,.04);
  transition: transform .22s cubic-bezier(.4, 1.4, .6, 1);
}
.ai-switch input:checked + .ai-switch-track { background: var(--bp-blue); }
.ai-switch input:checked + .ai-switch-track::before { transform: translateX(18px); }
.ai-switch input:disabled + .ai-switch-track { opacity: .45; cursor: not-allowed; }

/* ── PROVIDERS ────────────────────────────────────────────────── */
.ai-providers {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: .55rem;
}
.ai-provider {
  position: relative;
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
  padding: .6rem .85rem;
  background: var(--bg);
  border: 1.5px solid var(--border);
  border-radius: 9px;
  cursor: pointer;
  font-family: 'DM Sans', sans-serif;
  font-size: 13px;
  font-weight: 500;
  color: var(--text);
  transition: border-color .15s, background .15s, transform .12s;
}
.ai-provider:hover:not(:disabled) {
  border-color: var(--bp-blue-light);
  transform: translateY(-1px);
}
.ai-provider--active {
  border-color: var(--bp-blue) !important;
  background: var(--bp-blue-pale) !important;
  color: var(--bp-blue-dark);
  font-weight: 600;
}
.ai-provider--disabled {
  cursor: not-allowed;
  opacity: .55;
  background: var(--bg);
}
.ai-provider-name { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ai-provider-tag {
  font-size: 9.5px;
  letter-spacing: .08em;
  text-transform: uppercase;
  font-family: 'DM Mono', monospace;
  color: var(--text-muted);
  background: white;
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: .05rem .45rem;
}
.ai-provider-check {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px;
  border-radius: 50%;
  background: var(--bp-blue);
  color: white;
}

/* ── INPUTS ───────────────────────────────────────────────────── */
.ai-input {
  font-family: 'DM Sans', sans-serif;
  font-size: 13px;
  padding: .55rem .75rem;
  border: 1.5px solid var(--border);
  border-radius: 8px;
  background: white;
  color: var(--text);
  transition: border-color .15s, box-shadow .15s;
  width: 100%;
  box-sizing: border-box;
}
.ai-input:focus {
  outline: none;
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px var(--bp-blue-pale);
}
.ai-input-mono {
  font-family: 'DM Mono', monospace;
  font-size: 12.5px;
  letter-spacing: 0;
}

/* ── KEY ──────────────────────────────────────────────────────── */
.ai-key-set {
  display: flex; align-items: center; gap: .65rem;
  padding: .65rem .85rem;
  background: #f0fdf4;
  border: 1px solid #bbf7d0;
  border-radius: 8px;
  flex-wrap: wrap;
}
.ai-key-dots {
  font-family: 'DM Mono', monospace;
  letter-spacing: .15em;
  font-size: 13px;
  color: #166534;
}
.ai-key-state {
  font-size: 10.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .08em;
  color: #166534;
}
.ai-key-actions { margin-left: auto; display: flex; gap: .35rem; }
.ai-key-clear { color: #b91c1c; }
.ai-key-clear:hover { background: #fef2f2 !important; border-color: #fecaca !important; }
.ai-key-input-row { display: flex; gap: .5rem; align-items: stretch; }
.ai-key-input-row .ai-input { flex: 1; }

/* ── PRESETS ──────────────────────────────────────────────────── */
.ai-presets-label {
  font-size: 10.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .08em;
  color: var(--text-muted);
  margin-top: .25rem;
}
/* PAI-178: lock the model picker to 4 cards per row at the
   standard tab width (1200px). The fixed grid stops cards from
   wrapping into a 2 + 1 / 2 + 2 split that read awkwardly. On
   narrow viewports the responsive override below kicks in. */
.ai-presets-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: .65rem;
}
@media (max-width: 1100px) {
  .ai-presets-grid { grid-template-columns: repeat(3, 1fr); }
}
@media (max-width: 820px) {
  .ai-presets-grid { grid-template-columns: repeat(2, 1fr); }
}
@media (max-width: 540px) {
  .ai-presets-grid { grid-template-columns: 1fr; }
}
.ai-preset {
  position: relative;
  display: flex; flex-direction: column;
  gap: .35rem;
  padding: .8rem .95rem;
  background: var(--bg);
  border: 1.5px solid var(--border);
  border-radius: 10px;
  cursor: pointer;
  text-align: left;
  font-family: 'DM Sans', sans-serif;
  transition: border-color .15s, background .15s, transform .12s, box-shadow .15s;
}
.ai-preset:hover {
  border-color: var(--bp-blue-light);
  transform: translateY(-1px);
  box-shadow: 0 4px 10px rgba(46, 109, 164, .07);
}
.ai-preset--active {
  border-color: var(--bp-blue) !important;
  background: var(--bp-blue-pale) !important;
  box-shadow: 0 4px 12px rgba(46, 109, 164, .14) !important;
}
.ai-preset-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
}
.ai-preset-name {
  font-size: 13px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -.005em;
}
.ai-preset--active .ai-preset-name { color: var(--bp-blue-dark); }
.ai-preset-checkdot {
  display: inline-flex; align-items: center; justify-content: center;
  width: 18px; height: 18px;
  background: var(--bp-blue);
  color: white;
  border-radius: 50%;
  flex-shrink: 0;
}
.ai-preset-slug {
  font-family: 'DM Mono', monospace;
  font-size: 10.5px;
  color: var(--text-muted);
  background: transparent;
  padding: 0;
  word-break: break-all;
  line-height: 1.35;
}
.ai-preset-tags { display: flex; gap: .25rem; flex-wrap: wrap; margin-top: .15rem; }
.ai-preset-tag {
  font-size: 9.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .08em;
  padding: .12rem .45rem;
  border-radius: 999px;
  font-family: 'DM Sans', sans-serif;
  line-height: 1.25;
}
.ai-preset-tag--fast         { background: #dbeafe; color: #1e40af; }
.ai-preset-tag--fastest      { background: #dbeafe; color: #1e40af; }
.ai-preset-tag--quality      { background: #ede9fe; color: #5b21b6; }
.ai-preset-tag--frontier     { background: #ede9fe; color: #5b21b6; }
.ai-preset-tag--open         { background: #fef3c7; color: #92400e; }
.ai-preset-tag--open_weights { background: #fef3c7; color: #92400e; }
.ai-preset-tag--cheap        { background: #d1fae5; color: #065f46; }
.ai-preset-tag--cheapest     { background: #d1fae5; color: #065f46; }
.ai-preset-tag--value        { background: #fce7f3; color: #9d174d; }
.ai-preset-tag--free         { background: #ccfbf1; color: #115e59; }
/* PAI-178: vendor pills on Frontier cards. Each vendor gets its
   own brand-tinted pill so admins can scan the row at a glance:
   Anthropic plum, OpenAI green, xAI black, Google blue. */
.ai-preset-tag--vendor-anthropic { background: #f3e8ff; color: #6b21a8; }
.ai-preset-tag--vendor-openai    { background: #d1fae5; color: #065f46; }
.ai-preset-tag--vendor-xai       { background: #1e293b; color: white; }
.ai-preset-tag--vendor-google    { background: #dbeafe; color: #1d4ed8; }

/* PAI-160: category sections inside the Model card. */
.ai-cat {
  display: flex; flex-direction: column;
  gap: .55rem;
  margin-top: .8rem;
  padding-top: .8rem;
  border-top: 1px dashed var(--border);
}
.ai-cat:first-of-type { margin-top: 0; padding-top: 0; border-top: none; }
.ai-cat-headrow {
  display: flex; align-items: center; gap: .5rem;
  flex-wrap: wrap;
}
.ai-cat-icon {
  display: inline-flex; align-items: center; justify-content: center;
  width: 22px; height: 22px;
  border-radius: 6px;
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
}
.ai-cat-label {
  font-size: 12.5px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -.005em;
}
.ai-cat-betatag {
  font-size: 9.5px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
  padding: .12rem .45rem;
  border-radius: 999px;
  background: #fef3c7;
  color: #92400e;
}
.ai-cat-hint {
  flex: 1;
  font-size: 11.5px;
  color: var(--text-muted);
  line-height: 1.45;
}
@media (max-width: 720px) {
  .ai-cat-hint { flex: none; width: 100%; }
}

.ai-presets-headrow {
  display: flex; align-items: center; justify-content: space-between;
  gap: .5rem;
  margin-top: .25rem;
}
.ai-presets-headrow-left {
  display: inline-flex; align-items: center; gap: .55rem;
  flex-wrap: wrap;
}
.ai-presets-refresh {
  display: inline-flex; align-items: center; gap: .35rem;
}
.ai-stale-pill {
  display: inline-flex; align-items: center; gap: .3rem;
  font-size: 9.5px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
  padding: .12rem .45rem;
  border-radius: 999px;
  background: #fef3c7;
  color: #92400e;
}

/* PAI-160: extra meta line on each preset card. Keeps pricing +
   context next to the slug without crowding the tag row. */
.ai-preset-meta {
  display: flex; gap: .4rem; flex-wrap: wrap;
  margin-top: .15rem;
}
.ai-preset-meta-bit {
  font-family: 'DM Mono', monospace;
  font-size: 10.5px;
  color: var(--text-muted);
  background: white;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: .08rem .4rem;
}
.ai-preset-meta-bit--free {
  background: #ecfdf5;
  border-color: #a7f3d0;
  color: #166534;
}

/* ── INVARIANTS DISCLOSURE ────────────────────────────────────── */
.ai-invariants {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12px;
}
.ai-invariants > summary {
  display: flex; align-items: center; gap: .5rem;
  padding: .55rem .8rem;
  cursor: pointer;
  list-style: none;
  color: var(--text);
  font-weight: 600;
  user-select: none;
  font-size: 12px;
}
.ai-invariants > summary::-webkit-details-marker { display: none; }
.ai-invariants > summary::after {
  content: '+';
  margin-left: auto;
  color: var(--text-muted);
  font-weight: 400;
  font-size: 16px;
  line-height: 1;
  font-family: 'DM Mono', monospace;
}
.ai-invariants[open] > summary::after { content: '−'; }
.ai-invariants[open] > summary { border-bottom: 1px solid var(--border); }
.ai-invariants-list {
  margin: 0;
  padding: .65rem 1.25rem .85rem 2rem;
  color: var(--text-muted);
  line-height: 1.65;
}
.ai-invariants-list li { margin-bottom: .15rem; }
.ai-invariants-list code {
  font-family: 'DM Mono', monospace;
  font-size: 11px;
  background: white;
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0 .35rem;
  color: var(--text);
}

/* ── TEXTAREA ─────────────────────────────────────────────────── */
.ai-textarea {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-size: 12px;
  line-height: 1.6;
  padding: .8rem .95rem;
  border: 1.5px solid var(--border);
  border-radius: 8px;
  background: white;
  color: var(--text);
  resize: vertical;
  min-height: 220px;
  transition: border-color .15s, box-shadow .15s;
  width: 100%;
  box-sizing: border-box;
}
.ai-textarea:focus {
  outline: none;
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px var(--bp-blue-pale);
}

/* ── BANNERS ──────────────────────────────────────────────────── */
.ai-loading { color: var(--text-muted); padding: .5rem 0; font-size: 13px; }
.ai-banner {
  margin: 0;
  padding: .55rem .85rem;
  border-radius: 8px;
  font-size: 13px;
  display: inline-flex; align-items: center; gap: .45rem;
}
.ai-banner--error {
  background: #fef2f2; color: #b91c1c;
  border: 1px solid #fecaca;
}
.ai-banner--ok {
  background: #f0fdf4; color: #166534;
  border: 1px solid #bbf7d0;
}

/* ── ACTIONS FOOTER ───────────────────────────────────────────── */
.ai-actions {
  display: flex; align-items: center; justify-content: space-between;
  gap: 1rem;
  padding: .85rem 1.25rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 1px 2px rgba(0,0,0,.02);
  margin-top: .25rem;
}

/* PAI-180: sticky variant for the action bar at the top of the
   tab. The hero scrolls naturally; this bar pins to top:0 so
   admins always have Test + Save reachable while editing the
   long form below. Backdrop-blur keeps the content scrolling
   under it legible against the soft tab background. */
.ai-actions--sticky {
  position: sticky;
  top: 0;
  z-index: 5;
  margin-top: 0;
  /* Slightly heavier shadow when stuck so the divide between bar
     and content is obvious during scroll. */
  box-shadow: 0 4px 14px rgba(15, 35, 65, .06), 0 1px 2px rgba(0,0,0,.04);
  backdrop-filter: saturate(1.1) blur(6px);
  -webkit-backdrop-filter: saturate(1.1) blur(6px);
}

.ai-actions-meta {
  font-size: 12px;
  color: var(--text-muted);
}
.ai-actions-meta time { font-weight: 600; color: var(--text); }
.ai-save {
  display: inline-flex; align-items: center; gap: .4rem;
  font-weight: 600;
  padding: .55rem 1.15rem;
}

/* PAI-159: action footer holds Test + Save side by side. */
.ai-actions-buttons {
  display: flex; gap: .5rem; align-items: center;
}
.ai-test-btn {
  display: inline-flex; align-items: center; gap: .4rem;
  font-weight: 600;
  padding: .55rem 1rem;
}

/* PAI-159: result card is its own surface — softer than a banner so
   admins who click Test repeatedly aren't visually shouted at. */
.ai-test-result {
  display: flex; flex-direction: column; gap: .55rem;
  padding: .85rem 1rem;
  border-radius: 12px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  font-size: 13px;
}
.ai-test-result--ok {
  border-color: #bbf7d0;
  background: #f0fdf4;
  color: #166534;
}
.ai-test-result--fail {
  border-color: #fecaca;
  background: #fef2f2;
  color: #991b1b;
}
.ai-test-result-head {
  display: flex; align-items: center; gap: .55rem;
}
.ai-test-result-msg {
  flex: 1;
  font-weight: 600;
  letter-spacing: -.005em;
  line-height: 1.4;
}
.ai-test-result-x {
  background: none; border: none;
  color: inherit; opacity: .65;
  cursor: pointer; font-size: 18px; line-height: 1;
  padding: 0 .25rem;
}
.ai-test-result-x:hover { opacity: 1; }
.ai-test-result-body {
  display: flex; gap: .55rem; align-items: baseline;
  font-family: 'DM Mono', monospace;
  font-size: 12.5px;
  padding: .5rem .65rem;
  background: rgba(255,255,255,.55);
  border: 1px solid rgba(0,0,0,.04);
  border-radius: 8px;
  line-height: 1.55;
}
.ai-test-result-label {
  font-family: 'DM Sans', sans-serif;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
  color: currentColor;
  opacity: .7;
  flex-shrink: 0;
}
.ai-test-result-quote {
  word-break: break-word;
  color: var(--text);
}
.ai-test-result-meta {
  display: flex; gap: .35rem; flex-wrap: wrap;
}
.ai-test-result-pill {
  display: inline-flex; align-items: center; gap: .3rem;
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: .04em;
  padding: .15rem .55rem;
  border-radius: 999px;
  background: rgba(255,255,255,.7);
  border: 1px solid rgba(0,0,0,.06);
  color: currentColor;
  font-family: 'DM Mono', monospace;
}
.ai-test-result-pill--ok   { background: #16a34a; color: white; border-color: transparent; }
.ai-test-result-pill--fail { background: #dc2626; color: white; border-color: transparent; }

/* PAI-161: usage card. */
.ai-usage-summary {
  display: flex; flex-wrap: wrap; gap: .4rem;
  margin-bottom: .25rem;
}
.ai-usage-pill {
  display: inline-flex; align-items: center; gap: .3rem;
  font-family: 'DM Mono', monospace;
  font-size: 11.5px;
  padding: .25rem .6rem;
  border-radius: 999px;
  background: white;
  border: 1px solid var(--border);
  color: var(--text);
}
.ai-usage-pill strong { color: var(--bp-blue-dark); font-weight: 700; }
.ai-usage-pill--day { background: var(--bp-blue-pale); border-color: transparent; color: var(--bp-blue-dark); }
.ai-usage-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
  margin-top: .25rem;
}
.ai-usage-table thead th {
  font-family: 'DM Sans', sans-serif;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
  color: var(--text-muted);
  padding: .35rem .6rem;
  text-align: left;
  border-bottom: 1px solid var(--border);
}
.ai-usage-table tbody td {
  padding: .4rem .6rem;
  border-bottom: 1px solid #f1f5f9;
  color: var(--text);
}
.ai-usage-num { text-align: right; font-family: 'DM Mono', monospace; }
.ai-usage-row--over td { background: #fef2f2; }
.ai-usage-admin-tag {
  margin-left: .4rem;
  font-size: 9.5px; font-weight: 700; text-transform: uppercase; letter-spacing: .08em;
  background: #ede9fe; color: #5b21b6;
  padding: .1rem .4rem; border-radius: 999px;
}
.ai-usage-override-tag {
  margin-left: .35rem;
  font-size: 9px; font-weight: 700; text-transform: uppercase; letter-spacing: .08em;
  background: #fef3c7; color: #92400e;
  padding: .05rem .35rem; border-radius: 6px;
}
.ai-usage-over-tag {
  font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: .08em;
  background: #fee2e2; color: #991b1b;
  padding: .1rem .4rem; border-radius: 999px;
}

.spin { animation: ai-tab-spin 1s linear infinite; }
@keyframes ai-tab-spin { to { transform: rotate(360deg); } }

/* ── RESPONSIVE ───────────────────────────────────────────────── */
@media (max-width: 640px) {
  .ai-hero { flex-direction: column; align-items: flex-start; gap: .85rem; padding: 1.1rem 1.2rem; }
  .ai-hero-iconwrap { width: 42px; height: 42px; border-radius: 11px; }
  .ai-hero-title { font-size: 16px; }
  .ai-toggle-row { flex-direction: column; align-items: flex-start; gap: .8rem; }
  .ai-actions { flex-direction: column; align-items: stretch; gap: .65rem; }
  .ai-save { width: 100%; justify-content: center; }
}
</style>
