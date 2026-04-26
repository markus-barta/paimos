<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import { useAiPhases, type AiPhase } from '@/composables/useAiPhases'

const props = defineProps<{
  actionKey: string
  title: string
  startedAt: number
  model?: string
  cancellable?: boolean
  failed?: boolean
  cancelled?: boolean
}>()

const emit = defineEmits<{ (e: 'cancel'): void }>()

const failed = computed(() => !!props.failed || !!props.cancelled)
const { t } = useI18n()
const actionKeyRef = toRef(props, 'actionKey')
const startedAtRef = toRef(props, 'startedAt')
const { elapsedMs, phase, narration } = useAiPhases(actionKeyRef, startedAtRef, failed)

const phaseText = computed(() => {
  if (props.cancelled) return t('ai.phase.cancelled')
  if (props.failed) return t('ai.phase.failed')
  return narration.value.label
})

const show = computed(() => phase.value !== 'pending' || failed.value)
const elapsedText = computed(() => `${(elapsedMs.value / 1000).toFixed(elapsedMs.value >= 10_000 ? 0 : 1)}s`)
const iconName = computed(() => (failed.value ? 'alert-circle' : phase.value === 'stalled' ? 'clock-3' : 'loader-circle'))
</script>

<template>
  <div v-if="show" class="aux-act-strip" :class="[`aux-act-strip--${phase as AiPhase}`]" role="status" aria-live="polite">
    <AppIcon :name="iconName" :size="13" :class="{ spin: !failed }" />
    <div class="aux-act-main">
      <div class="aux-act-head">
        <span class="aux-act-title">{{ title }}</span>
        <span class="aux-act-phase">{{ phaseText }}</span>
        <span class="aux-act-time">{{ elapsedText }}</span>
      </div>
      <div class="aux-act-meta">
        <span class="aux-act-key">{{ actionKey }}</span>
        <span v-if="model" class="aux-act-model">{{ model }}</span>
        <span v-if="phase === 'stalled'" class="aux-act-note">{{ t('ai.providerSlow') }}</span>
      </div>
      <div class="aux-act-sweep" aria-hidden="true"></div>
    </div>
    <button v-if="cancellable" type="button" class="aux-act-cancel" @click="emit('cancel')">
      {{ t('ai.dismiss') }}
    </button>
  </div>
</template>

<style scoped>
.aux-act-strip {
  display: flex;
  align-items: flex-start;
  gap: .65rem;
  padding: .7rem .85rem;
  border: 1px solid rgba(46, 109, 164, .22);
  border-radius: 10px;
  background:
    linear-gradient(180deg, rgba(220, 233, 244, .65), rgba(255,255,255,.95)),
    var(--bg-card);
  box-shadow: 0 8px 24px rgba(30, 50, 80, .08);
}
.aux-act-strip--failed,
.aux-act-strip--cancelled {
  border-color: rgba(192, 57, 43, .24);
  background: linear-gradient(180deg, rgba(253, 242, 242, .92), rgba(255,255,255,.98));
}
.aux-act-main {
  min-width: 0;
  flex: 1;
}
.aux-act-head,
.aux-act-meta {
  display: flex;
  align-items: center;
  gap: .45rem;
  flex-wrap: wrap;
}
.aux-act-title {
  font-family: "Bricolage Grotesque", "DM Sans", sans-serif;
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
}
.aux-act-phase,
.aux-act-time,
.aux-act-key,
.aux-act-model,
.aux-act-note {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 11px;
  color: var(--text-muted);
}
.aux-act-phase {
  padding: .12rem .35rem;
  border-radius: 999px;
  background: rgba(46, 109, 164, .1);
  color: var(--bp-blue-dark);
}
.aux-act-sweep {
  height: 2px;
  margin-top: .5rem;
  border-radius: 999px;
  background: linear-gradient(90deg, transparent, rgba(46, 109, 164, .65), transparent);
  background-size: 140px 2px;
  animation: aux-act-sweep 1.4s cubic-bezier(.2, .7, .1, 1) infinite;
}
.aux-act-cancel {
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: 12px;
}
@keyframes aux-act-sweep {
  from { background-position: -140px 0; }
  to { background-position: 140px 0; }
}
@media (prefers-reduced-motion: reduce) {
  .aux-act-sweep {
    animation: none;
  }
}
</style>
