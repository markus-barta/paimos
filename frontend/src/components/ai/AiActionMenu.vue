<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-163. Multi-action AI button — the dropdown shell that replaces
 the single-purpose AiOptimizeButton (PAI-147).

 Layout
 ------
   [ AI ✨ ][ ⌄ ]
     │       │
     │       └── chevron: opens menu popover
     └────────── chip body: runs the default action (Optimize wording)

 The chip stays visually identical to the v1 button so the surface
 doesn't change cost for authors who only ever optimize. The chevron
 surfaces the rest only when needed.

 Surfaces filter the catalog
 ---------------------------
 The component takes a `surface` prop ("issue" | "customer"). The
 menu only shows actions whose backend `surface` matches — same
 author, same screen, different action set.

 Implemented vs stubbed
 ---------------------
 Backend marks each action `implemented: true|false`. Stubbed
 actions still render in the menu (so authors see what's coming)
 but are disabled with a tooltip pointing at the tracking ticket.
-->
<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import {
  useAiAction,
  type AiActionOptions,
  type AiExecutionOptionsScope,
  type AiPromptPresetChoice,
} from '@/composables/useAiAction'

const props = defineProps<{
  /** Field identifier (description, customer_notes, …). May be ""
   * for actions that don't operate on a single field. */
  field: string
  /** Pretty label for the diff overlay header. Defaults to `field`. */
  fieldLabel?: string
  /** Issue id for context assembly. 0 = no context (new-issue forms). */
  issueId?: number
  /** Surface ("issue" | "customer"). Filters the menu. */
  surface?: 'issue' | 'customer'
  /** PAI-179: placement ("text" | "issue"). Defaults to "text" so
   *  text-field hosts (textareas) get text-level actions only.
   *  Issue-level menu hosts (sidebar header, ellipsis, edit-mode
   *  toolbar) pass placement="issue" to surface the actions that
   *  operate on the whole record. */
  placement?: 'text' | 'issue'
  /** Current field content. Read at click time, not at mount time. */
  text: () => string
  /** Stable host identifier so AI feedback renders on the surface
   *  that initiated the action instead of a different editor copy. */
  hostKey?: string
  /** Called with optimized text when the user clicks Accept. Used by
   *  actions whose result is "rewritten field text" (optimize, tone-check,
   *  translate). */
  onAccept: (text: string) => void
  /** Optional: extra context the menu doesn't need but actions might,
   *  e.g. project_id. Passed through verbatim to the dispatcher. */
  context?: Record<string, unknown>
  /** Optional override of the disabled-state tooltip. */
  disabledTooltip?: string
}>()

const aiAction = useAiAction()

// Local menu state — kept here, not in the composable, because two
// menus on the same page may be open one at a time but the keyboard
// focus and submenu state are per-instance.
const menuOpen = ref(false)
const menuRoot = ref<HTMLElement | null>(null)
const submenuKey = ref<string | null>(null)
const selectedProfileId = ref('')
const selectedEffort = ref('')
const selectedPromptPresetRef = ref('')
const selectedContextPackId = ref('')

const surface = computed(() => props.surface ?? 'issue')
const placement = computed(() => props.placement ?? 'text')

// PAI-179: filter by surface AND placement. An action with
// placement="both" shows up in both text fields and issue-level
// menus; "text"/"issue" pin it to one or the other.
const visibleActions = computed(() => {
  const all = aiAction.actions.value
  return all.filter(a => {
    if (a.surface !== surface.value) return false
    if (placement.value === 'text' && a.placement === 'issue') return false
    if (placement.value === 'issue' && a.placement === 'text') return false
    return true
  })
})

// PAI-179: if the catalogue came up empty (typical when the very
// first /api/ai/actions call landed before login), nudge a retry
// when the menu mounts. Cheap — at most one round-trip per mount
// when the catalog is genuinely empty.
onMounted(() => {
  if (aiAction.actions.value.length === 0) {
    void aiAction.refreshActions()
  }
})

const defaultAction = computed(() => visibleActions.value.find(a => a.key === 'optimize'))
const executionProfiles = computed(() => aiAction.executionOptions.value?.profiles ?? [])
const executionEfforts = computed(() => aiAction.executionOptions.value?.efforts ?? [])
const executionContextPacks = computed(() => aiAction.executionOptions.value?.context_packs ?? [])
const visibleActionKeys = computed(() => new Set(visibleActions.value.map(a => a.key)))
const executionPromptPresets = computed(() => {
  const presets = aiAction.executionOptions.value?.prompt_presets ?? []
  return presets.filter(p => p.status === 'active' && promptPresetAppliesToAny(p, visibleActionKeys.value))
})
const showProfileControl = computed(() => executionProfiles.value.length > 1)
const showEffortControl = computed(() => executionEfforts.value.length > 1)
const showPromptPresetControl = computed(() => executionPromptPresets.value.length > 0)
const showContextControl = computed(() => executionContextPacks.value.length > 1)
const showExecutionControls = computed(() => showProfileControl.value || showEffortControl.value || showPromptPresetControl.value || showContextControl.value)
const selectedProfile = computed(() =>
  executionProfiles.value.find(p => p.id === selectedProfileId.value) ?? null,
)
const selectedPromptPreset = computed(() =>
  executionPromptPresets.value.find(p => p.ref === selectedPromptPresetRef.value) ?? null,
)
const selectedContextPack = computed(() =>
  executionContextPacks.value.find(p => p.id === selectedContextPackId.value) ?? null,
)
const executionMetaLine = computed(() => {
  const parts: string[] = []
  if (selectedProfile.value) {
    parts.push(`${selectedProfile.value.model} · ${selectedProfile.value.speed_label} · ${selectedProfile.value.cost_label}`)
  }
  if (selectedPromptPreset.value) {
    parts.push(`Prompt ${selectedPromptPreset.value.ref}@${selectedPromptPreset.value.revision}`)
  }
  if (selectedContextPack.value) {
    parts.push(`Context ${selectedContextPack.value.label}`)
  }
  return parts.join(' · ')
})

const disabled = computed(() =>
  !aiAction.available.value || aiAction.isRunning.value)
const emptyStateMessage = computed(() => {
  if (aiAction.actionsStatus.value === 'loading') {
    return 'Loading AI actions…'
  }
  if (aiAction.actionsStatus.value === 'error') {
    return aiAction.actionsLoadError.value ?? 'AI action catalog unavailable right now.'
  }
  return 'No AI actions are assigned to this location yet.'
})
const tooltip = computed(() => {
  if (!aiAction.available.value) {
    return props.disabledTooltip
      ?? 'AI is not configured. An admin can enable it under Settings → AI.'
  }
  if (aiAction.isRunning.value) return 'Action in progress…'
  return 'Optimize wording — click chevron for more actions'
})

// ── handlers ─────────────────────────────────────────────────────

function runDefault() {
  if (disabled.value) return
  // PAI-179: in issue-placement mode, no single "default" action
  // makes sense (find_parent and generate_subtasks aren't a sane
  // one-click affordance). Open the menu instead — the chip body
  // and the chevron behave identically.
  if (placement.value === 'issue' || !defaultAction.value) {
    openMenu()
    return
  }
  const text = props.text()
  if (!text.trim() && defaultAction.value.key === 'optimize') return
  void invoke(defaultAction.value.key, undefined)
}

async function invoke(actionKey: string, subAction?: string) {
  closeMenu()
  await aiAction.run({
    hostKey: props.hostKey,
    surface: surface.value,
    action: actionKey,
    subAction,
    field: props.field,
    fieldLabel: props.fieldLabel ?? props.field,
    text: props.text(),
    issueId: props.issueId,
    onAccept: props.onAccept,
    context: props.context,
    options: selectedActionOptions(),
  })
}

function toggleMenu() {
  if (menuOpen.value) {
    closeMenu()
  } else {
    openMenu()
  }
}

function openMenu() {
  menuOpen.value = true
  submenuKey.value = null
  void aiAction.refreshExecutionOptions(executionScope())
  nextTick(() => {
    const first = menuRoot.value?.querySelector<HTMLElement>('.ai-menu-item:not(:disabled)')
    first?.focus()
  })
}

function closeMenu() {
  menuOpen.value = false
  submenuKey.value = null
}

function onItemKeydown(e: KeyboardEvent, action: { key: string, sub_keys?: string[] }) {
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    if (action.sub_keys?.length) {
      submenuKey.value = action.key
      nextTick(() => {
        const first = menuRoot.value?.querySelector<HTMLElement>('.ai-submenu-item')
        first?.focus()
      })
    } else {
      void invoke(action.key)
    }
  } else if (e.key === 'ArrowRight' && action.sub_keys?.length) {
    submenuKey.value = action.key
  } else if (e.key === 'ArrowLeft') {
    submenuKey.value = null
  } else if (e.key === 'Escape') {
    closeMenu()
  }
}

