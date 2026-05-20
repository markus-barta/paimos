<!--
  PAI-469 — shared IssueFilterBar.

  Multi-select pills (status, type, priority, tags) + inline search,
  active-chip row, and mobile slide-up sheet. The customer portal
  (PAI-470) consumes it via the `enabledFilters` prop; the internal
  IssueList retains its existing IssueFilterPanel for now — same
  scope-cut reasoning as PAI-468's IssueTable note. Future work can
  fold IssueList onto this surface once the regression risk is paid
  down in a dedicated cycle.

  Layout:
    [🔎 Search …] [Status ▾] [Type ▾] [Priority ▾] [Tags ▾]    Clear all
                              ⌃ chip row with active selections

  Below 720px the pills collapse into a single "Filters (n)" button
  that opens a slide-up sheet (PAI-471). The sheet is wired in this
  component so the consumer doesn't have to manage backdrop/animation
  state — pass enabledFilters and the sheet stays in sync.
-->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import AppIcon from '@/components/AppIcon.vue'
import type {
  EnabledFilter,
  FilterOption,
  SharedFilterState,
  TagOption,
} from './types'

const props = defineProps<{
  modelValue: SharedFilterState
  enabledFilters: EnabledFilter[]
  statusOptions: FilterOption[]
  typeOptions: FilterOption[]
  priorityOptions?: FilterOption[]
  tagOptions?: TagOption[]
  density?: 'normal' | 'compact'
  /** Forced viewport breakpoint (test-only escape hatch). */
  mobileBreakpoint?: number
}>()

const emit = defineEmits<{
  'update:modelValue': [next: SharedFilterState]
}>()

const { t } = useI18n()

const filters = computed(() => props.modelValue)

function updateField<K extends keyof SharedFilterState>(
  key: K,
  value: SharedFilterState[K],
) {
  emit('update:modelValue', { ...filters.value, [key]: value })
}

function toggleStringFilter(field: 'status' | 'type' | 'priority', value: string) {
  const list = filters.value[field]
  const next = list.includes(value)
    ? list.filter((v) => v !== value)
    : [...list, value]
  updateField(field, next)
}

function toggleTagFilter(tagId: number) {
  const list = filters.value.tagIds
  const next = list.includes(tagId)
    ? list.filter((v) => v !== tagId)
    : [...list, tagId]
  updateField('tagIds', next)
}

function clearAll() {
  emit('update:modelValue', {
    status: [],
    type: [],
    priority: [],
    tagIds: [],
    q: '',
  })
}

function isEnabled(name: EnabledFilter): boolean {
  return props.enabledFilters.includes(name)
}

const activeCount = computed(() => {
  return (
    filters.value.status.length +
    filters.value.type.length +
    filters.value.priority.length +
    filters.value.tagIds.length +
    (filters.value.q ? 1 : 0)
  )
})

// ── Mobile sheet ────────────────────────────────────────────────────────
const sheetOpen = ref(false)
const viewportWidth = ref(typeof window !== 'undefined' ? window.innerWidth : 1280)
const breakpoint = computed(() => props.mobileBreakpoint ?? 720)
const isMobile = computed(() => viewportWidth.value < breakpoint.value)

function onResize() {
  viewportWidth.value = window.innerWidth
}

if (typeof window !== 'undefined') {
  window.addEventListener('resize', onResize, { passive: true })
}

watch(isMobile, (mobile) => {
  // Close the sheet automatically when crossing the breakpoint up — the
  // pills come back, so the sheet is redundant.
  if (!mobile) sheetOpen.value = false
})

// ── Dropdown open state ─────────────────────────────────────────────────
type DropdownKey = 'status' | 'type' | 'priority' | 'tag'
const openDropdown = ref<DropdownKey | null>(null)

function toggleDropdown(k: DropdownKey) {
  openDropdown.value = openDropdown.value === k ? null : k
}

function closeDropdowns() {
  openDropdown.value = null
}

// Helpers for active chips
const activeChips = computed(() => {
  const out: Array<{ key: string; field: keyof SharedFilterState; value: string | number; label: string }> = []
  for (const s of filters.value.status) {
    const opt = props.statusOptions.find((o) => o.value === s)
    out.push({ key: `status:${s}`, field: 'status', value: s, label: opt?.label ?? s })
  }
  for (const tp of filters.value.type) {
    const opt = props.typeOptions.find((o) => o.value === tp)
    out.push({ key: `type:${tp}`, field: 'type', value: tp, label: opt?.label ?? tp })
  }
  if (props.priorityOptions) {
    for (const p of filters.value.priority) {
      const opt = props.priorityOptions.find((o) => o.value === p)
      out.push({ key: `priority:${p}`, field: 'priority', value: p, label: opt?.label ?? p })
    }
  }
  if (props.tagOptions) {
    for (const tid of filters.value.tagIds) {
      const opt = props.tagOptions.find((o) => o.id === tid)
      out.push({ key: `tag:${tid}`, field: 'tagIds', value: tid, label: opt?.name ?? String(tid) })
    }
  }
  return out
})

