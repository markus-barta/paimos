<!--
  Project Manifest — tabbed editor for the right card of
  ProjectContextSection (P40.854a7db).

  Three tabs share the existing /projects/{id}/manifest endpoint:

    - Manifest   ← user-defined keys in manifest.data, EXCLUDING the
                   two reserved keys below
    - Guardrails ← manifest.data._guardrails  (rules the LLM follows
                   when working on this project)
    - Glossary   ← manifest.data._glossary    (project-specific terms,
                   acronyms, personas)

  Reserved-key convention: `_guardrails` and `_glossary` are off-limits
  for user-authored manifest fields. Save merges all three slices back
  into one document so the existing endpoint stays single-PUT.

  Each tab exposes a "Structure with AI" button that runs the
  /ai/action dispatcher with action keys structure_manifest /
  structure_guardrails / structure_glossary, surfaces the candidate
  JSON via the shared diff overlay (AiSurfaceFeedback), and on Accept
  replaces the tab's draft (no auto-save — user still hits Save).
-->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { errMsg } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import AiSurfaceFeedback from '@/components/ai/AiSurfaceFeedback.vue'
import type { ProjectManifest } from '@/types'
import { loadProjectContext, saveProjectContextManifest } from '@/services/projectContext'
import { useAiOptimize } from '@/composables/useAiOptimize'

const props = defineProps<{
  projectId: number
  canWrite: boolean
}>()

const emit = defineEmits<{
  populated: [v: boolean]
  summary: [payload: { hasManifest: boolean; hasGuardrails: boolean; hasGlossary: boolean; populated: boolean }]
}>()

type TabKey = 'manifest' | 'guardrails' | 'glossary'

interface TabDef {
  key: TabKey
  label: string
  description: string
  icon: string
  reservedKey: '_guardrails' | '_glossary' | null
  aiAction: string
  hostKey: string
  fieldLabel: string
  emptySeed: string
}

const TABS: TabDef[] = [
  {
    key: 'manifest',
    label: 'Manifest',
    description: 'Structured project truth — stack, commands, environments, ADRs, NFRs, ownership.',
    icon: 'box',
    reservedKey: null,
    aiAction: 'structure_manifest',
    hostKey: 'project-context:manifest',
    fieldLabel: 'Manifest',
    emptySeed: '{}',
  },
  {
    key: 'guardrails',
    label: 'Guardrails',
    description: 'Rules the LLM must obey when working on this project.',
    icon: 'shield',
    reservedKey: '_guardrails',
    aiAction: 'structure_guardrails',
    hostKey: 'project-context:guardrails',
    fieldLabel: 'Guardrails',
    emptySeed: '{}',
  },
  {
    key: 'glossary',
    label: 'Glossary',
    description: 'Project-specific terms, acronyms, and personas the LLM should know.',
    icon: 'book-open',
    reservedKey: '_glossary',
    aiAction: 'structure_glossary',
    hostKey: 'project-context:glossary',
    fieldLabel: 'Glossary',
    emptySeed: '{}',
  },
]

const RESERVED_KEYS = TABS.map(t => t.reservedKey).filter((k): k is '_guardrails' | '_glossary' => !!k)

const aiOptimize = useAiOptimize()
const aiAvailable = aiOptimize.available
const aiRunning = aiOptimize.isOptimizing

const activeTab = ref<TabKey>('manifest')
const loading = ref(true)
const manifest = ref<ProjectManifest>({ project_id: 0, data: {} })

// Per-tab drafts so switching tabs doesn't blow away unsaved edits.
const drafts = ref<Record<TabKey, string>>({
  manifest: '{}',
  guardrails: '{}',
  glossary: '{}',
})

const savingTab = ref<TabKey | null>(null)
const tabError = ref<Record<TabKey, string>>({ manifest: '', guardrails: '', glossary: '' })
const tabOk = ref<Record<TabKey, string>>({ manifest: '', guardrails: '', glossary: '' })

