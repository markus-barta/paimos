<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-149. Admin tab for the LLM text-optimization feature (PAI-146).

 - The api_key field follows the same "currently set / replace" pattern
   as the CRM tab so admins don't accidentally clear an existing key by
   saving with the field blank. The omitted/null payload is what tells
   the backend to leave the stored key untouched.
 - Model presets are local UI hints, not validated server-side. Admins
   can free-type any OpenRouter slug; the presets just save typing for
   the common ones. Keep this list small — a long curated catalog rots
   fast as providers add and remove models.
 - The optimize-instruction textarea seeds with the backend default the
   first time the tab is opened on a fresh install. Subsequent edits
   round-trip the admin's text verbatim.
-->
<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { api, errMsg } from '@/api/client'

interface AISettings {
  enabled: boolean
  provider: string
  model: string
  api_key: string
  optimize_instruction: string
  updated_at: string
}

// OpenRouter slugs that work well for short editorial rewrites today.
// Intentionally short. Any string can be typed in the model field; this
// is just a "save typing" affordance.
const MODEL_PRESETS: { slug: string; label: string }[] = [
  { slug: 'anthropic/claude-3.5-haiku',          label: 'Claude 3.5 Haiku — fast, cheap' },
  { slug: 'anthropic/claude-sonnet-4.5',         label: 'Claude Sonnet 4.5 — quality default' },
  { slug: 'openai/gpt-4o-mini',                  label: 'GPT-4o mini — fast, cheap' },
  { slug: 'openai/gpt-4o',                       label: 'GPT-4o — quality default' },
  { slug: 'meta-llama/llama-3.3-70b-instruct',   label: 'Llama 3.3 70B — open weights' },
]

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
    // We never echo the key into the form; the placeholder dots below
    // tell the admin a key is on file.
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

