<script setup lang="ts">
// MarkdownToolbar — MD / Text toggle.
//
// Default (no props): pill with border, used in comment compose header.
// subtle=true: plain text links "md · text", no border/background — used in meta row.
//
// Usage:
//   <MarkdownToolbar v-model="mdMode" />
//   <MarkdownToolbar v-model="mdMode" subtle />

const props = defineProps<{ modelValue: boolean; subtle?: boolean }>()
const emit  = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

function set(v: boolean) {
  if (props.modelValue !== v) emit('update:modelValue', v)
}
</script>

<template>
  <!-- Subtle variant: hairline segmented button -->
  <div v-if="subtle" class="md-subtle">
    <button
      type="button"
      :class="['md-subtle-btn', { 'md-subtle-btn--active': modelValue }]"
      @click="set(true)"
      title="Render as Markdown"
    >md</button>
    <button
      type="button"
      :class="['md-subtle-btn', { 'md-subtle-btn--active': !modelValue }]"
      @click="set(false)"
      title="Plain text"
    >text</button>
  </div>

  <!-- Default variant: pill with border -->
  <div v-else class="md-toolbar">
    <button
      type="button"
      :class="['md-btn', { active: modelValue }]"
      @click="set(true)"
      title="Render as Markdown"
    >MD</button>
    <button
      type="button"
      :class="['md-btn', { active: !modelValue }]"
      @click="set(false)"
      title="Plain text"
    >Text</button>
  </div>
</template>

<style scoped>
/* ── Default (pill) variant ───────────────────────────────────────────────── */
.md-toolbar {
  display: inline-flex;
  border: 1px solid var(--border);
  border-radius: 3px;
  overflow: hidden;
  flex-shrink: 0;
}
.md-btn {
  padding: .1rem .45rem;
  font-size: 10px;
  font-weight: 700;
  font-family: 'DM Mono', monospace;
  letter-spacing: .04em;
  background: transparent;
  color: var(--text-muted);
  border: none;
  cursor: pointer;
  transition: background .1s, color .1s;
  line-height: 1.7;
  user-select: none;
}
.md-btn + .md-btn { border-left: 1px solid var(--border); }
.md-btn.active { background: var(--bp-blue); color: #fff; }
.md-btn:hover:not(.active) { background: var(--bp-blue-pale); color: var(--bp-blue-dark); }

/* ── Subtle (hairline segmented) variant ─────────────────────────────────── */
.md-subtle {
  display: inline-flex;
  border: 1px solid var(--border);
  border-radius: 4px;
  overflow: hidden;
  flex-shrink: 0;
  align-self: flex-start;
}
.md-subtle-btn {
  background: none;
  border: none;
  padding: .15rem .45rem;
  font-size: 10px;
  font-weight: 600;
  font-family: 'DM Mono', monospace;
  color: var(--text-muted);
  cursor: pointer;
  user-select: none;
  line-height: 1.4;
  transition: background .1s, color .1s;
}
.md-subtle-btn + .md-subtle-btn { border-left: 1px solid var(--border); }
.md-subtle-btn--active {
  background: var(--bg);
  color: var(--text);
}
.md-subtle-btn:hover:not(.md-subtle-btn--active) {
  background: var(--bg);
}
</style>