function splitManifest(data: Record<string, any>): Record<TabKey, any> {
  const body: Record<string, any> = {}
  for (const [k, v] of Object.entries(data || {})) {
    if (RESERVED_KEYS.includes(k as any)) continue
    body[k] = v
  }
  return {
    manifest: body,
    guardrails: data?._guardrails ?? {},
    glossary: data?._glossary ?? {},
  }
}

function refreshDrafts() {
  const split = splitManifest(manifest.value.data || {})
  drafts.value.manifest = JSON.stringify(split.manifest, null, 2)
  drafts.value.guardrails = JSON.stringify(split.guardrails, null, 2)
  drafts.value.glossary = JSON.stringify(split.glossary, null, 2)
}

function isNonEmptyObject(v: unknown): boolean {
  return !!v && typeof v === 'object' && Object.keys(v as object).length > 0
}

const hasManifestBody = computed(() => {
  const split = splitManifest(manifest.value.data || {})
  return isNonEmptyObject(split.manifest)
})
const hasGuardrails = computed(() => isNonEmptyObject((manifest.value.data || {})._guardrails))
const hasGlossary = computed(() => isNonEmptyObject((manifest.value.data || {})._glossary))
const populated = computed(() => hasManifestBody.value || hasGuardrails.value || hasGlossary.value)

watch(populated, (v) => emit('populated', v), { immediate: true })
watch([hasManifestBody, hasGuardrails, hasGlossary, populated], () => {
  emit('summary', {
    hasManifest: hasManifestBody.value || hasGuardrails.value || hasGlossary.value,
    hasGuardrails: hasGuardrails.value,
    hasGlossary: hasGlossary.value,
    populated: populated.value,
  })
}, { immediate: true })

const tabFilledMap = computed<Record<TabKey, boolean>>(() => ({
  manifest: hasManifestBody.value,
  guardrails: hasGuardrails.value,
  glossary: hasGlossary.value,
}))

const activeDef = computed(() => TABS.find(t => t.key === activeTab.value)!)

async function load() {
  loading.value = true
  try {
    const data = await loadProjectContext(props.projectId)
    manifest.value = data.manifest
    refreshDrafts()
  } catch (e) {
    tabError.value[activeTab.value] = errMsg(e, 'Failed to load project context.')
  } finally {
    loading.value = false
  }
}

function clearStatus(tab: TabKey) {
  tabError.value[tab] = ''
  tabOk.value[tab] = ''
}

async function saveTab(tab: TabKey) {
  const def = TABS.find(t => t.key === tab)!
  clearStatus(tab)
  savingTab.value = tab
  try {
    const draftParsed = JSON.parse(drafts.value[tab] || def.emptySeed)
    const current = manifest.value.data || {}
    let merged: Record<string, any>
    if (def.reservedKey) {
      merged = { ...current, [def.reservedKey]: draftParsed }
    } else {
      // Manifest body tab — replace user-keys, preserve reserved keys.
      merged = { ...draftParsed }
      for (const rk of RESERVED_KEYS) {
        if (current[rk] !== undefined) merged[rk] = current[rk]
      }
    }
    manifest.value = await saveProjectContextManifest(props.projectId, merged)
    refreshDrafts()
    tabOk.value[tab] = `${def.label} saved.`
    setTimeout(() => { if (tabOk.value[tab]) tabOk.value[tab] = '' }, 2500)
  } catch (e) {
    tabError.value[tab] = errMsg(e, `Failed to save ${def.label.toLowerCase()}.`)
  } finally {
    savingTab.value = null
  }
}

function structureWithAi(tab: TabKey) {
  const def = TABS.find(t => t.key === tab)!
  clearStatus(tab)
  if (!aiAvailable.value || aiRunning.value) return
  const sourceText = drafts.value[tab] ?? ''
  void aiOptimize.runRewriteAction({
    hostKey: def.hostKey,
    surface: 'issue',
    action: def.aiAction,
    field: `${tab}_json`,
    fieldLabel: def.fieldLabel,
    text: sourceText,
    onAccept: (next) => {
      drafts.value[tab] = formatJsonCandidate(next)
    },
  })
}

// AI may return a JSON string or already-stringified JSON; pretty-print
// when we can, otherwise fall back to the raw response so the user
// always sees the candidate.
function formatJsonCandidate(text: string): string {
  const trimmed = (text || '').trim()
  if (!trimmed) return '{}'
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2)
  } catch {
    return trimmed
  }
}