function removeChip(chip: { field: keyof SharedFilterState; value: string | number }) {
  if (chip.field === 'tagIds') {
    updateField(
      'tagIds',
      filters.value.tagIds.filter((v) => v !== chip.value),
    )
    return
  }
  const arr = filters.value[chip.field] as string[]
  updateField(chip.field as 'status' | 'type' | 'priority', arr.filter((v) => v !== chip.value))
}
</script>

<template>
  <div class="if-bar" :class="{ 'if-bar--mobile': isMobile }">
    <!-- Desktop pills + search -->
    <template v-if="!isMobile">
      <div class="if-bar__row" @click.stop>
        <input
          v-if="isEnabled('q')"
          class="if-bar__search"
          type="search"
          :placeholder="$t('portal.search')"
          :value="filters.q"
          @input="updateField('q', ($event.target as HTMLInputElement).value)"
        />

        <button
          v-if="isEnabled('status')"
          type="button"
          class="if-bar__pill"
          :class="{ 'if-bar__pill--active': filters.status.length > 0 }"
          @click="toggleDropdown('status')"
        >
          {{ $t('portal.filters.allStatus') }}
          <span v-if="filters.status.length" class="if-bar__count">{{ filters.status.length }}</span>
          <AppIcon name="chevron-down" :size="11" />
        </button>

        <button
          v-if="isEnabled('type')"
          type="button"
          class="if-bar__pill"
          :class="{ 'if-bar__pill--active': filters.type.length > 0 }"
          @click="toggleDropdown('type')"
        >
          {{ $t('portal.filters.allTypes') }}
          <span v-if="filters.type.length" class="if-bar__count">{{ filters.type.length }}</span>
          <AppIcon name="chevron-down" :size="11" />
        </button>

        <button
          v-if="isEnabled('priority') && priorityOptions"
          type="button"
          class="if-bar__pill"
          :class="{ 'if-bar__pill--active': filters.priority.length > 0 }"
          @click="toggleDropdown('priority')"
        >
          {{ t('visibility.filterBarPriority') }}
          <span v-if="filters.priority.length" class="if-bar__count">{{ filters.priority.length }}</span>
          <AppIcon name="chevron-down" :size="11" />
        </button>

        <button
          v-if="isEnabled('tag') && tagOptions"
          type="button"
          class="if-bar__pill"
          :class="{ 'if-bar__pill--active': filters.tagIds.length > 0 }"
          @click="toggleDropdown('tag')"
        >
          {{ t('visibility.filterBarTags') }}
          <span v-if="filters.tagIds.length" class="if-bar__count">{{ filters.tagIds.length }}</span>
          <AppIcon name="chevron-down" :size="11" />
        </button>

        <button
          v-if="activeCount > 0"
          type="button"
          class="if-bar__clear"
          @click="clearAll"
        >
          {{ t('visibility.filterBarClearAll') }}
        </button>
      </div>

      <!-- Dropdown panels -->
      <div v-if="openDropdown === 'status'" class="if-bar__panel">
        <button
          v-for="opt in statusOptions"
          :key="opt.value"
          type="button"
          class="if-bar__opt"
          :class="{ 'if-bar__opt--on': filters.status.includes(opt.value) }"
          @click="toggleStringFilter('status', opt.value)"
        >
          {{ opt.label }}
        </button>
      </div>
      <div v-else-if="openDropdown === 'type'" class="if-bar__panel">
        <button
          v-for="opt in typeOptions"
          :key="opt.value"
          type="button"
          class="if-bar__opt"
          :class="{ 'if-bar__opt--on': filters.type.includes(opt.value) }"
          @click="toggleStringFilter('type', opt.value)"
        >
          {{ opt.label }}
        </button>
      </div>
      <div
        v-else-if="openDropdown === 'priority' && priorityOptions"
        class="if-bar__panel"
      >
        <button
          v-for="opt in priorityOptions"
          :key="opt.value"
          type="button"
          class="if-bar__opt"
          :class="{ 'if-bar__opt--on': filters.priority.includes(opt.value) }"
          @click="toggleStringFilter('priority', opt.value)"
        >
          {{ opt.label }}
        </button>
      </div>
      <div
        v-else-if="openDropdown === 'tag' && tagOptions"
        class="if-bar__panel"
      >
        <button
          v-for="opt in tagOptions"
          :key="opt.id"
          type="button"
          class="if-bar__opt"
          :class="{ 'if-bar__opt--on': filters.tagIds.includes(opt.id) }"
          @click="toggleTagFilter(opt.id)"
        >
          {{ opt.name }}
        </button>
      </div>

      <!-- Active chips row -->
      <div v-if="activeChips.length" class="if-bar__chips">
        <span
          v-for="chip in activeChips"
          :key="chip.key"
          class="if-bar__chip"
        >
          {{ chip.label }}
          <button
            type="button"
            class="if-bar__chip-x"
            :aria-label="'Remove ' + chip.label"
            @click="removeChip(chip)"
          >
            <AppIcon name="x" :size="10" />
          </button>
        </span>
      </div>
    </template>

    <!-- Mobile: collapsed pills → single "Filters (n)" button + sheet -->
    <template v-else>
      <button
        type="button"
        class="if-bar__mobile-toggle"
        @click="sheetOpen = true"
      >
        <AppIcon name="filter" :size="14" />
        {{ t('visibility.filterBarFilters') }}
        <span v-if="activeCount" class="if-bar__count">{{ activeCount }}</span>
      </button>

      <Teleport to="body">
        <div v-if="sheetOpen" class="if-bar__backdrop" @click="sheetOpen = false">
          <div class="if-bar__sheet" @click.stop>
            <div class="if-bar__sheet-handle" aria-hidden="true" />
            <header class="if-bar__sheet-head">
              <h3>{{ t('visibility.filterBarFilters') }}</h3>
              <button
                v-if="activeCount > 0"
                type="button"
                class="if-bar__clear"
                @click="clearAll"
              >
                {{ t('visibility.filterBarClearAll') }}
              </button>
              <button
                type="button"
                class="if-bar__sheet-close"
                @click="sheetOpen = false"
                aria-label="Close filters"
              >
                <AppIcon name="x" :size="16" />
              </button>
            </header>

            <input
              v-if="isEnabled('q')"
              class="if-bar__search"
              type="search"
              :placeholder="$t('portal.search')"
              :value="filters.q"
              @input="updateField('q', ($event.target as HTMLInputElement).value)"
            />

            <section v-if="isEnabled('status')" class="if-bar__group">
              <h4>{{ $t('portal.filters.allStatus') }}</h4>
              <div class="if-bar__opt-row">
                <button
                  v-for="opt in statusOptions"
                  :key="opt.value"
                  type="button"
                  class="if-bar__opt"
                  :class="{ 'if-bar__opt--on': filters.status.includes(opt.value) }"
                  @click="toggleStringFilter('status', opt.value)"
                >
                  {{ opt.label }}
                </button>
              </div>
            </section>
            <section v-if="isEnabled('type')" class="if-bar__group">
              <h4>{{ $t('portal.filters.allTypes') }}</h4>
              <div class="if-bar__opt-row">
                <button
                  v-for="opt in typeOptions"
                  :key="opt.value"
                  type="button"
                  class="if-bar__opt"
                  :class="{ 'if-bar__opt--on': filters.type.includes(opt.value) }"
                  @click="toggleStringFilter('type', opt.value)"
                >
                  {{ opt.label }}
                </button>
              </div>
            </section>
            <section v-if="isEnabled('priority') && priorityOptions" class="if-bar__group">
              <h4>{{ t('visibility.filterBarPriority') }}</h4>
              <div class="if-bar__opt-row">
                <button
                  v-for="opt in priorityOptions"
                  :key="opt.value"
                  type="button"
                  class="if-bar__opt"
                  :class="{ 'if-bar__opt--on': filters.priority.includes(opt.value) }"
                  @click="toggleStringFilter('priority', opt.value)"
                >
                  {{ opt.label }}
                </button>
              </div>
            </section>
            <section v-if="isEnabled('tag') && tagOptions" class="if-bar__group">
              <h4>{{ t('visibility.filterBarTags') }}</h4>
              <div class="if-bar__opt-row">
                <button
                  v-for="opt in tagOptions"
                  :key="opt.id"
                  type="button"
                  class="if-bar__opt"
                  :class="{ 'if-bar__opt--on': filters.tagIds.includes(opt.id) }"
                  @click="toggleTagFilter(opt.id)"
                >
                  {{ opt.name }}
                </button>
              </div>
            </section>
          </div>
        </div>
      </Teleport>
    </template>
  </div>
