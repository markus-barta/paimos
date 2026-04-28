<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-105. Admin Integrations CRM tab. Provider cards rendered from
 /api/integrations/crm; the form below each card is generated from the
 provider's ConfigSchema, so adding a new in-tree provider lights it up
 with no UI change. Secret fields are never echoed: they show as a
 "Currently set · Replace" affordance until the admin chooses to
 overwrite them.
-->
<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { api, errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import { useExternalProvider } from '@/composables/useExternalProvider'
import type { CRMTestResult, ExternalProvider, ExternalProviderConfig, ExternalProviderConfigField } from '@/types'

const providers = ref<ExternalProvider[]>([])
const loading = ref(true)
const loadError = ref('')

const expanded = ref<Record<string, boolean>>({})
const configs = reactive<Record<string, ExternalProviderConfig | null>>({})
const drafts = reactive<Record<string, Record<string, string>>>({})
// Tracks which secret fields have a pending replacement value typed in
// (so the input shows when the admin clicks "Replace" but hides again on
// save / cancel).
const replacing = reactive<Record<string, Record<string, boolean>>>({})
// PAI-259: per-(provider × field) eye-toggle state for secret inputs.
const showSecret = reactive<Record<string, Record<string, boolean>>>({})
const saving = reactive<Record<string, boolean>>({})
const saveError = reactive<Record<string, string>>({})
const togglingEnabled = reactive<Record<string, boolean>>({})

// PAI-259: per-provider Test integration state.
interface TestLog {
  ts: number
  ok: boolean
  message: string
  lines: string[]
}
const testing = reactive<Record<string, boolean>>({})
const testLogs = reactive<Record<string, TestLog[]>>({})

const { refresh: refreshProviderCache } = useExternalProvider()

async function loadProviders() {
  loading.value = true
  loadError.value = ''
  try {
    providers.value = await api.get<ExternalProvider[]>('/integrations/crm')
  } catch (e: unknown) {
    loadError.value = errMsg(e, 'Failed to load CRM providers.')
  } finally {
    loading.value = false
  }
}
onMounted(loadProviders)

async function loadConfig(id: string) {
  const cfg = await api.get<ExternalProviderConfig>(`/integrations/crm/${id}/config`)
  configs[id] = cfg
  // Seed the draft map from current values; secret fields start blank
  // (the stored value is never echoed back to the client).
  const draft: Record<string, string> = {}
  for (const f of cfg.fields) {
    draft[f.key] = f.type === 'secret' ? '' : (f.value ?? '')
  }
  drafts[id] = draft
  replacing[id] = {}
}

async function toggleExpand(p: ExternalProvider) {
  expanded.value[p.id] = !expanded.value[p.id]
  if (expanded.value[p.id] && !configs[p.id]) {
    try { await loadConfig(p.id) } catch (e) { saveError[p.id] = errMsg(e) }
  }
}

async function toggleEnabled(p: ExternalProvider) {
  // Refuse to flip on a misconfigured provider — same guard the backend
  // enforces, but surface it client-side too so the toggle doesn't snap
  // back after a save/refetch round trip.
  togglingEnabled[p.id] = true
  saveError[p.id] = ''
  try {
    const next = !p.enabled
    if (next && !p.configured) {
      throw new Error('Configure this provider before enabling.')
    }
    const res = await api.put<{ enabled: boolean }>(`/integrations/crm/${p.id}/enabled`, { enabled: next })
    p.enabled = res.enabled
    refreshProviderCache()
  } catch (e: unknown) {
    saveError[p.id] = errMsg(e, 'Failed to toggle provider.')
  } finally {
    togglingEnabled[p.id] = false
  }
}

function startReplace(providerId: string, fieldKey: string) {
  replacing[providerId] = { ...(replacing[providerId] ?? {}), [fieldKey]: true }
  drafts[providerId][fieldKey] = ''
}
function cancelReplace(providerId: string, fieldKey: string) {
  replacing[providerId] = { ...(replacing[providerId] ?? {}), [fieldKey]: false }
  drafts[providerId][fieldKey] = ''
  // Re-mask the field on cancel so a subsequent Replace doesn't reveal
  // whatever the admin typed last.
  showSecret[providerId] = { ...(showSecret[providerId] ?? {}), [fieldKey]: false }
}
function clearSecret(providerId: string, fieldKey: string) {
  // "Clear" sends an empty string; the backend treats that as detach.
  drafts[providerId][fieldKey] = ''
  replacing[providerId] = { ...(replacing[providerId] ?? {}), [fieldKey]: true }
}
function toggleShowSecret(providerId: string, fieldKey: string) {
  const current = showSecret[providerId]?.[fieldKey] ?? false
  showSecret[providerId] = { ...(showSecret[providerId] ?? {}), [fieldKey]: !current }
}

async function saveConfig(p: ExternalProvider) {
  saving[p.id] = true
  saveError[p.id] = ''
  // Build the patch: only include keys with values; for secrets, only
  // include keys the admin actively replaced (so an empty input on a
  // pre-set secret doesn't accidentally clear it).
  const cfg = configs[p.id]
  if (!cfg) return
  const patch: Record<string, string | null> = {}
  for (const f of cfg.fields) {
    const draftValue = drafts[p.id][f.key] ?? ''
    if (f.type === 'secret') {
      // PAI-259: secrets get sent in two scenarios:
      //  1. Replace flow: admin clicked "Replace" on a previously-set
      //     secret — empty input means clear, non-empty means overwrite.
      //  2. First-time setup: no value persisted yet (`!f.has_value`)
      //     and the admin typed something. Previously this case was
      //     silently dropped, which made the backend reject the save
      //     with "must not be empty" against a clearly non-empty input.
      const isReplaceFlow = replacing[p.id]?.[f.key]
      const isFirstTimeSetup = !f.has_value
      if (isReplaceFlow) {
        patch[f.key] = draftValue
      } else if (isFirstTimeSetup && draftValue !== '') {
        patch[f.key] = draftValue
      }
    } else {
      patch[f.key] = draftValue
    }
  }
  try {
    const updated = await api.put<ExternalProviderConfig>(
      `/integrations/crm/${p.id}/config`,
      { values: patch },
    )
    configs[p.id] = updated
    // Reset draft from the freshly returned config (secret fields empty;
    // non-secret fields show the persisted value).
    const newDraft: Record<string, string> = {}
    for (const f of updated.fields) {
      newDraft[f.key] = f.type === 'secret' ? '' : (f.value ?? '')
    }
    drafts[p.id] = newDraft
    replacing[p.id] = {}
    // Re-mask any revealed secret inputs after save — typed value is
    // now persisted; nothing to verify against the visible draft.
    showSecret[p.id] = {}
    // Refetch the provider list so configured/enabled flags update.
    await loadProviders()
    refreshProviderCache()
  } catch (e: unknown) {
    saveError[p.id] = errMsg(e, 'Save failed.')
  } finally {
    saving[p.id] = false
  }
}

// PAI-259: Test integration. Calls the new POST endpoint, appends a
// timestamped entry to the per-provider log. Doesn't include the
// secret on the wire — the backend uses the persisted config, not
// what's currently in the unsaved draft.
async function testProvider(p: ExternalProvider) {
  if (testing[p.id]) return
  testing[p.id] = true
  try {
    const result = await api.post<CRMTestResult>(`/integrations/crm/${p.id}/test`, {})
    appendTestLog(p.id, {
      ts: Date.now(),
      ok: !!result.ok,
      message: result.message ?? (result.ok ? 'OK' : 'Test failed'),
      lines: result.lines ?? [],
    })
  } catch (e: unknown) {
    appendTestLog(p.id, {
      ts: Date.now(),
      ok: false,
      message: errMsg(e, 'Test request failed.'),
      lines: [],
    })
  } finally {
    testing[p.id] = false
  }
}

function appendTestLog(providerId: string, entry: TestLog) {
  const existing = testLogs[providerId] ?? []
  // Keep the last 20 attempts so the panel doesn't grow unbounded
  // across a long admin session.
  testLogs[providerId] = [entry, ...existing].slice(0, 20)
}

function clearTestLog(providerId: string) {
  testLogs[providerId] = []
}

function fmtTime(ts: number): string {
  const d = new Date(ts)
  return d.toLocaleTimeString(undefined, { hour12: false })
}

function statusLabel(p: ExternalProvider): string {
  if (!p.configured) return 'Needs configuration'
  if (!p.enabled)    return 'Disabled'
  return 'Enabled'
}
function statusClass(p: ExternalProvider): string {
  if (!p.configured) return 'crm-status--needs'
  if (!p.enabled)    return 'crm-status--off'
  return 'crm-status--on'
}

const hasProviders = computed(() => providers.value.length > 0)
</script>

<template>
  <div class="section">
    <div class="section-header">
      <h2 class="section-title">CRM Providers</h2>
      <p class="section-desc">
        Connect external CRMs (HubSpot, Pipedrive, …) so customers can be
        imported and re-synced from inside PAIMOS. Manual customer entry
        works without any provider configured.
      </p>
    </div>

    <div v-if="loading" class="crm-loading">Loading providers…</div>
    <div v-else-if="loadError" class="crm-banner-error">{{ loadError }}</div>

    <div v-else-if="!hasProviders" class="crm-empty">
      <AppIcon name="puzzle" :size="22" />
      <div>
        <strong>No CRM providers compiled in.</strong>
        <p>
          Add a Go provider under <code>backend/handlers/crm/</code> and
          blank-import it from <code>main.go</code>. See
          <a href="https://github.com/markus-barta/paimos/blob/main/docs/CRM_PROVIDERS.md" target="_blank" rel="noopener">
            developer docs
          </a> for a worked example.
        </p>
      </div>
    </div>

    <div v-else class="crm-grid">
      <article v-for="p in providers" :key="p.id" :class="['crm-card', { 'crm-card--open': expanded[p.id] }]">
        <header class="crm-head" @click="toggleExpand(p)">
          <div class="crm-head-id">
            <img v-if="p.logo_url" :src="p.logo_url" :alt="p.name" class="crm-logo" />
            <AppIcon v-else name="globe" :size="20" />
            <div class="crm-id-text">
              <h3 class="crm-name">{{ p.name }}</h3>
              <code class="crm-id">{{ p.id }}</code>
            </div>
          </div>
          <div class="crm-head-status">
            <span :class="['crm-status', statusClass(p)]">{{ statusLabel(p) }}</span>
            <label class="crm-toggle" :title="p.configured ? '' : 'Configure first to enable'" @click.stop>
              <input
                type="checkbox"
                :checked="p.enabled"
                :disabled="!p.configured || togglingEnabled[p.id]"
                @change="toggleEnabled(p)"
              />
              <span class="crm-toggle-track" />
            </label>
            <AppIcon
              :name="expanded[p.id] ? 'chevron-up' : 'chevron-down'"
              :size="16"
              class="crm-caret"
            />
          </div>
        </header>

        <div v-if="expanded[p.id]" class="crm-body">
          <div v-if="!configs[p.id]" class="crm-loading-inline">Loading config…</div>

          <form v-else class="crm-form" @submit.prevent="saveConfig(p)">
            <div v-for="f in configs[p.id]!.fields" :key="f.key" class="crm-field">
              <label>
                {{ f.label }}
                <span v-if="!f.required" class="crm-field-opt">— optional</span>
              </label>
              <p v-if="f.help" class="crm-field-help">{{ f.help }}</p>

              <!-- Secret fields: show "currently set" pill until the admin
                   clicks Replace, then become a real input. -->
              <template v-if="f.type === 'secret'">
                <div v-if="!replacing[p.id]?.[f.key] && f.has_value" class="crm-secret-set">
                  <span class="crm-secret-dots">•••••</span>
                  <span class="crm-secret-meta">currently set</span>
                  <button type="button" class="btn btn-ghost btn-sm" @click="startReplace(p.id, f.key)">
                    <AppIcon name="key-round" :size="12" /> Replace
                  </button>
                  <button type="button" class="btn btn-ghost btn-sm crm-secret-clear" @click="clearSecret(p.id, f.key)">
                    Clear
                  </button>
                </div>
                <div v-else class="crm-secret-input-row">
                  <div class="crm-secret-input-wrap">
                    <input
                      v-model="drafts[p.id][f.key]"
                      :type="showSecret[p.id]?.[f.key] ? 'text' : 'password'"
                      autocomplete="new-password"
                      :placeholder="f.placeholder ?? ''"
                    />
                    <button
                      type="button"
                      class="crm-secret-eye"
                      :title="showSecret[p.id]?.[f.key] ? 'Hide value' : 'Show value'"
                      :aria-label="showSecret[p.id]?.[f.key] ? 'Hide value' : 'Show value'"
                      @click="toggleShowSecret(p.id, f.key)"
                    >
                      <AppIcon :name="showSecret[p.id]?.[f.key] ? 'eye-off' : 'eye'" :size="14" />
                    </button>
                  </div>
                  <button
                    v-if="f.has_value"
                    type="button"
                    class="btn btn-ghost btn-sm"
                    @click="cancelReplace(p.id, f.key)"
                  >
                    Cancel
                  </button>
                </div>
              </template>

              <select v-else-if="f.type === 'select'" v-model="drafts[p.id][f.key]">
                <option value="">—</option>
                <option v-for="o in f.options ?? []" :key="o.value" :value="o.value">
                  {{ o.label }}
                </option>
              </select>

              <input
                v-else-if="f.type === 'number'"
                v-model="drafts[p.id][f.key]"
                type="number"
                :placeholder="f.placeholder ?? ''"
              />

              <input
                v-else
                v-model="drafts[p.id][f.key]"
                type="text"
                :placeholder="f.placeholder ?? ''"
              />
            </div>

            <p v-if="saveError[p.id]" class="crm-banner-error">{{ saveError[p.id] }}</p>

            <div class="crm-form-actions">
              <button
                v-if="p.test_supported"
                type="button"
                class="btn btn-ghost"
                :disabled="!p.configured || testing[p.id]"
                :title="!p.configured ? 'Save configuration first' : 'Run a connection test against the live API'"
                @click="testProvider(p)"
              >
                <AppIcon name="plug" :size="13" />
                {{ testing[p.id] ? 'Testing…' : 'Test integration' }}
              </button>
              <button type="submit" class="btn btn-primary" :disabled="saving[p.id]">
                {{ saving[p.id] ? 'Saving…' : 'Save configuration' }}
              </button>
            </div>

            <!-- PAI-259: inline log panel for the most recent test attempts.
                 Stays empty (and hidden) until the admin runs the first
                 test in this session. -->
            <section v-if="(testLogs[p.id]?.length ?? 0) > 0" class="crm-testlog">
              <header class="crm-testlog-head">
                <span class="crm-testlog-title">Test integration · log</span>
                <button type="button" class="btn btn-ghost btn-sm" @click="clearTestLog(p.id)">
                  Clear
                </button>
              </header>
              <ul class="crm-testlog-list">
                <li
                  v-for="(entry, idx) in testLogs[p.id]"
                  :key="entry.ts + ':' + idx"
                  :class="['crm-testlog-entry', entry.ok ? 'crm-testlog-entry--ok' : 'crm-testlog-entry--fail']"
                >
                  <div class="crm-testlog-row">
                    <span class="crm-testlog-time">{{ fmtTime(entry.ts) }}</span>
                    <span class="crm-testlog-status">{{ entry.ok ? 'OK' : 'FAIL' }}</span>
                    <span class="crm-testlog-message">{{ entry.message }}</span>
                  </div>
                  <ul v-if="entry.lines.length" class="crm-testlog-lines">
                    <li v-for="(line, lidx) in entry.lines" :key="lidx">{{ line }}</li>
                  </ul>
                </li>
              </ul>
            </section>
          </form>
        </div>
      </article>
    </div>
  </div>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.crm-loading { color: var(--text-muted); padding: 1rem; }
.crm-banner-error {
  background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca;
  padding: .5rem .75rem; border-radius: var(--radius); font-size: 13px; margin: 0;
}

.crm-empty {
  display: flex; gap: 1rem; align-items: flex-start;
  padding: 1.25rem; border: 1px dashed var(--border); border-radius: 8px;
  color: var(--text-muted);
}
.crm-empty strong { color: var(--text); display: block; margin-bottom: .25rem; }
.crm-empty p { margin: 0; font-size: 13px; line-height: 1.55; }
.crm-empty code {
  font-family: 'DM Mono', monospace; background: var(--bg);
  padding: .05rem .35rem; border-radius: 4px; font-size: 12px;
}

.crm-grid { display: flex; flex-direction: column; gap: .75rem; }

.crm-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  overflow: hidden;
  transition: box-shadow .18s ease, border-color .18s ease;
}
.crm-card--open { border-color: var(--bp-blue-light); box-shadow: var(--shadow); }

