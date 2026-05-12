<script setup lang="ts">
// PAI-339 — inline editor for a single knowledge entry. Driven by the
// canonical KnowledgeEntryInput shape so it works for create + update
// without the parent component branching. Per-category metadata is
// surfaced via a small set of well-known fields (URL for external
// systems, type/scope/confidence/applies_to_environments for memory,
// instance_url for related projects, related_agents for runbooks) —
// the column itself is schemaless on the wire so PAI-344's content
// migrations and PAI-349's bot-authored drafts can iterate without
// coordinating with this file.

import { computed, ref, watch } from 'vue'
import type { KnowledgeCategory, KnowledgeEntryInput, IssueRelation } from '@/types'
import { useMarkdown } from '@/composables/useMarkdown'
import {
  archivedStatusValue,
  activeStatusValue,
  suggestSlug,
  validateKnowledgeSlug,
  promoteMemory,
  type MemoryScope,
} from '@/services/projectKnowledge'
import { loadIssueRelations, removeIssueRelation } from '@/services/issueRelations'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'

const props = defineProps<{
  category: KnowledgeCategory
  // null = create new; non-null = edit existing entry. The original
  // slug is held in `currentSlug` so the parent can route rename PUTs
  // (URL slug vs body slug) per the backend handler in handlers.go.
  initial: KnowledgeEntryInput
  currentSlug: string | null
  saving: boolean
  saveError: string
  // Slug suggestion is opt-in — we only auto-fill while the user
  // hasn't manually edited the slug. Existing-entry edits leave the
  // slug alone so renames are explicit, never accidental.
  autosuggestSlug: boolean
  // PAI-342 — when editing an existing memory entry, the parent passes
  // the underlying issue id so this editor can render the live
  // "Originating Tickets" section (issues linked via the
  // applies_to_memory relation, reverse direction). Optional —
  // create-mode + non-memory categories pass undefined.
  entryId?: number
  projectId?: number
}>()

const emit = defineEmits<{
  save: [payload: KnowledgeEntryInput]
  cancel: []
  // PAI-345: emitted after a successful promote so the parent can
  // refresh the list / dismiss the editor (the source row is
  // soft-deleted, so staying on this view shows a stale entry).
  promoted: [scope: MemoryScope]
}>()

const slug = ref(props.initial.slug ?? '')
const title = ref(props.initial.title ?? '')
const body = ref(props.initial.body ?? '')
const status = ref(props.initial.status ?? activeStatusValue())
// Metadata holds per-category tail fields. We surface specific keys
// in the UI but copy the full record so unknown fields round-trip
// without loss — important for migrations / bot-authored drafts that
// may stash provenance fields the editor doesn't know about.
const metadata = ref<Record<string, unknown>>({ ...(props.initial.metadata ?? {}) })

// Derived fields per category (only the well-known keys; everything
// else is preserved verbatim through the metadata.value spread).
const memoryType = ref(stringFromMeta(metadata.value, 'type', 'project'))
const memoryScope = ref(stringFromMeta(metadata.value, 'scope', 'project'))
const memoryConfidence = ref(stringFromMeta(metadata.value, 'confidence', 'medium'))

// PAI-347: confidence tooltip — explains the three levels so authors
// pick deliberately. Surfaced on the label icon + the select itself.
const confidenceTooltip =
  'high = applied multiple times with no exception\n' +
  'medium = solid rule with known edge cases\n' +
  'low = working hypothesis'
const memoryEnvironments = ref(arrayFromMeta(metadata.value, 'applies_to_environments'))
const memoryOriginatingTickets = ref(arrayFromMeta(metadata.value, 'originating_tickets'))
const memoryEnvironmentsInput = ref(memoryEnvironments.value.join(', '))
const memoryTicketsInput = ref(memoryOriginatingTickets.value.join(', '))
// PAI-348 — inherit flag. Default `true` matches the bundle resolver's
// memoryInheritsFlag (existing entries without the field still
// inherit). Persisted under `metadata.inherit` so the server validator
// in handlers/knowledge/memory.go can enforce the bool type.
const memoryInherit = ref(boolFromMeta(metadata.value, 'inherit', true))

