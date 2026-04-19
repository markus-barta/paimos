<script setup lang="ts">
import { ref, computed } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import AppModal from '@/components/AppModal.vue'
import changelogRaw from '@docs/CHANGELOG.md?raw'

defineProps<{ open: boolean }>()
defineEmits<{ close: [] }>()

const showAll = ref(false)

// Split changelog into per-version sections (split on ## headings)
const allSections = computed(() => {
  // Each section starts at a ## heading
  const parts = changelogRaw.split(/(?=^## v)/m).filter(s => s.trim())
  return parts
})

const visibleSections = computed(() =>
  showAll.value ? allSections.value : allSections.value.slice(0, 5)
)

const hiddenCount = computed(() => Math.max(0, allSections.value.length - 5))

// Render each section as HTML with marked
function renderSection(md: string): string {
  return DOMPurify.sanitize(marked.parse(md) as string)
}
</script>

<template>
  <AppModal
    title="What's new"
    :open="open"
    max-width="660px"
    @close="$emit('close')"
  >
    <div class="changelog">
      <div
        v-for="(section, i) in visibleSections"
        :key="i"
        class="changelog-section"
        v-html="renderSection(section)"
      />

      <div v-if="!showAll && hiddenCount > 0" class="changelog-expand">
        <button class="changelog-expand-btn" @click="showAll = true">
          Show {{ hiddenCount }} older release{{ hiddenCount !== 1 ? 's' : '' }}
        </button>
      </div>

      <div v-if="showAll && hiddenCount > 0" class="changelog-expand">
        <button class="changelog-expand-btn" @click="showAll = false">
          Show less
        </button>
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.changelog {
  padding: .25rem 0;
  max-height: 72vh;
  overflow-y: auto;
  padding-right: .5rem;
}

/* Section spacing */
.changelog-section { margin-bottom: 1.75rem; }
.changelog-section:last-of-type { margin-bottom: .5rem; }

/* Headings — ## becomes h2 */
.changelog-section :deep(h1) {
  font-size: 18px; font-weight: 800; color: var(--text);
  letter-spacing: -.02em; margin: 0 0 1.25rem;
}
.changelog-section :deep(h2) {
  font-size: 15px; font-weight: 700; color: var(--text);
  letter-spacing: -.01em; margin: 0 0 .5rem;
  padding-bottom: .35rem;
  border-bottom: 1px solid var(--border);
}
.changelog-section :deep(h3) {
  font-size: 13px; font-weight: 700; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .06em;
  margin: .75rem 0 .35rem;
}

/* Body text */
.changelog-section :deep(p) {
  font-size: 13px; color: var(--text); line-height: 1.6;
  margin: 0 0 .6rem;
}

/* Bullet lists */
.changelog-section :deep(ul) {
  margin: .25rem 0 .6rem 1rem; padding: 0;
  list-style: disc;
}
.changelog-section :deep(li) {
  font-size: 13px; color: var(--text); line-height: 1.55;
  margin-bottom: .2rem;
}

/* Inline code */
.changelog-section :deep(code) {
  font-family: 'DM Mono', 'Fira Code', monospace;
  font-size: 12px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 3px;
  padding: .05rem .35rem;
  color: var(--text);
}

/* Horizontal rule (---) */
.changelog-section :deep(hr) {
  display: none; /* section dividers handled by margin */
}

/* Strong in list items */
.changelog-section :deep(strong) {
  font-weight: 700; color: var(--text);
}

/* Expand / collapse */
.changelog-expand {
  text-align: center;
  padding: .5rem 0 .25rem;
}
.changelog-expand-btn {
  background: none; border: 1px solid var(--border);
  border-radius: 6px; padding: .35rem .9rem;
  font-size: 12px; font-weight: 600; color: var(--text-muted);
  cursor: pointer; font-family: inherit;
  transition: background .1s, color .1s;
}
.changelog-expand-btn:hover {
  background: var(--bg); color: var(--text);
}
</style>
