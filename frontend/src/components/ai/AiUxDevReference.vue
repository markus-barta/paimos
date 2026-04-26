<script setup lang="ts">
import { computed, ref } from 'vue'
import AiActivityStrip from '@/components/ai/AiActivityStrip.vue'
import AiDecisionRow from '@/components/ai/AiDecisionRow.vue'
import AiResultStrip from '@/components/ai/AiResultStrip.vue'

const activityState = ref<'pending' | 'working' | 'stalled' | 'failed' | 'cancelled'>('working')
const showDecision = ref(true)
const showDetails = ref(true)
const startedAt = computed(() => {
  const now = Date.now()
  if (activityState.value === 'pending') return now
  if (activityState.value === 'stalled') return now - 9000
  return now - 1800
})
</script>

<template>
  <div class="aiux-dev">
    <header class="aiux-head">
      <div>
        <h1>AI UX reference</h1>
        <p>Living visual reference for activity, result, and decision states.</p>
      </div>
      <div class="aiux-controls">
        <label>
          Activity
          <select v-model="activityState">
            <option value="pending">pending</option>
            <option value="working">working</option>
            <option value="stalled">stalled</option>
            <option value="failed">failed</option>
            <option value="cancelled">cancelled</option>
          </select>
        </label>
        <label><input v-model="showDecision" type="checkbox" /> decision row</label>
        <label><input v-model="showDetails" type="checkbox" /> details open</label>
      </div>
    </header>

    <section class="aiux-card">
      <h2>Activity</h2>
      <AiActivityStrip
        action-key="spec_out"
        title="Acceptance Criteria"
        :started-at="startedAt"
        :failed="activityState === 'failed'"
        :cancelled="activityState === 'cancelled'"
      />
    </section>

    <section class="aiux-card">
      <h2>Result</h2>
      <AiResultStrip
        action-key="estimate_effort"
        title="Estimate effort"
        summary="6h · 1 LP · medium"
        details-label="Details"
        :primary="showDecision ? { label: 'Apply', action: () => undefined } : undefined"
        :secondary="showDecision ? [{ label: 'Dismiss', action: () => undefined }] : undefined"
      >
        <template v-if="showDecision" #decision>
          Apply the AI estimate to this issue?
        </template>
        <div v-if="showDetails">
          <p>Dominant cost driver: frontend + backend validation plus one migration.</p>
        </div>
      </AiResultStrip>
    </section>

    <section class="aiux-card">
      <h2>Decision</h2>
      <AiDecisionRow
        :primary="{ label: 'Apply', shortcut: 'A', action: () => undefined }"
        :secondary="[{ label: 'Dismiss', shortcut: 'D', action: () => undefined }]"
        :explain="{ label: 'Explain', shortcut: 'E', action: () => undefined }"
      >
        Set PAI-83 as parent?
      </AiDecisionRow>
    </section>
  </div>
</template>

<style scoped>
.aiux-dev {
  max-width: 1100px;
  margin: 0 auto;
  padding: 2rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.aiux-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
}
.aiux-head h1 {
  margin: 0 0 .25rem;
}
.aiux-head p {
  margin: 0;
  color: var(--text-muted);
}
.aiux-controls {
  display: flex;
  gap: .75rem;
  flex-wrap: wrap;
}
.aiux-controls label {
  display: flex;
  align-items: center;
  gap: .35rem;
  font-size: 12px;
}
.aiux-controls select {
  min-height: 32px;
}
.aiux-card {
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--bg-card);
  display: flex;
  flex-direction: column;
  gap: .75rem;
}
.aiux-card h2 {
  font-size: 14px;
}
</style>