// Outside-click dismissal
function onDocClick(e: MouseEvent) {
  if (!menuOpen.value) return
  const t = e.target as Node | null
  if (t && menuRoot.value && !menuRoot.value.contains(t)) {
    closeMenu()
  }
}
onMounted(() => document.addEventListener('mousedown', onDocClick))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocClick))
watch(menuOpen, (o) => {
  if (!o) submenuKey.value = null
})
watch(executionPromptPresets, (presets) => {
  if (selectedPromptPresetRef.value && !presets.some(p => p.ref === selectedPromptPresetRef.value)) {
    selectedPromptPresetRef.value = ''
  }
})
watch(executionContextPacks, (packs) => {
  if (selectedContextPackId.value && !packs.some(p => p.id === selectedContextPackId.value)) {
    selectedContextPackId.value = ''
  }
})

function subActionLabel(parent: string, sub: string): string {
  if (parent === 'suggest_enhancement') {
    return ({
      security:    'Security',
      performance: 'Performance',
      ux:          'UX',
      dx:          'DX (developer experience)',
      flow:        'Flow / state',
      risks:       'Risks & dependencies',
    } as Record<string, string>)[sub] ?? sub
  }
  if (parent === 'translate') {
    return ({
      de_en: 'German → English',
      en_de: 'English → German',
    } as Record<string, string>)[sub] ?? sub
  }
  return sub
}

