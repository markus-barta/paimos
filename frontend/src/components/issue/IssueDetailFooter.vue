<script setup lang="ts">
import AppIcon from "@/components/AppIcon.vue";
import type { Issue } from "@/types";

defineProps<{
  issue: Issue;
  formatDateTime: (value: string) => string;
}>();

defineEmits<{
  history: [];
}>();
</script>

<template>
  <footer class="issue-footer">
    <span class="issue-footer-item">
      Last edited <strong>{{ formatDateTime(issue.updated_at) }}</strong>
      <template v-if="issue.last_changed_by_name">
        by {{ issue.last_changed_by_name }}</template
      >
    </span>
    <span class="issue-footer-sep">·</span>
    <span class="issue-footer-item">
      Created <strong>{{ formatDateTime(issue.created_at) }}</strong>
      <template v-if="issue.created_by_name">
        by {{ issue.created_by_name }}</template
      >
    </span>
    <template v-if="issue.assignee?.username">
      <span class="issue-footer-sep">·</span>
      <span class="issue-footer-item"
        >Assigned to <strong>{{ issue.assignee.username }}</strong></span
      >
    </template>
    <span class="issue-footer-spacer"></span>
    <button class="history-btn" @click="$emit('history')">
      <AppIcon name="history" :size="13" />
      History
    </button>
  </footer>
</template>

<style scoped>
.issue-footer {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
  padding: 1.25rem 0 0.5rem;
  margin-top: 2rem;
  border-top: 1px solid var(--border);
  font-size: 12px;
  color: var(--text-muted);
}
.issue-footer strong {
  color: var(--text);
  font-weight: 600;
}
.issue-footer-sep {
  color: var(--border);
}
.issue-footer-item {
  display: flex;
  align-items: center;
  gap: 0.3rem;
}
.issue-footer-spacer {
  flex: 1;
}
.history-btn {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  background: none;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 0.25rem 0.6rem;
  cursor: pointer;
  font-family: inherit;
  transition:
    color 0.12s,
    border-color 0.12s;
}
.history-btn:hover {
  color: var(--bp-blue);
  border-color: var(--bp-blue);
}
</style>
