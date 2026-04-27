<script setup lang="ts">
/**
 * MetaSelect — a custom select that renders visual indicators (dots, SVGs, colored text)
 * alongside option labels. Replaces native <select> for status/priority/type fields.
 *
 * Props:
 *   modelValue  — currently selected value
 *   options     — array of { value, label, icon?, dotColor?, dotOutline?, arrowColor? }
 *   placeholder — shown when no value selected (empty string value)
 */
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue'
import AppIcon from '@/components/AppIcon.vue'

export interface MetaOption {
  value: string
  label: string
  /** Raw SVG string — rendered via v-html */
  icon?: string
  /** Dot color (hex/var). If set, renders a status dot */
  dotColor?: string
  /** If true, dot is outline-only (no fill) */
  dotOutline?: boolean
  /** If set, label + arrow use this color */
  arrowColor?: string
  /** Arrow character (↑ → ↓) */
  arrow?: string
  /** Lucide icon name — rendered as <AppIcon> */
  iconName?: string
}

const props = defineProps<{
  modelValue: string
  options: MetaOption[]
  placeholder?: string
  searchable?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const searchQuery = ref('')
const searchInput = ref<HTMLInputElement | null>(null)
const dropdownPos = ref({ top: '0px', left: '0px', minWidth: '0px', maxHeight: '' })

const selected = computed(() =>
  props.options.find(o => o.value === props.modelValue) ?? null
)

const filteredOptions = computed(() => {
  if (!props.searchable || !searchQuery.value.trim()) return props.options
  const q = searchQuery.value.toLowerCase()
  return props.options.filter(o => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q))
})

function select(value: string) {
  emit('update:modelValue', value)
  open.value = false
  searchQuery.value = ''
}

function toggleOpen() {
  open.value = !open.value
  if (open.value) {
    if (root.value) {
      const rect = root.value.getBoundingClientRect()
      // PAI-243: flip the dropdown above the trigger when there isn't
      // enough room below it (e.g. status/priority cells in the bottom
      // rows of a long table). Estimated panel height is the search row
      // (~36px) plus the options scroll cap (240px) plus border/padding.
      const margin = 8
      const estimatedHeight = (props.searchable ? 36 : 0) + 240 + 12
      const spaceBelow = window.innerHeight - rect.bottom - margin
      const spaceAbove = rect.top - margin
      const flipUp = spaceBelow < estimatedHeight && spaceAbove > spaceBelow
      const usable = Math.max(120, Math.floor((flipUp ? spaceAbove : spaceBelow)))
      dropdownPos.value = {
        top: flipUp
          ? Math.max(margin, rect.top - 4 - Math.min(estimatedHeight, usable)) + 'px'
          : rect.bottom + 4 + 'px',
        left: rect.left + 'px',
        minWidth: Math.max(rect.width, 140) + 'px',
        maxHeight: usable + 'px',
      }
    }
    if (props.searchable) {
      searchQuery.value = ''
      nextTick(() => searchInput.value?.focus())
    }
  }
}

function onOutsideClick(e: MouseEvent) {
  const target = e.target as Node
  if (root.value && !root.value.contains(target)) {
    // Also check the teleported dropdown
    const dd = document.querySelector('.meta-select-dropdown--teleported')
    if (dd && dd.contains(target)) return
    open.value = false
  }
}

onMounted(() => document.addEventListener('mousedown', onOutsideClick))
onUnmounted(() => document.removeEventListener('mousedown', onOutsideClick))
</script>

