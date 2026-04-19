<script setup lang="ts">
import type { Tag } from '@/types'
import AppIcon from '@/components/AppIcon.vue'

defineProps<{
  tag: Tag
  removable?: boolean
}>()
defineEmits<{ remove: [id: number] }>()
</script>

<template>
  <span :class="['tag-chip', `tag-${tag.color}`, { 'tag-system': tag.system }]">
    {{ tag.name }}
    <button v-if="removable" class="tag-remove" @click.stop="$emit('remove', tag.id)" aria-label="Remove tag">
      <AppIcon name="x" :size="11" :stroke-width="2.5" />
    </button>
  </span>
</template>

<style scoped>
.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: .25rem;
  padding: .15rem .55rem;
  border-radius: 20px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: .02em;
  white-space: nowrap;
  line-height: 1.6;
}

.tag-remove {
  background: none;
  border: none;
  padding: 0;
  margin-left: .1rem;
  cursor: pointer;
  font-size: 13px;
  line-height: 1;
  opacity: .6;
  color: inherit;
}
.tag-remove:hover { opacity: 1; }

/* Color palette */
.tag-gray   { background: #e9ecef; color: #495057; }
.tag-slate  { background: #e2e8f0; color: #334155; }
.tag-blue   { background: #dbeafe; color: #1e40af; }
.tag-indigo { background: #e0e7ff; color: #3730a3; }
.tag-purple { background: #ede9fe; color: #5b21b6; }
.tag-pink   { background: #fce7f3; color: #9d174d; }
.tag-red    { background: #fee2e2; color: #991b1b; }
.tag-orange { background: #ffedd5; color: #9a3412; }
.tag-yellow { background: #fef9c3; color: #854d0e; }
.tag-green  { background: #dcfce7; color: #166534; }
.tag-teal   { background: #ccfbf1; color: #134e4a; }
.tag-cyan   { background: #cffafe; color: #164e63; }

/* System tags — dashed border, muted background */
.tag-system {
  border: 1px dashed currentColor;
  background: transparent !important;
  opacity: .85;
}
</style>