</template>

<style scoped>
.if-bar {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.if-bar__row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.if-bar__search {
  flex: 1;
  min-width: 12rem;
  padding: 0.375rem 0.625rem;
  border-radius: 6px;
  border: 1px solid var(--border, #e5e7eb);
  font-size: 0.875rem;
  background: white;
}

.if-bar__pill {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  padding: 0.3rem 0.75rem;
  border-radius: 999px;
  border: 1px solid var(--border, #e5e7eb);
  background: var(--bg-subtle, #f9fafb);
  color: var(--text-muted, #6b7280);
  font-size: 0.8125rem;
  font-weight: 500;
  cursor: pointer;
  transition: border-color 120ms, color 120ms, background 120ms;
}
.if-bar__pill:hover {
  border-color: var(--brand, #2563eb);
  color: var(--brand, #2563eb);
}
.if-bar__pill--active {
  background: color-mix(in srgb, var(--brand, #2563eb) 12%, transparent);
  border-color: var(--brand, #2563eb);
  color: var(--brand, #2563eb);
}

.if-bar__count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 1.125rem;
  height: 1.125rem;
  padding: 0 0.375rem;
  border-radius: 999px;
  background: var(--brand, #2563eb);
  color: white;
  font-size: 0.6875rem;
  font-weight: 700;
}

.if-bar__clear {
  margin-left: auto;
  border: none;
  background: transparent;
  color: var(--brand, #2563eb);
  font-size: 0.8125rem;
  cursor: pointer;
  padding: 0.25rem 0.5rem;
  font-weight: 600;
}

.if-bar__panel {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
  padding: 0.5rem 0.625rem;
  background: white;
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 8px;
}

.if-bar__opt {
  border: 1px solid var(--border, #e5e7eb);
  background: var(--bg-subtle, #f9fafb);
  color: var(--text-muted, #6b7280);
  padding: 0.25rem 0.625rem;
  border-radius: 999px;
  font-size: 0.75rem;
  cursor: pointer;
}
.if-bar__opt:hover { border-color: var(--brand, #2563eb); }
.if-bar__opt--on {
  background: var(--brand, #2563eb);
  color: white;
  border-color: var(--brand, #2563eb);
}

.if-bar__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem;
}

.if-bar__chip {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  background: color-mix(in srgb, var(--brand, #2563eb) 12%, transparent);
  color: var(--brand, #2563eb);
  font-size: 0.75rem;
  font-weight: 600;
  padding: 0.15rem 0.5rem 0.15rem 0.625rem;
  border-radius: 999px;
}

.if-bar__chip-x {
  background: none;
  border: none;
  color: inherit;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  opacity: 0.7;
}
.if-bar__chip-x:hover { opacity: 1; }

/* Mobile sheet */
.if-bar__mobile-toggle {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.5rem 0.875rem;
  border-radius: 8px;
  border: 1px solid var(--border, #e5e7eb);
  background: var(--bg-subtle, #f9fafb);
  font-weight: 600;
  cursor: pointer;
  font-size: 0.9rem;
  /* Touch target ≥ 40px high per PAI-471. */
  min-height: 40px;
}

.if-bar__backdrop {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.45);
  z-index: 1000;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  animation: if-bar-fade-in 160ms ease-out;
}

.if-bar__sheet {
  background: white;
  width: 100%;
  max-width: 540px;
  max-height: 85vh;
  overflow-y: auto;
  border-radius: 16px 16px 0 0;
  padding: 1rem 1.25rem 1.5rem;
  box-shadow: 0 -10px 30px rgba(15, 23, 42, 0.2);
  animation: if-bar-slide-up 220ms cubic-bezier(0.16, 1, 0.3, 1);
}

.if-bar__sheet-handle {
  width: 36px;
  height: 4px;
  background: var(--border, #e5e7eb);
  border-radius: 999px;
  margin: 0 auto 0.75rem;
}

.if-bar__sheet-head {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.75rem;
}
.if-bar__sheet-head h3 {
  font-size: 1.125rem;
  font-weight: 700;
  margin: 0;
  flex: 1;
}

.if-bar__sheet-close {
  border: none;
  background: transparent;
  cursor: pointer;
  color: var(--text-muted, #6b7280);
  /* Touch target ≥ 40px per PAI-471. */
  width: 40px;
  height: 40px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.if-bar__group { margin-bottom: 0.875rem; }
.if-bar__group h4 {
  font-size: 0.8125rem;
  font-weight: 600;
  margin: 0 0 0.4rem;
  color: var(--text-muted, #6b7280);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.if-bar__opt-row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}

@keyframes if-bar-slide-up {
  from { transform: translateY(100%); }
  to { transform: translateY(0); }
}
@keyframes if-bar-fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
</style>
