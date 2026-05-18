<script setup lang="ts">
import type { Issue } from "@/types";

defineProps<{
  issue: Issue;
  descHtml: string;
  acHtml: string;
  notesHtml: string;
  // PAI-418 / PAI-422. Markdown-rendered HTML for the customer-facing
  // Projektbericht summary (shared field, two AI styles).
  reportSummaryHtml: string;
  isMonospace: boolean;
  mdMode: boolean;
}>();
</script>

<template>
  <div class="body-section">
    <div class="body-block">
      <p class="body-label">Description</p>
      <div
        v-if="issue.description"
        :class="[
          'body-text',
          { 'body-text--mono': isMonospace, 'md-rendered': mdMode },
        ]"
        v-html="descHtml"
      />
      <span v-else class="body-empty">—</span>
    </div>
    <div
      class="body-block"
      v-if="['epic', 'cost_unit', 'ticket'].includes(issue.type)"
    >
      <p class="body-label">Acceptance Criteria</p>
      <div
        v-if="issue.acceptance_criteria"
        :class="[
          'body-text',
          { 'body-text--mono': isMonospace, 'md-rendered': mdMode },
        ]"
        v-html="acHtml"
      />
      <span v-else class="body-empty">—</span>
    </div>
    <div class="body-block">
      <p class="body-label">Notes</p>
      <div
        v-if="issue.notes"
        :class="[
          'body-text',
          { 'body-text--mono': isMonospace, 'md-rendered': mdMode },
        ]"
        v-html="notesHtml"
      />
      <span v-else class="body-empty">—</span>
    </div>
    <div
      class="body-block"
      v-if="['epic', 'cost_unit', 'ticket'].includes(issue.type)"
    >
      <p class="body-label">
        Report summary
        <span class="body-label-hint">(Projektbericht · Kundenfassung)</span>
      </p>
      <div
        v-if="issue.report_summary"
        :class="[
          'body-text',
          { 'body-text--mono': isMonospace, 'md-rendered': mdMode },
        ]"
        v-html="reportSummaryHtml"
      />
      <span v-else class="body-empty">—</span>
    </div>
    <div
      v-if="
        !issue.description &&
        !issue.notes &&
        !issue.report_summary &&
        !(
          issue.acceptance_criteria &&
          ['epic', 'cost_unit', 'ticket'].includes(issue.type)
        )
      "
      class="body-empty"
    >
      No description or notes.
    </div>
  </div>
</template>

<style scoped>
.body-section {
  padding: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}
.body-label {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-muted);
  margin-bottom: 0.4rem;
}
.body-label-hint {
  font-weight: 500;
  text-transform: none;
  letter-spacing: 0;
  margin-left: 0.4em;
  color: var(--text-muted);
  opacity: 0.75;
}
.body-text {
  font-size: 14px;
  color: var(--text);
  line-height: 1.7;
  white-space: pre-wrap;
}
.body-empty {
  font-size: 13px;
  color: var(--text-muted);
  font-style: italic;
}
.body-text--mono {
  font-family: "DM Mono", "Menlo", monospace;
  font-size: 13px;
}
</style>