const aiTooltip = computed(() => {
  if (!aiAvailable.value) return 'AI is not configured. Ask an admin to enable it in Settings → AI.'
  if (aiRunning.value) return 'An AI action is already running.'
  return 'Convert pasted prose into a structured JSON candidate.'
})

onMounted(load)
</script>

<template>
  <section class="manifest-tabs" :data-active-tab="activeTab">
    <header class="mt-head">
      <nav class="mt-tabs" role="tablist" aria-label="Project context editors">
        <button
          v-for="t in TABS"
          :key="t.key"
          role="tab"
          type="button"
          :aria-selected="activeTab === t.key"
          :class="['mt-tab', { active: activeTab === t.key, filled: tabFilledMap[t.key] }]"
          @click="activeTab = t.key"
        >
          <AppIcon :name="t.icon" :size="13" />
          <span class="mt-tab-label">{{ t.label }}</span>
          <span v-if="tabFilledMap[t.key]" class="mt-tab-dot" aria-hidden="true"></span>
        </button>
      </nav>
    </header>

    <p class="mt-desc">{{ activeDef.description }}</p>

    <AiSurfaceFeedback :host-key="activeDef.hostKey" />

    <div v-if="loading" class="mt-empty">Loading…</div>

    <template v-else>
      <div v-if="tabError[activeTab]" class="mt-error">{{ tabError[activeTab] }}</div>
      <div v-if="tabOk[activeTab]" class="mt-ok">{{ tabOk[activeTab] }}</div>

      <div v-if="canWrite" class="mt-editor-wrap">
        <textarea
          v-model="drafts[activeTab]"
          class="mt-editor"
          spellcheck="false"
          :placeholder="`Paste prose or JSON. Click ✨ Structure with AI to shape it into ${activeDef.label.toLowerCase()} JSON.`"
        ></textarea>
        <div class="mt-actions">
          <button
            type="button"
            class="btn-ai"
            :disabled="!aiAvailable || aiRunning"
            :title="aiTooltip"
            @click="structureWithAi(activeTab)"
          >
            <AppIcon :name="aiRunning ? 'loader-circle' : 'sparkles'" :size="13" :class="{ spin: aiRunning }" />
            <span>{{ aiRunning ? 'Structuring…' : 'Structure with AI' }}</span>
          </button>
          <button
            type="button"
            class="btn-save"
            :disabled="savingTab === activeTab"
            @click="saveTab(activeTab)"
          >
            {{ savingTab === activeTab ? 'Saving…' : `Save ${activeDef.label.toLowerCase()}` }}
          </button>
        </div>
      </div>

      <pre v-else-if="tabFilledMap[activeTab]" class="mt-read">{{ drafts[activeTab] }}</pre>
      <div v-else class="mt-empty">No {{ activeDef.label.toLowerCase() }} saved yet.</div>
    </template>
  </section>
</template>

<style scoped>
.manifest-tabs { display: flex; flex-direction: column; gap: .75rem; }

.mt-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 1rem; }
.mt-tabs {
  display: inline-flex;
  gap: 0;
  border-bottom: 2px solid var(--border);
  margin-bottom: -1px;
  align-self: stretch;
  width: 100%;
}
.mt-tab {
  display: inline-flex;
  align-items: center;
  gap: .4rem;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  margin-bottom: -2px;
  padding: .5rem .9rem;
  font: inherit;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted);
  cursor: pointer;
  transition: color .15s ease, border-color .15s ease, background-color .15s ease;
  border-radius: 6px 6px 0 0;
}
.mt-tab:hover { color: var(--text); background: var(--bg); }
.mt-tab.active {
  color: var(--bp-blue-dark);
  border-bottom-color: var(--bp-blue);
  font-weight: 600;
}
.mt-tab .mt-tab-label { letter-spacing: .01em; }
.mt-tab-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--bp-green, #16a34a);
  display: inline-block;
  box-shadow: 0 0 0 2px var(--bg-card);
}
.mt-tab.active .mt-tab-dot { background: var(--bp-blue); }
.mt-tab.filled:not(.active) { color: var(--text); }