const externalUrl = ref(stringFromMeta(metadata.value, 'url', ''))
const externalPurpose = ref(stringFromMeta(metadata.value, 'purpose', ''))
const externalSecretPath = ref(stringFromMeta(metadata.value, 'secret_path', ''))

const relatedInstanceUrl = ref(stringFromMeta(metadata.value, 'instance_url', ''))
const relatedKey = ref(stringFromMeta(metadata.value, 'key', ''))
const relatedRelationship = ref(stringFromMeta(metadata.value, 'relationship', ''))

const runbookRelatedAgents = ref(arrayFromMeta(metadata.value, 'related_agents'))
const runbookAgentsInput = ref(runbookRelatedAgents.value.join(', '))

const guidelineRule = ref(stringFromMeta(metadata.value, 'rule', ''))

// ── PAI-342: Originating Tickets (memory only) ──────────────────────
//
// Pulls live reverse-direction rows from /api/issues/:memoryId/
// relations?type=applies_to_memory. The `metadata.originating_tickets`
// array remains the curated list (free-text keys, may include
// cross-instance refs); the live list reflects the in-instance graph
// and supplements it. Both surfaces stay visible — the curated array
// covers PAI-338's documented contract (cross-instance refs), the
// live list covers the bidirectional UX PAI-342 promises.

const auth = useAuthStore()
const { confirm } = useConfirm()
const linkedTickets = ref<IssueRelation[]>([])
const linkedTicketsError = ref('')

async function loadOriginatingTickets() {
  if (props.category !== 'memory' || !props.entryId) {
    linkedTickets.value = []
    return
  }
  try {
    const all = await loadIssueRelations(props.entryId)
    // Reverse direction: the issue endpoint surfaces rows where the
    // memory is the target side (direction='incoming' from the
    // memory's perspective). Filter to type='applies_to_memory' so
    // any other relation types that might land on a memory entry
    // (parent links, etc.) don't pollute this view.
    linkedTickets.value = all.filter(
      (r) => r.type === 'applies_to_memory' && r.direction === 'incoming',
    )
  } catch {
    linkedTickets.value = []
  }
}

void loadOriginatingTickets()
watch(() => props.entryId, () => loadOriginatingTickets())

const canEditLinks = computed(() => auth.isAdmin)

async function unlinkTicket(rel: IssueRelation) {
  if (!props.entryId) return
  if (!await confirm({ message: `Unlink ${rel.target_key ?? 'this ticket'} from this memory?`, confirmLabel: 'Unlink' })) return
  try {
    // Mirror the original direction: the underlying row has the
    // ticket as source and the memory as target, so we POST against
    // the ticket id (rel.source_id) — not the memory.
    await removeIssueRelation(rel.source_id, props.entryId, 'applies_to_memory')
    linkedTickets.value = linkedTickets.value.filter(
      (r) => !(r.source_id === rel.source_id && r.target_id === rel.target_id && r.type === rel.type),
    )
  } catch (e: unknown) {
    linkedTicketsError.value = e instanceof Error ? e.message : 'Failed to unlink ticket.'
  }
}

function ticketRoute(rel: IssueRelation): string {
  return props.projectId
    ? `/projects/${props.projectId}/issues/${rel.source_id}`
    : `/issues/${rel.source_id}`
}

// Markdown preview toggle — defaults to off in v1 (textarea-only is
// "good enough" per PAI-339's out-of-scope list) but the toggle keeps
// the keyboard-driven authoring loop fast.
const previewEnabled = ref(false)
const { html: previewHtml } = useMarkdown(body, previewEnabled)

const slugTouched = ref(props.currentSlug !== null)
function onTitleInput() {
  if (!props.autosuggestSlug) return
  if (slugTouched.value) return
  slug.value = suggestSlug(title.value)
}
function onSlugInput() {
  slugTouched.value = true
}