async function onSave() {
  saving.value = true
  saveError.value = ''
  saveOK.value = false
  try {
    // Build a payload where api_key is null when the admin didn't touch
    // it, "" when they cleared it, or the typed value when they set a
    // new one. The backend only writes the column when api_key is not
    // null, mirroring the CRM secret-field convention.
    const body: Record<string, unknown> = {
      enabled: form.enabled,
      provider: form.provider,
      model: form.model.trim(),
      optimize_instruction: form.optimize_instruction,
    }
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

const canEnable = computed(() => hasStoredKey.value && form.model.trim() !== '')
const enableHint = computed(() => {
  if (!hasStoredKey.value) return 'Add an OpenRouter API key first.'
  if (form.model.trim() === '') return 'Pick a model first.'
  return ''
})

function applyPreset(slug: string) {
  form.model = slug
}
</script>

<template>
  <div class="section">
    <div class="section-header">
      <h2 class="section-title">AI text optimization</h2>
      <p class="section-desc">
        Adds an inline <strong>AI</strong> action to multiline fields
        (description, acceptance criteria, notes) so authors can polish
        wording without leaving the editor. Optimized output is shown in
        a diff preview before anything is replaced — nothing is rewritten
        silently.
      </p>
      <p class="section-desc">
        Today this calls <a href="https://openrouter.ai/" target="_blank" rel="noopener">OpenRouter</a>
        with the configured model. The provider layer is abstracted so a
        future local-model integration (PAI-122) can be plugged in without
        changing the editor experience.
      </p>
    </div>

    <div v-if="loading" class="ai-loading">Loading…</div>
    <div v-else-if="loadError" class="ai-banner-error">{{ loadError }}</div>

    <form v-else class="ai-form" @submit.prevent="onSave">
      <!-- Enable toggle -->
      <div class="ai-row">
        <label class="ai-toggle-row">
          <input
            type="checkbox"
            v-model="form.enabled"
            :disabled="!canEnable"
          />
          <span>Enable AI optimization</span>
          <span v-if="!canEnable" class="ai-hint">{{ enableHint }}</span>
        </label>
      </div>

      <!-- Provider — single option in v1, but rendered so admins see the slot -->
      <div class="ai-row">
        <label class="ai-label">Provider</label>
        <select v-model="form.provider">
          <option value="openrouter">OpenRouter</option>
        </select>
        <p class="ai-help">
          Local-model providers (Ollama, LM Studio, vLLM, llama.cpp) are
          tracked under PAI-122 and will appear in this dropdown when ready.
        </p>
      </div>

      <!-- API key with currently-set / replace pattern -->
      <div class="ai-row">
        <label class="ai-label">OpenRouter API key</label>
        <div v-if="!replacingKey && hasStoredKey" class="ai-secret-set">
          <span class="ai-secret-dots">•••••</span>
          <span class="ai-secret-meta">currently set</span>
          <button type="button" class="btn btn-ghost btn-sm" @click="startReplace">
            Replace
          </button>
        </div>
        <div v-else class="ai-secret-input-row">
          <input
            v-model="form.api_key"
            type="password"
            autocomplete="new-password"
            placeholder="sk-or-…"
          />
          <button
            v-if="hasStoredKey"
            type="button"
            class="btn btn-ghost btn-sm"
            @click="cancelReplace"
          >
            Cancel
          </button>
        </div>
        <p class="ai-help">
          Get a key at <a href="https://openrouter.ai/keys" target="_blank" rel="noopener">openrouter.ai/keys</a>.
          Stored unencrypted in the PAIMOS database — keep the data volume on
          encrypted storage if your threat model needs that.
        </p>
      </div>

      <!-- Model -->
      <div class="ai-row">
        <label class="ai-label">Model</label>
        <input
          v-model="form.model"
          type="text"
          placeholder="anthropic/claude-3.5-haiku"
          spellcheck="false"
        />
        <div class="ai-presets">
          <span class="ai-presets-label">Presets:</span>
          <button
            v-for="p in MODEL_PRESETS" :key="p.slug"
            type="button"
            class="ai-preset-btn"
            :title="p.slug"
            @click="applyPreset(p.slug)"
          >{{ p.label }}</button>
        </div>
      </div>

      <!-- Optimize instruction -->
      <div class="ai-row">
        <label class="ai-label">Optimization instruction</label>
        <textarea
          v-model="form.optimize_instruction"
          rows="10"
          spellcheck="false"
        ></textarea>
        <p class="ai-help">
          Layered inside a fixed PAIMOS-owned wrapper that always preserves
          markdown structure, technical meaning, and architecture-significant
          intent. Use this field to add project-specific tone guidance — not
          to override the safety rules.
        </p>
      </div>

      <p v-if="saveError" class="ai-banner-error">{{ saveError }}</p>
      <p v-if="saveOK" class="ai-banner-ok">Saved.</p>

      <div class="ai-form-actions">
        <button type="submit" class="btn btn-primary" :disabled="saving">
          {{ saving ? 'Saving…' : 'Save' }}
        </button>
      </div>
    </form>
  </div>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.ai-loading { color: var(--text-muted); padding: 1rem; }
.ai-banner-error {
  background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca;
  padding: .5rem .75rem; border-radius: var(--radius); font-size: 13px; margin: 0;
}
.ai-banner-ok {
  background: #f0fdf4; color: #166534; border: 1px solid #bbf7d0;
  padding: .5rem .75rem; border-radius: var(--radius); font-size: 13px; margin: 0;
}

.ai-form { display: flex; flex-direction: column; gap: 1.1rem; max-width: 720px; }
.ai-row { display: flex; flex-direction: column; gap: .35rem; }

.ai-label {
  font-size: 12px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .05em;
}
.ai-help { font-size: 12px; color: var(--text-muted); margin: 0; line-height: 1.55; }
.ai-help a { color: var(--bp-blue-dark); }
.ai-hint { font-size: 12px; color: var(--text-muted); margin-left: .5rem; }

.ai-toggle-row { display: flex; align-items: center; gap: .55rem; font-weight: 500; }

.ai-secret-set {
  display: flex; align-items: center; gap: .5rem;
  background: #f0fdf4; border: 1px solid #bbf7d0;
  border-radius: var(--radius); padding: .35rem .65rem;
}
.ai-secret-dots {
  font-family: 'DM Mono', monospace; color: #166534;
  letter-spacing: .25em; font-size: 14px;
}
.ai-secret-meta { font-size: 12px; color: #166534; font-weight: 600; }
.ai-secret-input-row { display: flex; gap: .5rem; align-items: center; }
.ai-secret-input-row input { flex: 1; }

.ai-presets {
  display: flex; flex-wrap: wrap; gap: .35rem; align-items: center;
  margin-top: .25rem;
}
.ai-presets-label { font-size: 11px; color: var(--text-muted); font-weight: 600; text-transform: uppercase; letter-spacing: .05em; margin-right: .25rem; }
.ai-preset-btn {
  background: var(--bg); border: 1px solid var(--border); border-radius: 999px;
  padding: .15rem .55rem; font-size: 11px; cursor: pointer;
  font-family: 'DM Mono', monospace; color: var(--text-muted);
  transition: background .12s, color .12s, border-color .12s;
}
.ai-preset-btn:hover {
  background: var(--bp-blue-pale); border-color: var(--bp-blue-light); color: var(--bp-blue-dark);
}

textarea {
  font-family: 'DM Mono', monospace;
  font-size: 12px;
  line-height: 1.55;
  padding: .5rem .65rem;
  border: 1px solid var(--border); border-radius: var(--radius);
  background: #fff; resize: vertical;
}

.ai-form-actions { display: flex; justify-content: flex-end; }
</style>