.mt-desc { margin: 0; color: var(--text-muted); font-size: 12px; line-height: 1.45; }

.mt-error { color: #b42318; background: #fef3f2; border: 1px solid #fecdca; border-radius: 8px; padding: .55rem .75rem; font-size: 13px; }
.mt-ok    { color: #166534; background: #ecfdf3; border: 1px solid #abefc6; border-radius: 8px; padding: .55rem .75rem; font-size: 13px; }
.mt-empty { color: var(--text-muted); font-size: 13px; padding: .25rem 0; }

.mt-editor-wrap { display: flex; flex-direction: column; gap: .55rem; }

.mt-editor {
  width: 100%;
  min-height: 320px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg);
  color: var(--text);
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 12px;
  line-height: 1.55;
  padding: .75rem .85rem;
  resize: vertical;
  transition: border-color .15s ease, box-shadow .15s ease;
}
.mt-editor:focus {
  outline: none;
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--bp-blue) 18%, transparent);
}
.mt-editor::placeholder { color: var(--text-muted); opacity: .85; font-style: italic; }

.mt-read {
  margin: 0;
  padding: .85rem .95rem;
  border-radius: 10px;
  background: var(--bg);
  border: 1px solid var(--border);
  overflow: auto;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 12px;
  line-height: 1.5;
  color: var(--text);
}

.mt-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: .55rem;
  flex-wrap: wrap;
}

.btn-ai {
  display: inline-flex;
  align-items: center;
  gap: .4rem;
  padding: .45rem .85rem;
  font: inherit;
  font-size: 13px;
  font-weight: 500;
  color: var(--bp-blue-dark);
  background: linear-gradient(180deg,
    color-mix(in srgb, var(--bp-blue-pale, #dce9f4) 65%, #fff) 0%,
    color-mix(in srgb, var(--bp-blue-pale, #dce9f4) 90%, #fff) 100%);
  border: 1px solid color-mix(in srgb, var(--bp-blue) 35%, var(--border));
  border-radius: 999px;
  cursor: pointer;
  transition: background .15s ease, border-color .15s ease, transform .12s ease, box-shadow .15s ease;
  position: relative;
}
.btn-ai:hover:not(:disabled) {
  background: linear-gradient(180deg,
    color-mix(in srgb, var(--bp-blue-pale, #dce9f4) 50%, #fff) 0%,
    color-mix(in srgb, var(--bp-blue-pale, #dce9f4) 80%, #fff) 100%);
  border-color: var(--bp-blue);
  box-shadow: 0 1px 0 color-mix(in srgb, var(--bp-blue) 18%, transparent),
              0 4px 14px -6px color-mix(in srgb, var(--bp-blue) 35%, transparent);
}
.btn-ai:active:not(:disabled) { transform: translateY(1px); }
.btn-ai:disabled {
  opacity: .55;
  cursor: not-allowed;
  filter: grayscale(.4);
}
.btn-ai .spin { animation: mt-spin 1s linear infinite; }
@keyframes mt-spin { to { transform: rotate(360deg); } }

.btn-save {
  padding: .45rem 1rem;
  font: inherit;
  font-size: 13px;
  font-weight: 600;
  color: #fff;
  background: var(--bp-green, #16a34a);
  border: 1px solid color-mix(in srgb, var(--bp-green, #16a34a) 75%, #000);
  border-radius: 999px;
  cursor: pointer;
  transition: background .15s ease, transform .12s ease, box-shadow .15s ease;
  box-shadow: 0 1px 0 rgba(0,0,0,.04);
}
.btn-save:hover:not(:disabled) {
  background: color-mix(in srgb, var(--bp-green, #16a34a) 85%, #000);
  box-shadow: 0 2px 0 rgba(0,0,0,.06), 0 6px 16px -8px color-mix(in srgb, var(--bp-green, #16a34a) 60%, transparent);
}
.btn-save:active:not(:disabled) { transform: translateY(1px); }
.btn-save:disabled { opacity: .6; cursor: not-allowed; }
</style>