function actionTooltip(a: { key: string; implemented: boolean }): string {
  if (!a.implemented) {
    return 'Coming soon — the menu shell is here, the action handler ships in a follow-up ticket.'
  }
  if (!selectedPromptAllowsAction(a.key)) {
    return 'Selected prompt preset does not apply to this action.'
  }
  return ''
}

function selectedActionOptions(): AiActionOptions | undefined {
  const opts: AiActionOptions = {}
  if (selectedProfileId.value) opts.profile_id = selectedProfileId.value
  if (selectedEffort.value) opts.effort = selectedEffort.value
  if (selectedPromptPresetRef.value) opts.prompt_preset_ref = selectedPromptPresetRef.value
  if (selectedContextPackId.value) opts.context_pack = selectedContextPackId.value
  return Object.keys(opts).length ? opts : undefined
}

function executionScope(): AiExecutionOptionsScope | undefined {
  const issueId = props.issueId ?? 0
  if (issueId > 0) return { issueId }
  const projectId = numberFromUnknown(props.context?.project_id)
  if (projectId > 0) return { projectId }
  return undefined
}

function numberFromUnknown(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return 0
}

function selectedPromptAllowsAction(actionKey: string): boolean {
  if (!selectedPromptPreset.value) return true
  return promptPresetAppliesToAction(selectedPromptPreset.value, actionKey)
}

function promptPresetAppliesToAny(preset: AiPromptPresetChoice, actionKeys: Set<string>): boolean {
  const actions = preset.actions ?? []
  if (actions.includes('*')) return true
  return actions.some(action => actionKeys.has(action))
}

function promptPresetAppliesToAction(preset: AiPromptPresetChoice, actionKey: string): boolean {
  const actions = preset.actions ?? []
  return actions.includes('*') || actions.includes(actionKey)
}

