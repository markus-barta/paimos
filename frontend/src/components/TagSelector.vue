<script setup lang="ts">
import { ref, computed } from 'vue'
import type { Tag } from '@/types'
import TagChip from './TagChip.vue'

const props = defineProps<{
  allTags: Tag[]
  selectedIds: number[]
  variant?: 'field' | 'pills'
  addLabel?: string
}>()

const emit = defineEmits<{
  add: [tagId: number]
  remove: [tagId: number]
}>()

const open = ref(false)
const search = ref('')
const inputRef = ref<HTMLInputElement | null>(null)
const root = ref<HTMLElement | null>(null)

const selected = computed(() =>
  props.allTags.filter(t => props.selectedIds.includes(t.id))
)

const available = computed(() =>
  props.allTags.filter(t =>
    !t.system &&
    !props.selectedIds.includes(t.id) &&
    t.name.toLowerCase().includes(search.value.toLowerCase())
  )
)

function toggle(tag: Tag) {
  if (props.selectedIds.includes(tag.id)) {
    emit('remove', tag.id)
  } else {
    emit('add', tag.id)
  }
}

function onBlur() {
  setTimeout(() => {
    if (root.value?.contains(document.activeElement)) return
    open.value = false
    search.value = ''
  }, 150)
}

function openPillPicker() {
  open.value = true
  setTimeout(() => inputRef.value?.focus(), 0)
}
</script>

<template>
  <div ref="root" :class="['tag-selector', { 'tag-selector--pills': variant === 'pills' }]">
    <!-- Selected chips -->
    <div class="selected-tags" v-if="selected.length || variant === 'pills'">
      <TagChip
        v-for="tag in selected"
        :key="tag.id"
        :tag="tag"
        removable
        @remove="emit('remove', $event)"
      />
      <div v-if="variant === 'pills'" class="tag-input-wrap tag-input-wrap--pills">
        <button type="button" class="tag-add-pill" @click="openPillPicker" @blur="onBlur">
          {{ addLabel ?? 'Add tag' }}
        </button>
        <div v-if="open" class="tag-dropdown tag-dropdown--pills">
          <input
            ref="inputRef"
            type="text"
            v-model="search"
            placeholder="Search tags…"
            class="tag-input tag-input--dropdown"
            @blur="onBlur"
            @keydown.escape="open = false"
          />
          <div v-if="available.length === 0" class="tag-empty">
            {{ search ? 'No matching tags' : 'All tags selected' }}
          </div>
          <button
            v-for="tag in available"
            :key="tag.id"
            class="tag-option"
            @mousedown.prevent="toggle(tag)"
          >
            <TagChip :tag="tag" />
          </button>
        </div>
      </div>
    </div>

    <!-- Dropdown trigger -->
    <div v-if="variant !== 'pills'" class="tag-input-wrap">
      <input
        type="text"
        v-model="search"
        placeholder="Add tag…"
        class="tag-input"
        @focus="open = true"
        @blur="onBlur"
      />
      <div v-if="open" class="tag-dropdown">
        <div v-if="available.length === 0" class="tag-empty">
          {{ search ? 'No matching tags' : 'All tags selected' }}
        </div>
        <button
          v-for="tag in available"
          :key="tag.id"
          class="tag-option"
          @mousedown.prevent="toggle(tag)"
        >
          <TagChip :tag="tag" />
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tag-selector { display: flex; flex-direction: column; gap: .5rem; }
.tag-selector--pills { gap: 0; }

.selected-tags {
  display: flex;
  flex-wrap: wrap;
  gap: .35rem;
  min-height: 0;
}

.tag-input-wrap { position: relative; }
.tag-input-wrap--pills { display: inline-flex; }

.tag-input {
  width: 100%;
  padding: .4rem .75rem;
  font-size: 13px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--bg-card);
  color: var(--text);
  outline: none;
}
.tag-input--dropdown {
  margin: .35rem .35rem .25rem;
  width: calc(100% - .7rem);
  padding: .35rem .55rem;
  font-size: 12px;
}
.tag-input:focus {
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px rgba(46,109,164,.12);
}

.tag-add-pill {
  display: inline-flex;
  align-items: center;
  gap: .25rem;
  min-height: 22px;
  padding: .15rem .55rem;
  border: 1px dashed var(--border);
  border-radius: 20px;
  background: transparent;
  color: var(--text-muted);
  font: inherit;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.6;
  cursor: pointer;
}
.tag-add-pill::before { content: '+'; font-weight: 700; }
.tag-add-pill:hover,
.tag-add-pill:focus-visible {
  color: var(--bp-blue-dark);
  border-color: var(--bp-blue);
  background: color-mix(in srgb, var(--bp-blue) 7%, transparent);
  outline: none;
}

.tag-dropdown {
  position: absolute;
  top: calc(100% + 4px);
  left: 0; right: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-md);
  z-index: 200;
  max-height: 200px;
  overflow-y: auto;
  padding: .35rem;
  display: flex;
  flex-direction: column;
  gap: .2rem;
}
.tag-dropdown--pills {
  top: calc(100% + 6px);
  left: 0;
  right: auto;
  width: min(260px, 72vw);
}

.tag-empty {
  padding: .5rem .75rem;
  font-size: 12px;
  color: var(--text-muted);
}

.tag-option {
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  padding: .3rem .5rem;
  border-radius: 4px;
  transition: background .1s;
}
.tag-option:hover { background: var(--bg); }
</style>
