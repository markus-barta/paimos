<script setup lang="ts">
import { ref } from 'vue'
import AiActivityStrip from '@/components/ai/AiActivityStrip.vue'
import AiDecisionRow from '@/components/ai/AiDecisionRow.vue'
import AiResultStrip from '@/components/ai/AiResultStrip.vue'

const startedAt = ref(Date.now() - 1200)
</script>

<template>
  <div class="aiux-dev">
    <h1>AI UX reference</h1>
    <section class="aiux-card">
      <h2>Activity</h2>
      <AiActivityStrip action-key="spec_out" title="Acceptance Criteria" :started-at="startedAt" />
    </section>
    <section class="aiux-card">
      <h2>Result</h2>
      <AiResultStrip action-key="estimate_effort" title="Estimate effort" summary="6h · 1 LP suggested" details-label="Details">
        <template #decision>
          Apply the AI estimate to this issue?
        </template>
        <p>Model: anthropic/claude-sonnet-4.5 · 124 tokens</p>
      </AiResultStrip>
    </section>
    <section class="aiux-card">
      <h2>Decision</h2>
      <AiDecisionRow
        :primary="{ label: 'Apply', action: () => undefined }"
        :secondary="[{ label: 'Dismiss', action: () => undefined }]"
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
