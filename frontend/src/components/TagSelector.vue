<script setup lang="ts">
import { ref, computed } from 'vue'
import type { Tag } from '@/types'
import TagChip from './TagChip.vue'

const props = defineProps<{
  allTags: Tag[]
  selectedIds: number[]
}>()

const emit = defineEmits<{
  add: [tagId: number]
  remove: [tagId: number]
}>()

const open = ref(false)
const search = ref('')

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
  setTimeout(() => { open.value = false; search.value = '' }, 150)
}
</script>

<template>
  <div class="tag-selector">
    <!-- Selected chips -->
    <div class="selected-tags" v-if="selected.length">
      <TagChip
        v-for="tag in selected"
        :key="tag.id"
        :tag="tag"
        removable
        @remove="emit('remove', $event)"
      />
    </div>

    <!-- Dropdown trigger -->
    <div class="tag-input-wrap">
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

.selected-tags {
  display: flex;
  flex-wrap: wrap;
  gap: .35rem;
  min-height: 0;
}

.tag-input-wrap { position: relative; }

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
.tag-input:focus {
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px rgba(46,109,164,.12);
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