const slugError = computed(() => validateKnowledgeSlug(slug.value))
const titleError = computed(() => (title.value.trim() === '' ? 'Title required.' : ''))
const externalUrlError = computed(() => {
  if (props.category !== 'external_system') return ''
  const trimmed = externalUrl.value.trim()
  if (!trimmed) return ''
  try {
    const u = new URL(trimmed)
    if (!u.protocol || !u.host) return 'URL must be absolute (scheme + host).'
    return ''
  } catch {
    return 'URL must parse as a valid URL.'
  }
})

const formValid = computed(
  () => slugError.value === '' && titleError.value === '' && externalUrlError.value === '',
)

const isArchived = computed(() => status.value === archivedStatusValue())

function toggleArchived() {
  status.value = isArchived.value ? activeStatusValue() : archivedStatusValue()
}

// ── PAI-345: promotion (memory only) ───────────────────────────────
//
// "Current scope" detection: project_id > 0 → project (the editor
// is hosted under /projects/:id, so this is the only branch the UI
// currently exposes). user / instance scopes are reserved for the
// dedicated user / instance editor surfaces (not built in v1 — but
// the server-side machinery exists). The button row greys out the
// current scope so the user can't pick a no-op promotion.
const promoting = ref<MemoryScope | null>(null)
const promoteError = ref('')
const canPromote = computed(
  () => props.category === 'memory' && props.currentSlug !== null && (props.projectId ?? 0) > 0,
)
const isAdmin = computed(() => auth.isAdmin)
const currentScope = computed<MemoryScope>(() => {
  // Editor is project-rooted in v1. Future: detect user / instance
  // when the editor is reused on /users/me/memory or /instance/memory.
  return 'project'
})

async function onPromote(target: MemoryScope) {
  if (!canPromote.value || promoting.value) return
  if (target === currentScope.value) return
  if (!props.currentSlug) return
  const label =
    target === 'project'
      ? 'project'
      : target === 'user'
        ? 'your user-scope memory (visible across all your projects)'
        : 'instance-scope memory (visible to all users)'
  if (!await confirm({
    message: `Promote "${props.currentSlug}" to ${label}? The current entry will be archived.`,
    confirmLabel: 'Promote',
  })) return
  promoting.value = target
  promoteError.value = ''
  try {
    await promoteMemory(props.currentSlug, {
      to: target,
      from_project_id: props.projectId,
    })
    emit('promoted', target)
  } catch (e: unknown) {
    promoteError.value = e instanceof Error ? e.message : 'Promotion failed.'
  } finally {
    promoting.value = null
  }
}

function buildPayload(): KnowledgeEntryInput {
  // Start from the original metadata so unknown fields survive.
  const meta: Record<string, unknown> = { ...metadata.value }
  if (props.category === 'memory') {
    meta.type = memoryType.value
    meta.scope = memoryScope.value
    meta.confidence = memoryConfidence.value
    meta.applies_to_environments = parseList(memoryEnvironmentsInput.value)
    meta.originating_tickets = parseList(memoryTicketsInput.value)
    // PAI-348 — only persist `inherit` when explicitly false. Omitting
    // the field defaults to true (the bundle resolver's contract), so
    // we keep existing entries untouched and only write the opt-out.
    if (memoryInherit.value === false) {
      meta.inherit = false
    } else {
      delete meta.inherit
    }
  } else if (props.category === 'external_system') {
    if (externalUrl.value.trim() !== '') meta.url = externalUrl.value.trim()
    else delete meta.url
    if (externalPurpose.value.trim() !== '') meta.purpose = externalPurpose.value.trim()
    else delete meta.purpose
    if (externalSecretPath.value.trim() !== '') meta.secret_path = externalSecretPath.value.trim()
    else delete meta.secret_path
  } else if (props.category === 'related_project') {
    if (relatedInstanceUrl.value.trim() !== '') meta.instance_url = relatedInstanceUrl.value.trim()
    else delete meta.instance_url
    if (relatedKey.value.trim() !== '') meta.key = relatedKey.value.trim()
    else delete meta.key
    if (relatedRelationship.value.trim() !== '') meta.relationship = relatedRelationship.value.trim()
    else delete meta.relationship
  } else if (props.category === 'runbook') {
    meta.related_agents = parseList(runbookAgentsInput.value)
  } else if (props.category === 'guideline') {
    if (guidelineRule.value.trim() !== '') meta.rule = guidelineRule.value.trim()
    else delete meta.rule
  }
  return {
    slug: slug.value.trim(),
    title: title.value.trim(),
    body: body.value,
    status: status.value,
    metadata: meta,
  }
}