function promptActionsLabel(preset: AiPromptPresetChoice): string {
  const actions = preset.actions ?? []
  if (actions.includes('*')) return 'all actions'
  return actions.map(a => actionLabel(a)).join(', ')
}

function actionLabel(key: string): string {
  return visibleActions.value.find(a => a.key === key)?.label ?? key.replace(/[_-]+/g, ' ')
}

function actionDefaultMeta(a: { key: string; default_profile_id?: string; default_effort?: string }): string {
  const selectorDefault = aiAction.executionOptions.value?.selector_defaults?.actions?.[a.key]
  const parts: string[] = []
  const profile = selectorDefault?.profile_id || a.default_profile_id
  const effort = selectorDefault?.effort || a.default_effort
  if (profile) parts.push(profileLabel(profile))
  if (effort) parts.push(effortLabel(effort))
  return parts.join(' · ')
}

function profileLabel(id: string): string {
  return executionProfiles.value.find(p => p.id === id)?.label ?? id
}

function effortLabel(effort: string): string {
  return effort.replace(/[_-]+/g, ' ').replace(/\b\w/g, c => c.toUpperCase())
}
</script>

<template>
  <div class="ai-menu-root" ref="menuRoot">
    <div class="ai-menu-chip" :class="{ 'ai-menu-chip--busy': aiAction.isRunning.value, 'ai-menu-chip--disabled': disabled }">
      <button
        type="button"
        class="ai-menu-chip-body"
        :disabled="disabled"
        :title="tooltip"
        :aria-label="tooltip"
        @click="runDefault"
      >
        <AppIcon :name="aiAction.isRunning.value ? 'loader-circle' : 'sparkles'" :size="12" :class="{ spin: aiAction.isRunning.value }" />
        <span class="ai-menu-chip-label">AI</span>
      </button>
      <button
        type="button"
        class="ai-menu-chip-chev"
        :disabled="!aiAction.available.value"
        :aria-haspopup="true"
        :aria-expanded="menuOpen"
        title="More AI actions"
        @click="toggleMenu"
      >
        <AppIcon name="chevron-down" :size="11" />
      </button>
    </div>

    <transition name="ai-menu-pop">
      <div
        v-if="menuOpen"
        class="ai-menu-popover"
        role="menu"
        @keydown.esc="closeMenu"
      >
        <div class="ai-menu-list">
          <template v-for="a in visibleActions" :key="a.key">
            <button
              type="button"
              class="ai-menu-item"
              :class="{ 'ai-menu-item--disabled': !a.implemented || !selectedPromptAllowsAction(a.key), 'ai-menu-item--has-sub': (a.sub_keys?.length ?? 0) > 0, 'ai-menu-item--active': submenuKey === a.key }"
              :disabled="!a.implemented || !selectedPromptAllowsAction(a.key)"
              :title="actionTooltip(a)"
              role="menuitem"
              @click="!a.sub_keys?.length ? invoke(a.key) : (submenuKey = submenuKey === a.key ? null : a.key)"
              @keydown="(e: KeyboardEvent) => onItemKeydown(e, a)"
              @mouseenter="a.sub_keys?.length && (submenuKey = a.key)"
            >
              <AppIcon :name="iconFor(a.key)" :size="12" />
              <span class="ai-menu-item-main">
                <span class="ai-menu-item-label">{{ a.label }}</span>
                <span v-if="actionDefaultMeta(a)" class="ai-menu-item-default">{{ actionDefaultMeta(a) }}</span>
              </span>
              <span v-if="!a.implemented" class="ai-menu-item-soon">soon</span>
              <AppIcon v-if="a.sub_keys?.length" name="chevron-right" :size="11" class="ai-menu-item-chev" />
            </button>

            <!-- Inline submenu — drops below its parent on click. We
                 prefer inline expansion over a flyout because PAIMOS
                 editors live inside narrow panels where right-anchored
                 submenus regularly clip off-screen. -->
            <div
              v-if="a.sub_keys?.length && submenuKey === a.key"
              class="ai-menu-submenu"
              role="menu"
            >
              <button
                v-for="sub in a.sub_keys" :key="sub"
                type="button"
                class="ai-submenu-item"
                role="menuitem"
                @click="invoke(a.key, sub)"
              >
                {{ subActionLabel(a.key, sub) }}
              </button>
            </div>
          </template>
          <div v-if="!visibleActions.length" class="ai-menu-empty">
            {{ emptyStateMessage }}
          </div>
        </div>
        <div
          v-if="showExecutionControls"
          class="ai-menu-execution"
          :class="{ 'ai-menu-execution--single': showProfileControl !== showEffortControl }"
          @click.stop
          @mousedown.stop
          @keydown.stop
        >
          <label v-if="showProfileControl" class="ai-menu-execution-field">
            <span>Profile</span>
            <select v-model="selectedProfileId" aria-label="AI profile">
              <option value="">Recommended</option>
              <option v-for="profile in executionProfiles" :key="profile.id" :value="profile.id">
                {{ profile.label }}
              </option>
            </select>
          </label>
          <label v-if="showEffortControl" class="ai-menu-execution-field">
            <span>Effort</span>
            <select v-model="selectedEffort" aria-label="AI effort">
              <option value="">Recommended</option>
              <option v-for="effort in executionEfforts" :key="effort" :value="effort">
                {{ effortLabel(effort) }}
              </option>
            </select>
          </label>
          <label v-if="showPromptPresetControl" class="ai-menu-execution-field">
            <span>Prompt</span>
            <select v-model="selectedPromptPresetRef" aria-label="AI prompt preset">
              <option value="">Default</option>
              <option v-for="preset in executionPromptPresets" :key="preset.ref" :value="preset.ref">
                {{ preset.label }} — {{ promptActionsLabel(preset) }}
              </option>
            </select>
          </label>
          <label v-if="showContextControl" class="ai-menu-execution-field">
            <span>Context</span>
            <select v-model="selectedContextPackId" aria-label="AI context pack">
              <option value="">Issue only</option>
              <option v-for="pack in executionContextPacks.filter(p => p.id !== 'issue')" :key="pack.id" :value="pack.id">
                {{ pack.label }}
              </option>
            </select>
          </label>
          <div v-if="executionMetaLine" class="ai-menu-execution-meta">
            {{ executionMetaLine }}
          </div>
        </div>
      </div>
    </transition>
  </div>
