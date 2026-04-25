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

interface AISettings {
  enabled: boolean
  provider: string
  model: string
  api_key: string
  optimize_instruction: string
  updated_at: string
}

type PresetTag = 'fast' | 'quality' | 'open' | 'cheap'

interface ModelPreset {
  slug: string
  name: string
  tags: PresetTag[]
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

// Curated, NOT a catalog. Anything can be typed into the Model input.
const MODEL_PRESETS: ModelPreset[] = [
  { slug: 'anthropic/claude-3.5-haiku',          name: 'Claude 3.5 Haiku',  tags: ['fast', 'cheap'] },
  { slug: 'anthropic/claude-sonnet-4.5',         name: 'Claude Sonnet 4.5', tags: ['quality'] },
  { slug: 'openai/gpt-4o-mini',                  name: 'GPT-4o mini',       tags: ['fast', 'cheap'] },
  { slug: 'openai/gpt-4o',                       name: 'GPT-4o',            tags: ['quality'] },
  { slug: 'meta-llama/llama-3.3-70b-instruct',   name: 'Llama 3.3 70B',     tags: ['open'] },
]

const TAG_LABEL: Record<PresetTag, string> = {
  fast: 'Fast',
  quality: 'Quality',
  open: 'Open weights',
  cheap: 'Cheap',
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
onMounted(load)

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
        <input
          v-model="form.model"
          type="text"
          placeholder="anthropic/claude-3.5-haiku"
          class="ai-input ai-input-mono"
          spellcheck="false"
        />
        <div class="ai-presets-label">Quick picks</div>
        <div class="ai-presets-grid">
          <button
            v-for="p in MODEL_PRESETS" :key="p.slug"
            type="button"
            :class="['ai-preset', { 'ai-preset--active': form.model.trim() === p.slug }]"
            :title="p.slug"
            @click="applyPreset(p.slug)"
          >
            <div class="ai-preset-row">
              <strong class="ai-preset-name">{{ p.name }}</strong>
              <span v-if="form.model.trim() === p.slug" class="ai-preset-checkdot" aria-hidden="true">
                <AppIcon name="check" :size="10" />
              </span>
            </div>
            <code class="ai-preset-slug">{{ p.slug }}</code>
            <div class="ai-preset-tags">
              <span
                v-for="t in p.tags" :key="t"
                :class="['ai-preset-tag', `ai-preset-tag--${t}`]"
              >{{ TAG_LABEL[t] }}</span>
            </div>
          </button>
        </div>
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

      <!-- ── BANNERS ─────────────────────────────────────────────── -->
      <p v-if="saveError" class="ai-banner ai-banner--error">
        <AppIcon name="alert-triangle" :size="14" /> {{ saveError }}
      </p>
      <p v-if="saveOK" class="ai-banner ai-banner--ok">
        <AppIcon name="check-circle" :size="14" /> Settings saved.
      </p>

      <!-- ── ACTIONS ─────────────────────────────────────────────── -->
      <footer class="ai-actions">
        <span class="ai-actions-meta">
          <template v-if="form.updated_at">
            Last saved <time :datetime="form.updated_at">{{ relTime(form.updated_at) }}</time>
          </template>
          <template v-else>—</template>
        </span>
        <button
          type="button"
          class="btn btn-primary ai-save"
          :disabled="saving"
          @click="onSave"
        >
          <AppIcon v-if="saving" name="loader-circle" :size="13" class="spin" />
          {{ saving ? 'Saving…' : 'Save changes' }}
        </button>
      </footer>
    </template>
  </div>
</template>

<style scoped>
/* The whole tab: capped width so wide screens don't stretch the form
   into uselessness, but inputs inside still flex to the column edge. */
.ai-tab {
  display: flex; flex-direction: column;
  gap: 1rem;
  max-width: 920px;
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
.ai-presets-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: .65rem;
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
.ai-preset-tag--fast    { background: #dbeafe; color: #1e40af; }
.ai-preset-tag--quality { background: #ede9fe; color: #5b21b6; }
.ai-preset-tag--open    { background: #fef3c7; color: #92400e; }
.ai-preset-tag--cheap   { background: #d1fae5; color: #065f46; }

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
