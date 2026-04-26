<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import AiDecisionRow from '@/components/ai/AiDecisionRow.vue'

interface StripButton {
  label: string
  shortcut?: string
  action: () => void
}

const props = defineProps<{
  actionKey: string
  title: string
  summary: string
  detailsLabel?: string
  primary?: StripButton
  secondary?: StripButton[]
  explain?: StripButton
  dismissable?: boolean
  autoDismissMs?: number
}>()

const emit = defineEmits<{
  (e: 'dismiss'): void
  (e: 'primary'): void
}>()

const open = ref(false)
let timer: number | null = null

watch(() => props.summary, () => {
  open.value = false
  if (timer) {
    window.clearTimeout(timer)
    timer = null
  }
  if (props.autoDismissMs && props.dismissable) {
    timer = window.setTimeout(() => emit('dismiss'), props.autoDismissMs)
  }
}, { immediate: true })

const { t } = useI18n()
</script>

<template>
  <div class="aux-res-strip">
    <div class="aux-res-top">
      <div class="aux-res-copy">
        <div class="aux-res-head">
          <span class="aux-res-title">{{ title }}</span>
          <span class="aux-res-key">{{ actionKey }}</span>
        </div>
        <p class="aux-res-summary">{{ summary }}</p>
      </div>
      <div class="aux-res-controls">
        <button
          v-if="detailsLabel"
          type="button"
          class="btn btn-ghost aux-res-btn"
          @click="open = !open"
        >
          <AppIcon :name="open ? 'chevron-up' : 'chevron-down'" :size="12" />
          {{ detailsLabel }}
        </button>
        <button v-if="dismissable" type="button" class="btn btn-ghost aux-res-btn" @click="emit('dismiss')">
          {{ t('ai.dismiss') }}
        </button>
      </div>
    </div>

    <AiDecisionRow v-if="primary" :primary="primary" :secondary="secondary" :explain="explain" @decide="$emit('primary')">
      <slot name="decision" />
    </AiDecisionRow>

    <div v-if="open" class="aux-res-details">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.aux-res-strip {
  display: flex;
  flex-direction: column;
  gap: .55rem;
  padding: .8rem .9rem;
  border-radius: 10px;
  border: 1px solid rgba(46, 109, 164, .18);
  background: rgba(255,255,255,.94);
  box-shadow: 0 10px 24px rgba(30, 50, 80, .06);
}
.aux-res-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: .75rem;
}
.aux-res-copy {
  min-width: 0;
}
.aux-res-head {
  display: flex;
  align-items: center;
  gap: .5rem;
  flex-wrap: wrap;
}
.aux-res-title {
  font-family: "Bricolage Grotesque", "DM Sans", sans-serif;
  font-size: 13px;
  font-weight: 600;
}
.aux-res-key {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 11px;
  color: var(--text-muted);
}
.aux-res-summary {
  margin-top: .25rem;
  font-size: 13px;
  color: var(--text);
}
.aux-res-controls {
  display: flex;
  align-items: center;
  gap: .35rem;
}
.aux-res-btn {
  min-height: 32px;
  font-size: 12px;
}
.aux-res-details {
  padding-top: .35rem;
  border-top: 1px solid var(--border);
  font-size: 13px;
  color: var(--text);
}
</style>