</template>

<script lang="ts">
// Icon mapping for menu items. Kept outside <script setup> so it can
// be a pure const lookup without re-allocating per render.
const ICONS: Record<string, string> = {
  optimize:            'sparkles',
  suggest_enhancement: 'lightbulb',
  spec_out:            'list-checks',
  find_parent:         'git-branch',
  translate:           'languages',
  generate_subtasks:   'list-tree',
  estimate_effort:     'gauge',
  detect_duplicates:   'copy',
  ui_generation:       'monitor',
  tone_check:          'message-square',
}
function iconFor(key: string): string {
  return ICONS[key] ?? 'sparkles'
}
export { iconFor }
</script>

<style scoped>
.ai-menu-root { position: relative; display: inline-block; }

.ai-menu-chip {
  display: inline-flex; align-items: stretch;
  border: 1px solid transparent;
  border-radius: 999px;
  background: transparent;
  font-family: 'DM Sans', sans-serif;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: .04em;
  color: var(--text-muted);
  transition: background .12s, color .12s, border-color .12s;
}
.ai-menu-chip:hover:not(.ai-menu-chip--disabled) {
  background: var(--bp-blue-pale, #dce9f4);
  color: var(--bp-blue-dark, #1f4d75);
  border-color: var(--bp-blue-light, #4a8fc2);
}
.ai-menu-chip--busy {
  color: var(--bp-blue-dark, #1f4d75);
  background: var(--bp-blue-pale, #dce9f4);
  border-color: var(--bp-blue-light, #4a8fc2);
}
.ai-menu-chip--disabled { opacity: .55; }

.ai-menu-chip-body, .ai-menu-chip-chev {
  background: none;
  border: none;
  padding: .15rem .45rem;
  display: inline-flex; align-items: center; gap: .25rem;
  cursor: pointer;
  font-family: inherit;
  font-size: inherit;
  color: inherit;
  letter-spacing: inherit;
}
.ai-menu-chip-body:disabled, .ai-menu-chip-chev:disabled {
  cursor: not-allowed;
}
.ai-menu-chip-chev {
  border-left: 1px solid currentColor;
  border-left-color: rgba(0,0,0,.06);
  padding: .15rem .35rem .15rem .3rem;
}
.ai-menu-chip:hover .ai-menu-chip-chev,
.ai-menu-chip--busy .ai-menu-chip-chev {
  border-left-color: rgba(46, 109, 164, .25);
}

.spin { animation: ai-action-spin 1s linear infinite; }
@keyframes ai-action-spin {
  from { transform: rotate(0); }
  to   { transform: rotate(360deg); }
}

.ai-menu-popover {
  position: absolute; top: calc(100% + 6px); right: 0;
  z-index: 200;
  background: var(--bg-card, white);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 10px 30px rgba(0,0,0,.10), 0 4px 8px rgba(0,0,0,.04);
  min-width: 260px;
  padding: .35rem;
  overflow: hidden;
}
.ai-menu-list { display: flex; flex-direction: column; gap: 1px; }
.ai-menu-item {
  display: flex; align-items: center; gap: .55rem;
  padding: .45rem .65rem;
  background: none; border: none;
  font-family: 'DM Sans', sans-serif;
  font-size: 12.5px;
  color: var(--text);
  text-align: left;
  cursor: pointer;
  border-radius: 7px;
  transition: background .1s, color .1s;
}
.ai-menu-item:hover, .ai-menu-item:focus {
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
  outline: none;
}
.ai-menu-item--disabled {
  cursor: not-allowed;
  opacity: .55;
}
.ai-menu-item--disabled:hover { background: transparent; color: var(--text); }
.ai-menu-item--active { background: var(--bp-blue-pale); color: var(--bp-blue-dark); }
.ai-menu-item-main {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: .05rem;
}
.ai-menu-item-label { flex: 1; }
.ai-menu-item-default {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 10px;
  color: var(--text-muted);
  line-height: 1.2;
}
.ai-menu-item-soon {
  font-size: 9.5px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
  background: #fef3c7;
  color: #92400e;
  padding: .1rem .4rem;
  border-radius: 999px;
}
.ai-menu-item-chev { color: var(--text-muted); }

.ai-menu-submenu {
  display: flex; flex-direction: column; gap: 1px;
  margin-left: 1rem;
  padding: .15rem 0;
  border-left: 2px solid var(--border);
  padding-left: .4rem;
}
.ai-submenu-item {
  display: block;
  background: none; border: none;
  font-family: inherit;
  font-size: 12px;
  color: var(--text);
  text-align: left;
  padding: .35rem .55rem;
  border-radius: 6px;
  cursor: pointer;
}
.ai-submenu-item:hover, .ai-submenu-item:focus {
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
  outline: none;
}

.ai-menu-empty {
  padding: .5rem .75rem;
  font-size: 12px;
  color: var(--text-muted);
}
.ai-menu-execution {
  margin-top: .35rem;
  padding: .45rem .5rem .5rem;
  border-top: 1px solid var(--border);
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(116px, 1fr));
  gap: .45rem;
}
.ai-menu-execution-field {
  display: flex;
  flex-direction: column;
  gap: .18rem;
  min-width: 0;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: .04em;
  text-transform: uppercase;
  color: var(--text-muted);
}
.ai-menu-execution-field select {
  width: 100%;
  min-height: 30px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--bg-card, white);
  color: var(--text);
  font: 500 12px "DM Sans", sans-serif;
  letter-spacing: 0;
  text-transform: none;
  padding: .25rem .45rem;
}
.ai-menu-execution-meta {
  grid-column: 1 / -1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 10px;
  color: var(--text-muted);
}

.ai-menu-pop-enter-active, .ai-menu-pop-leave-active { transition: opacity .12s, transform .12s; }
.ai-menu-pop-enter-from, .ai-menu-pop-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