<template>
  <div class="meta-select" :class="{ open }" ref="root">
    <button type="button" class="meta-select-trigger" @click="toggleOpen">
      <!-- Selected value display -->
        <span v-if="selected" class="meta-select-val">
          <span v-if="selected.dotColor"
            class="ms-dot"
            :class="{ 'ms-dot--outline': selected.dotOutline }"
            :style="selected.dotOutline
              ? { borderColor: selected.dotColor }
              : { background: selected.dotColor }"
          ></span>
          <span v-if="selected.icon" class="ms-icon" v-html="selected.icon"></span>
          <span class="ms-label" :style="selected.arrowColor ? { color: selected.arrowColor } : {}">
            <AppIcon v-if="selected.arrow" :name="selected.arrow" :size="12" :stroke-width="2.5" class="ms-arrow-icon" :style="{ color: selected.arrowColor }" />
            {{ selected.label }}
          </span>
        </span>
      <span v-else class="meta-select-placeholder">{{ placeholder ?? 'All' }}</span>
      <span class="meta-select-chevron"><AppIcon name="chevron-down" :size="12" /></span>
    </button>

    <Teleport to="body">
    <div v-if="open" class="meta-select-dropdown meta-select-dropdown--teleported" :style="{ top: dropdownPos.top, left: dropdownPos.left, minWidth: dropdownPos.minWidth, maxHeight: dropdownPos.maxHeight }">
      <!-- Search input -->
      <div v-if="searchable" class="ms-search-wrap">
        <input ref="searchInput" v-model="searchQuery" type="text" class="ms-search" placeholder="Search…" autocomplete="off" @keydown.escape="open = false" />
      </div>

      <!-- Placeholder / "All" option -->
      <div class="ms-options-scroll">
      <button
        v-if="placeholder !== undefined && !searchQuery"
        type="button"
        class="ms-option"
        :class="{ active: modelValue === '' }"
        @click="select('')"
      >
        <span class="ms-label muted">{{ placeholder }}</span>
      </button>

      <button
        v-for="opt in filteredOptions"
        :key="opt.value"
        type="button"
        class="ms-option"
        :class="{ active: modelValue === opt.value }"
        @click="select(opt.value)"
      >
        <span v-if="opt.dotColor"
          class="ms-dot"
          :class="{ 'ms-dot--outline': opt.dotOutline }"
          :style="opt.dotOutline
            ? { borderColor: opt.dotColor }
            : { background: opt.dotColor }"
        ></span>
        <span v-if="opt.icon" class="ms-icon" v-html="opt.icon"></span>
        <span class="ms-label" :style="opt.arrowColor ? { color: opt.arrowColor } : {}">
          <AppIcon v-if="opt.arrow" :name="opt.arrow" :size="12" :stroke-width="2.5" class="ms-arrow-icon" :style="{ color: opt.arrowColor }" />
          {{ opt.label }}
        </span>
      </button>

      <div v-if="searchable && searchQuery && !filteredOptions.length" class="ms-no-results">No matches</div>
      </div>
    </div>
    </Teleport>
  </div>
</template>

<style scoped>
.meta-select {
  position: relative;
  display: inline-flex;
  max-width: 100%;
}

.meta-select-trigger {
  display: inline-flex;
  align-items: center;
  gap: .4rem;
  padding: .4rem .6rem;
  font-size: 12px;
  font-weight: 500;
  line-height: 1.3;
  color: var(--text);
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  transition: border-color .12s, background .12s;
  font-family: inherit;
  min-width: 90px;
  max-width: 100%;
  justify-content: space-between;
}
.meta-select-trigger:hover,
.meta-select.open .meta-select-trigger {
  border-color: var(--bp-blue);
  background: var(--bg);
}

.meta-select-placeholder { color: var(--text-muted); }
.meta-select-chevron { font-size: 10px; color: var(--text-muted); margin-left: .2rem; }

.meta-select-val {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  line-height: 1.3;
  overflow: hidden;
  min-width: 0;
}

.meta-select-dropdown {
  position: fixed;
  z-index: 9000;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-md);
  min-width: 140px;
  max-width: 320px;
  padding: .25rem 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.ms-search-wrap { padding: .3rem .5rem; flex: 0 0 auto; }
.ms-search {
  width: 100%; box-sizing: border-box; border: 1px solid var(--border); border-radius: 4px;
  padding: .3rem .5rem; font-size: 12px; font-family: inherit; outline: none;
  background: var(--bg);
}
.ms-search:focus { border-color: var(--bp-blue); }
.ms-options-scroll { flex: 1 1 auto; min-height: 0; max-height: 240px; overflow-y: auto; }
.ms-no-results { padding: .4rem .75rem; font-size: 11px; color: var(--text-muted); }

.ms-option {
  display: flex;
  align-items: center;
  gap: .4rem;
  padding: .45rem .75rem;
  font-size: 12px;
  font-weight: 500;
  line-height: 1.3;
  color: var(--text);
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  font-family: inherit;
  white-space: nowrap;
  width: 100%;
  transition: background .08s;
}
.ms-option:hover { background: var(--bg); }
.ms-option.active { background: var(--bp-blue-pale); }

/* Dot indicator */
.ms-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  display: inline-block;
}
.ms-dot--outline {
  background: transparent !important;
  border: 2px solid;
}

/* SVG icon */
.ms-icon {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  line-height: 1;
}
.ms-icon :deep(svg) { display: block; }

/* Label + arrow */
.ms-label {
  display: inline-flex;
  align-items: center;
  gap: .2rem;
  line-height: 1.3;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}
.ms-label.muted { color: var(--text-muted); }
.ms-arrow-icon { flex-shrink: 0; }
</style>