.crm-head {
  display: flex; align-items: center; justify-content: space-between;
  gap: 1rem; padding: 1rem 1.2rem;
  cursor: pointer;
  user-select: none;
}
.crm-head-id { display: flex; align-items: center; gap: .85rem; }
.crm-logo { width: 28px; height: 28px; object-fit: contain; }
.crm-id-text { display: flex; flex-direction: column; gap: .1rem; }
.crm-name { font-size: 14px; font-weight: 700; color: var(--text); margin: 0; letter-spacing: -.01em; }
.crm-id { font-size: 11px; color: var(--text-muted); font-family: 'DM Mono', monospace; }

.crm-head-status { display: flex; align-items: center; gap: .85rem; }
.crm-status {
  display: inline-block;
  padding: .15rem .55rem;
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .07em; border-radius: 999px;
  font-family: 'DM Sans', sans-serif;
}
.crm-status--on    { background: #dcfce7; color: #166534; }
.crm-status--off   { background: #e2e8f0; color: #475569; }
.crm-status--needs { background: #fef3c7; color: #92400e; }

/* Toggle switch */
.crm-toggle { position: relative; display: inline-block; width: 32px; height: 18px; cursor: pointer; }
.crm-toggle input { opacity: 0; width: 0; height: 0; position: absolute; }
.crm-toggle-track {
  position: absolute; inset: 0;
  background: var(--border); border-radius: 999px;
  transition: background .18s;
}
.crm-toggle-track::before {
  content: ''; position: absolute;
  width: 14px; height: 14px; left: 2px; top: 2px;
  background: #fff; border-radius: 50%;
  box-shadow: 0 1px 2px rgba(0,0,0,.2);
  transition: transform .18s;
}
.crm-toggle input:checked + .crm-toggle-track { background: var(--bp-blue); }
.crm-toggle input:checked + .crm-toggle-track::before { transform: translateX(14px); }
.crm-toggle input:disabled + .crm-toggle-track { opacity: .5; cursor: not-allowed; }

.crm-caret { color: var(--text-muted); }

/* ── Body ───────────────────────────────────────────────────────── */
.crm-body {
  padding: 1rem 1.2rem 1.2rem;
  border-top: 1px solid var(--border);
  background: #fafbfc;
}
.crm-loading-inline { color: var(--text-muted); font-size: 13px; }

.crm-form { display: flex; flex-direction: column; gap: .85rem; }
.crm-field { display: flex; flex-direction: column; gap: .35rem; }
.crm-field label {
  font-size: 12px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .05em;
}
.crm-field-opt { font-weight: 400; text-transform: none; letter-spacing: 0; }
.crm-field-help { font-size: 12px; color: var(--text-muted); margin: 0 0 .15rem; line-height: 1.5; }

.crm-secret-set {
  display: flex; align-items: center; gap: .5rem;
  background: #f0fdf4; border: 1px solid #bbf7d0;
  border-radius: var(--radius); padding: .35rem .65rem;
}
.crm-secret-dots {
  font-family: 'DM Mono', monospace; color: #166534;
  letter-spacing: .25em; font-size: 14px;
}
.crm-secret-meta { font-size: 12px; color: #166534; font-weight: 600; }
.crm-secret-clear { color: #b91c1c; }
.crm-secret-clear:hover { background: #fef2f2; border-color: #fecaca; }

.crm-secret-input-row { display: flex; gap: .5rem; align-items: center; }
.crm-secret-input-row input { flex: 1; }

/* PAI-259: eye-toggle button sits inside the input box on the right. */
.crm-secret-input-wrap {
  flex: 1;
  position: relative;
  display: flex;
}
.crm-secret-input-wrap input {
  width: 100%;
  padding-right: 2.2rem;
}
.crm-secret-eye {
  position: absolute;
  right: .35rem;
  top: 50%;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: 0;
  background: transparent;
  color: var(--text-muted);
  border-radius: 6px;
  cursor: pointer;
  transition: background .12s, color .12s;
}
.crm-secret-eye:hover {
  background: var(--surface-2);
  color: var(--text);
}
.crm-secret-eye:focus-visible {
  outline: 2px solid var(--bp-blue);
  outline-offset: 1px;
}

.crm-form-actions { display: flex; justify-content: flex-end; gap: .5rem; }

/* PAI-259: inline log panel for "Test integration" attempts. */
.crm-testlog {
  margin-top: .85rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: #fff;
  overflow: hidden;
}
.crm-testlog-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .5rem;
  padding: .35rem .5rem .35rem .75rem;
  border-bottom: 1px solid var(--border);
  background: #fafbfc;
}
.crm-testlog-title {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .06em;
  color: var(--text-muted);
}
.crm-testlog-list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 240px;
  overflow-y: auto;
}
.crm-testlog-entry {
  padding: .45rem .75rem;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
}
.crm-testlog-entry:last-child { border-bottom: 0; }
.crm-testlog-row {
  display: flex;
  align-items: baseline;
  gap: .55rem;
}
.crm-testlog-time {
  font-family: 'DM Mono', monospace;
  font-size: 11px;
  color: var(--text-muted);
  flex-shrink: 0;
}
.crm-testlog-status {
  font-family: 'DM Mono', monospace;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: .04em;
  padding: .05rem .4rem;
  border-radius: 999px;
  flex-shrink: 0;
}
.crm-testlog-entry--ok   .crm-testlog-status { background: #dcfce7; color: #166534; }
.crm-testlog-entry--fail .crm-testlog-status { background: #fee2e2; color: #b91c1c; }
.crm-testlog-message {
  color: var(--text);
  line-height: 1.45;
  word-break: break-word;
}
.crm-testlog-lines {
  list-style: none;
  margin: .35rem 0 0 0;
  padding: .35rem .55rem;
  background: var(--surface-2);
  border-radius: 6px;
  font-family: 'DM Mono', monospace;
  font-size: 11px;
  color: var(--text-muted);
}
.crm-testlog-lines li {
  padding: .05rem 0;
  word-break: break-all;
}
</style>