function onSave() {
  if (!formValid.value) return
  emit('save', buildPayload())
}

function parseList(raw: string): string[] {
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '')
}

function stringFromMeta(meta: Record<string, unknown>, key: string, fallback: string): string {
  const v = meta[key]
  return typeof v === 'string' ? v : fallback
}

function arrayFromMeta(meta: Record<string, unknown>, key: string): string[] {
  const v = meta[key]
  if (!Array.isArray(v)) return []
  return v.filter((s): s is string => typeof s === 'string')
}

// PAI-348 — strict bool reader for `inherit`. The server validator
// rejects non-bool values, so we only accept `true` / `false` here.
// Anything else falls back to the default so a partially-migrated
// entry doesn't render as a confusing "tri-state" checkbox.
function boolFromMeta(
  meta: Record<string, unknown>,
  key: string,
  fallback: boolean,
): boolean {
  const v = meta[key]
  return typeof v === 'boolean' ? v : fallback
}

// Re-sync local state if parent swaps the entry under us (e.g. user
// clicks a different row mid-edit). Without this the editor would
// keep the previous row's body when the keyed parent re-renders.
watch(
  () => props.initial,
  (next) => {
    slug.value = next.slug ?? ''
    title.value = next.title ?? ''
    body.value = next.body ?? ''
    status.value = next.status ?? activeStatusValue()
    metadata.value = { ...(next.metadata ?? {}) }
    memoryType.value = stringFromMeta(metadata.value, 'type', 'project')
    memoryScope.value = stringFromMeta(metadata.value, 'scope', 'project')
    memoryConfidence.value = stringFromMeta(metadata.value, 'confidence', 'medium')
    memoryEnvironmentsInput.value = arrayFromMeta(metadata.value, 'applies_to_environments').join(', ')
    memoryTicketsInput.value = arrayFromMeta(metadata.value, 'originating_tickets').join(', ')
    memoryInherit.value = boolFromMeta(metadata.value, 'inherit', true)
    externalUrl.value = stringFromMeta(metadata.value, 'url', '')
    externalPurpose.value = stringFromMeta(metadata.value, 'purpose', '')
    externalSecretPath.value = stringFromMeta(metadata.value, 'secret_path', '')
    relatedInstanceUrl.value = stringFromMeta(metadata.value, 'instance_url', '')
    relatedKey.value = stringFromMeta(metadata.value, 'key', '')
    relatedRelationship.value = stringFromMeta(metadata.value, 'relationship', '')
    runbookAgentsInput.value = arrayFromMeta(metadata.value, 'related_agents').join(', ')
    guidelineRule.value = stringFromMeta(metadata.value, 'rule', '')
  },
)
</script>

