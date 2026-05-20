<!--
  PAI-468 — shared IssueTable.

  Lightweight, registry-driven table the customer portal (PAI-470) and
  admin visibility report (PAI-467) consume. The internal IssueList
  retains its bespoke IssueTable.vue for now — it's tied to inline-edit,
  selection mode, AI menu, and sprint pickers. Unifying them was the
  long-term intent of this ticket, but the deeper refactor risks
  regressions beyond this epic's scope, so PAI-468 ships the shared
  component fresh and leaves the internal one alone.
-->
<script setup lang="ts">
import { computed, isVNode, type VNode } from 'vue'

import AppIcon from '@/components/AppIcon.vue'
import type { ColumnDef, RowAction, EmptyState, IssueLike } from './types'

// Generic over the issue row type — internal Issue, PortalIssue, and
// AdminVisibilityIssue all satisfy IssueLike (id-only). Consumers pass
// their richer type into ColumnDef<T>; the table itself only needs id
// for `:key` and to forward to row-click / row-actions.
const props = defineProps<{
  issues: IssueLike[]
  columns: ColumnDef<any>[]
  loading?: boolean
  density?: 'normal' | 'compact'
  rowActions?: (issue: any) => RowAction[]
  emptyState?: EmptyState
  sort?: { col: string; dir: 'asc' | 'desc' }
}>()

const emit = defineEmits<{
  sort: [col: string]
  'row-click': [issue: IssueLike]
}>()

const compact = computed(() => props.density === 'compact')

function onHeaderClick(col: ColumnDef) {
  if (!col.sortable) return
  emit('sort', col.key)
}

function sortIndicator(col: ColumnDef): string {
  if (!col.sortable) return ''
  if (props.sort?.col !== col.key) return 'chevrons-up-down'
  return props.sort.dir === 'asc' ? 'chevron-up' : 'chevron-down'
}

function rowClick(issue: IssueLike, event: MouseEvent) {
  // Skip if the click landed on an action button — those have their own handlers.
  const target = event.target as HTMLElement
  if (target.closest('.it-action')) return
  emit('row-click', issue)
}

function isVNodeCell(v: unknown): v is VNode {
  return isVNode(v as VNode)
}

function cellText(col: ColumnDef<any>, issue: IssueLike): string {
  const v = col.render(issue)
  if (v == null) return '—'
  if (typeof v === 'string' || typeof v === 'number') return String(v)
  return ''
}
</script>

<template>
  <div class="it-wrap" :class="{ 'it-wrap--compact': compact }">
    <table class="it-table" :class="{ 'it-table--loading': loading }">
      <thead>
        <tr>
          <th
            v-for="col in columns"
            :key="col.key"
            :style="col.width ? { width: col.width } : undefined"
            :class="{ 'it-th--sortable': col.sortable }"
            @click="onHeaderClick(col)"
          >
            <span class="it-th-label">{{ col.label }}</span>
            <AppIcon v-if="col.sortable" :name="sortIndicator(col)" :size="11" class="it-th-sort" />
          </th>
          <th v-if="rowActions" class="it-th-actions" />
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="issue in issues"
          :key="issue.id"
          class="it-row"
          @click="rowClick(issue, $event)"
        >
          <td v-for="col in columns" :key="col.key" class="it-cell">
            <component
              :is="col.render(issue)"
              v-if="isVNodeCell(col.render(issue))"
            />
            <span v-else>{{ cellText(col, issue) }}</span>
          </td>
          <td v-if="rowActions" class="it-cell it-cell--actions">
            <button
              v-for="ra in rowActions(issue)"
              :key="ra.key"
              :class="['it-action', `it-action--${ra.variant ?? 'ghost'}`]"
              :disabled="ra.disabled"
              @click.stop="ra.onClick"
            >
              {{ ra.label }}
            </button>
          </td>
        </tr>
        <tr v-if="!issues.length && !loading">
          <td :colspan="columns.length + (rowActions ? 1 : 0)" class="it-empty">
            <div v-if="emptyState" class="it-empty-state">
              <div class="it-empty-title">{{ emptyState.title }}</div>
              <div v-if="emptyState.subtitle" class="it-empty-subtitle">{{ emptyState.subtitle }}</div>
              <button
                v-if="emptyState.actionLabel"
                class="it-action it-action--primary"
                @click="emptyState.onAction?.()"
              >
                {{ emptyState.actionLabel }}
              </button>
            </div>
            <div v-else class="it-empty-state">—</div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.it-wrap {
  width: 100%;
  overflow-x: auto;
}

.it-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.875rem;
}

.it-table--loading { opacity: 0.55; }

.it-table thead th {
  text-align: left;
  font-weight: 600;
  padding: 0.5rem 0.625rem;
  background: var(--bg-subtle, #f9fafb);
  border-bottom: 1px solid var(--border, #e5e7eb);
  position: sticky;
  top: 0;
  z-index: 1;
}

.it-th--sortable {
  cursor: pointer;
  user-select: none;
}

.it-th--sortable:hover {
  background: color-mix(in srgb, var(--brand, #2563eb) 6%, var(--bg-subtle, #f9fafb));
}

.it-th-label { margin-right: 0.25rem; }
.it-th-sort { vertical-align: middle; color: var(--text-muted, #9ca3af); }

.it-row {
  cursor: pointer;
}
.it-row:hover {
  background: color-mix(in srgb, var(--brand, #2563eb) 4%, transparent);
}

.it-cell {
  padding: 0.5rem 0.625rem;
  border-bottom: 1px solid var(--border, #e5e7eb);
  vertical-align: middle;
}

.it-wrap--compact .it-cell,
.it-wrap--compact .it-table thead th {
  padding: 0.375rem 0.5rem;
  font-size: 0.8125rem;
}

.it-cell--actions {
  text-align: right;
  white-space: nowrap;
}

.it-action {
  border: none;
  background: transparent;
  font-weight: 600;
  padding: 0.25rem 0.625rem;
  border-radius: 6px;
  cursor: pointer;
  font-size: 0.8125rem;
}

.it-action--primary {
  background: var(--brand, #2563eb);
  color: white;
}
.it-action--ghost {
  background: var(--bg-subtle, #f9fafb);
  color: var(--text, #1f2937);
}
.it-action--ghost:hover {
  background: color-mix(in srgb, var(--brand, #2563eb) 10%, var(--bg-subtle, #f9fafb));
}
.it-action--danger {
  color: #b91c1c;
}
.it-action:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.it-empty {
  text-align: center;
  padding: 2rem 1rem;
}

.it-empty-state {
  color: var(--text-muted, #6b7280);
}
.it-empty-title { font-weight: 600; margin-bottom: 0.25rem; }
.it-empty-subtitle { font-size: 0.8125rem; margin-bottom: 0.75rem; }
</style>