<template>
  <div class="ke-form">
    <div class="ke-row">
      <div class="ke-field">
        <label>Title</label>
        <input v-model="title" type="text" placeholder="Short headline" @input="onTitleInput" />
        <span v-if="titleError" class="ke-field-error">{{ titleError }}</span>
      </div>
      <div class="ke-field">
        <label>Slug <span class="ke-hint">[a-z][a-z0-9_-]*, max 64</span></label>
        <input v-model="slug" type="text" maxlength="64" class="ke-mono" @input="onSlugInput" />
        <span v-if="slugError" class="ke-field-error">{{ slugError }}</span>
      </div>
    </div>

    <!-- Per-category metadata fields. Each block is intentionally
         narrow — the common case is "title + body"; the metadata
         section adds 1–2 inputs only when the category needs them. -->
    <template v-if="category === 'memory'">
      <div class="ke-row">
        <div class="ke-field">
          <label>Type</label>
          <select v-model="memoryType">
            <option value="feedback">feedback</option>
            <option value="project">project</option>
            <option value="reference">reference</option>
            <option value="user">user</option>
          </select>
        </div>
        <div class="ke-field">
          <label>Scope</label>
          <select v-model="memoryScope">
            <option value="project">project</option>
            <option value="user-on-this-project">user-on-this-project</option>
          </select>
        </div>
        <div class="ke-field">
          <label>
            Confidence
            <span
              class="ke-info"
              :title="confidenceTooltip"
              aria-label="Confidence definitions"
            >&#9432;</span>
          </label>
          <select v-model="memoryConfidence" :title="confidenceTooltip">
            <option value="high">high</option>
            <option value="medium">medium</option>
            <option value="low">low</option>
          </select>
        </div>
      </div>
      <div class="ke-row">
        <div class="ke-field">
          <label>Applies to environments <span class="ke-hint">comma-separated</span></label>
          <input v-model="memoryEnvironmentsInput" type="text" placeholder="staging, prod" />
        </div>
        <div class="ke-field">
          <label>Originating tickets <span class="ke-hint">comma-separated keys (free-text, cross-instance OK)</span></label>
          <input v-model="memoryTicketsInput" type="text" placeholder="PAI-339, PAI-353" class="ke-mono" />
        </div>
      </div>

      <!-- PAI-348 — opt out of inheritance per-memory. Default is
           checked: most rules ARE general, so they propagate to
           projects that declare this project as related/upstream. -->
      <div class="ke-field">
        <label class="ke-inline-toggle">
          <input
            v-model="memoryInherit"
            type="checkbox"
            data-testid="memory-inherit-checkbox"
          />
          <span>Inherit</span>
          <span
            class="ke-hint"
            title="When unchecked, this memory will not be visible to projects that declare this project as related/upstream."
          >
            propagate to downstream projects
          </span>
        </label>
      </div>

      <!-- PAI-342: Live reverse-direction view of issues linked via the
           `applies_to_memory` relation. Distinct from the free-text
           array above — this is the in-instance graph. Only renders
           for an existing entry (entryId set). -->
      <div v-if="entryId && linkedTickets.length" class="ke-field">
        <label>Linked from tickets <span class="ke-hint">live, in-instance</span></label>
        <div class="ke-ticket-chips">
          <div v-for="rel in linkedTickets" :key="`${rel.source_id}-${rel.target_id}`" class="ke-ticket-chip">
            <a :href="ticketRoute(rel)" class="ke-ticket-key">
              {{ rel.target_key || rel.source_id }}
            </a>
            <span v-if="rel.target_title" class="ke-ticket-title">{{ rel.target_title }}</span>
            <button
              v-if="canEditLinks"
              type="button"
              class="ke-ticket-del"
              title="Unlink"
              @click="unlinkTicket(rel)"
            >×</button>
          </div>
        </div>
        <span v-if="linkedTicketsError" class="ke-field-error">{{ linkedTicketsError }}</span>
      </div>
    </template>

    <template v-else-if="category === 'external_system'">
      <div class="ke-row">
        <div class="ke-field">
          <label>URL <span class="ke-hint">must parse as absolute URL</span></label>
          <input v-model="externalUrl" type="text" placeholder="https://sentry.example.com/org" class="ke-mono" />
          <span v-if="externalUrlError" class="ke-field-error">{{ externalUrlError }}</span>
        </div>
        <div class="ke-field">
          <label>Purpose</label>
          <input v-model="externalPurpose" type="text" placeholder="error tracking" />
        </div>
      </div>
      <div class="ke-field">
        <label>Secret path <span class="ke-hint">e.g. ~/Secrets/sentry.env</span></label>
        <input v-model="externalSecretPath" type="text" placeholder="~/Secrets/…" class="ke-mono" />
      </div>
    </template>

    <template v-else-if="category === 'related_project'">
      <div class="ke-row">
        <div class="ke-field">
          <label>Instance URL <span class="ke-hint">required for cross-instance refs</span></label>
          <input v-model="relatedInstanceUrl" type="text" placeholder="https://pm.example.com" class="ke-mono" />
        </div>
        <div class="ke-field">
          <label>Project key</label>
          <input v-model="relatedKey" type="text" placeholder="ACME" class="ke-mono" />
        </div>
        <div class="ke-field">
          <label>Relationship</label>
          <input v-model="relatedRelationship" type="text" placeholder="upstream / shared-customer / fork" />
        </div>
      </div>
    </template>

    <template v-else-if="category === 'runbook'">
      <div class="ke-field">
        <label>Related agents <span class="ke-hint">comma-separated agent slugs</span></label>
        <input v-model="runbookAgentsInput" type="text" placeholder="ops, dev" class="ke-mono" />
      </div>
    </template>

    <template v-else-if="category === 'guideline'">
      <div class="ke-field">
        <label>One-liner rule <span class="ke-hint">surfaces in agent prompts</span></label>
        <input v-model="guidelineRule" type="text" placeholder="Use 'prod' not 'live'" />
      </div>
    </template>

    <div class="ke-field">
      <div class="ke-body-head">
        <label>Body <span class="ke-hint">markdown</span></label>
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          :class="{ active: previewEnabled }"
          @click="previewEnabled = !previewEnabled"
        >
          {{ previewEnabled ? 'Edit' : 'Preview' }}
        </button>
      </div>
      <textarea
        v-if="!previewEnabled"
        v-model="body"
        class="ke-textarea"
        rows="10"
        placeholder="## Context&#10;&#10;…"
      />
      <div
        v-else
        class="ke-preview"
        v-html="previewHtml"
      />
    </div>

    <!-- PAI-345: promote action — memory only, existing entries only.
         The current scope is greyed (per the ticket's UX note);
         instance scope is admin-gated server-side, but we also
         disable it in the UI so non-admins don't get a 403 on
         click. -->
    <div v-if="canPromote" class="ke-promote">
      <span class="ke-promote-label">Promote to:</span>
      <button
        type="button"
        class="btn btn-ghost btn-sm"
        :class="{ active: currentScope === 'project' }"
        :disabled="currentScope === 'project' || promoting !== null"
        title="Current scope"
        @click="onPromote('project')"
      >Project</button>
      <button
        type="button"
        class="btn btn-ghost btn-sm"
        :class="{ active: currentScope === 'user' }"
        :disabled="currentScope === 'user' || promoting !== null"
        title="Visible across all your projects"
        @click="onPromote('user')"
      >{{ promoting === 'user' ? 'Promoting…' : 'User' }}</button>
      <button
        type="button"
        class="btn btn-ghost btn-sm"
        :class="{ active: currentScope === 'instance' }"
        :disabled="currentScope === 'instance' || promoting !== null || !isAdmin"
        :title="isAdmin ? 'Visible to all users on this server' : 'Admin only'"
        @click="onPromote('instance')"
      >{{ promoting === 'instance' ? 'Promoting…' : 'Instance' }}</button>
      <span v-if="promoteError" class="ke-error">{{ promoteError }}</span>
    </div>

    <div class="ke-actions">
      <button
        type="button"
        class="btn btn-ghost btn-sm"
        :class="{ active: isArchived }"
        @click="toggleArchived"
      >
        {{ isArchived ? 'Archived' : 'Active' }}
      </button>
      <span v-if="saveError" class="ke-error">{{ saveError }}</span>
      <span class="ke-actions-spacer" />
      <button type="button" class="btn btn-ghost btn-sm" @click="emit('cancel')">Cancel</button>
      <button
        type="button"
        class="btn btn-primary btn-sm"
        :disabled="!formValid || saving"
        @click="onSave"
      >
        {{ saving ? 'Saving…' : currentSlug === null ? 'Add' : 'Save' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.ke-form { display: flex; flex-direction: column; gap: .65rem; padding: .75rem; background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; }
.ke-row { display: flex; gap: .65rem; flex-wrap: wrap; }
.ke-row > .ke-field { flex: 1 1 200px; }
.ke-field { display: flex; flex-direction: column; gap: .2rem; min-width: 0; }
.ke-field label { font-size: 12px; color: var(--text-muted); font-weight: 600; text-transform: uppercase; letter-spacing: .04em; }
.ke-field input, .ke-field select, .ke-field textarea { width: 100%; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text); font: inherit; padding: .45rem .55rem; box-sizing: border-box; }
.ke-field-error { color: #b42318; font-size: 11px; }
.ke-hint { color: var(--text-muted); font-weight: 400; font-size: 11px; text-transform: none; letter-spacing: 0; }
.ke-info { color: var(--text-muted); font-weight: 400; font-size: 12px; cursor: help; margin-left: .25rem; }
.ke-mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
.ke-inline-toggle { display: inline-flex; align-items: center; gap: .4rem; cursor: pointer; font-size: 12px; }
.ke-inline-toggle input[type="checkbox"] { width: auto; margin: 0; }
.ke-textarea { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; min-height: 200px; resize: vertical; }
.ke-body-head { display: flex; align-items: center; justify-content: space-between; gap: .5rem; }
.ke-body-head > label { margin-bottom: 0; }
.ke-preview { padding: .65rem .75rem; min-height: 200px; border: 1px dashed var(--border); border-radius: 6px; background: var(--bg); font-size: 13px; line-height: 1.55; overflow: auto; }
.ke-preview :deep(h1), .ke-preview :deep(h2), .ke-preview :deep(h3) { margin: .6rem 0 .3rem; font-weight: 700; }
.ke-preview :deep(p) { margin: .35rem 0; }
.ke-preview :deep(code) { background: var(--bg-card); padding: 0 .25rem; border-radius: 3px; font-size: 12px; }
.ke-preview :deep(pre) { background: var(--bg-card); padding: .5rem .65rem; border-radius: 6px; overflow: auto; }
.ke-actions { display: flex; gap: .4rem; align-items: center; flex-wrap: wrap; }
.ke-actions-spacer { flex: 1; }
.ke-error { color: #b42318; font-size: 12px; }

/* PAI-345: scope-promotion controls. Reuses the global btn / btn-sm
   chrome so the row visually matches the active/archived toggle. */
.ke-promote { display: flex; gap: .4rem; align-items: center; flex-wrap: wrap; }
.ke-promote-label { font-size: 12px; color: var(--text-muted); font-weight: 600; text-transform: uppercase; letter-spacing: .04em; }

/* PAI-342: linked-tickets reverse-direction chips. */
.ke-ticket-chips { display: flex; flex-wrap: wrap; gap: .35rem; }
.ke-ticket-chip {
  display: inline-flex; align-items: center; gap: .3rem;
  background: var(--surface-2, var(--bg-card)); border: 1px solid var(--border);
  border-radius: 6px; padding: .2rem .5rem; font-size: 12px;
}
.ke-ticket-key { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-weight: 700; color: var(--bp-blue); text-decoration: none; }
.ke-ticket-key:hover { text-decoration: underline; }
.ke-ticket-title { color: var(--text-muted); max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ke-ticket-del { background: none; border: none; cursor: pointer; color: var(--text-muted); font-size: 14px; line-height: 1; padding: 0 .15rem; border-radius: 3px; }
.ke-ticket-del:hover { color: #c0392b; }

/* Mobile: 375px viewport. Stack rows so labels + inputs each get a
   full line; otherwise inputs collapse below their min-content. */
@media (max-width: 540px) {
  .ke-row { flex-direction: column; }
  .ke-row > .ke-field { flex: 1 1 auto; }
  .ke-actions { justify-content: flex-end; }
}
</style>
