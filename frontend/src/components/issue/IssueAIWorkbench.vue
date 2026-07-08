<script setup lang="ts">
import AppIcon from '@/components/AppIcon.vue'
import AgentRunPanel from '@/components/issue/AgentRunPanel.vue'
import IssueAiActivity from '@/components/issue/IssueAiActivity.vue'

withDefaults(defineProps<{
  issueId: number
  issueKey: string
  projectId: number
  issueType: string
  issueStatus: string
  issueTitle: string
  canEdit?: boolean
}>(), {
  canEdit: true,
})
</script>

<template>
  <section id="ai-workbench" class="issue-workbench" aria-label="AI Workbench">
    <div class="iw-head">
      <div class="iw-title">
        <AppIcon name="sparkles" :size="15" />
        <h3>AI Workbench</h3>
      </div>
      <div class="iw-context" aria-label="Selected issue context">
        <span class="iw-key">{{ issueKey }}</span>
        <span>{{ issueType }}</span>
        <span>{{ issueStatus }}</span>
        <span class="iw-context-title">{{ issueTitle }}</span>
      </div>
    </div>

    <AgentRunPanel :issue-id="issueId" :issue-key="issueKey" :project-id="projectId" :can-edit="canEdit !== false" />
    <IssueAiActivity :issue-id="issueId" start-open />
  </section>
</template>

<style scoped>
.issue-workbench {
  margin-top: 1.25rem;
  display: grid;
  gap: 0.75rem;
}
.iw-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.iw-title {
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
  min-width: 0;
}
.iw-title h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 800;
  color: var(--text);
}
.iw-context {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  min-width: 0;
  max-width: 100%;
  color: var(--text-muted);
  font-size: 12px;
}
.iw-context > span {
  min-height: 24px;
  display: inline-flex;
  align-items: center;
  max-width: 18rem;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0.12rem 0.45rem;
  background: var(--bg-card);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.iw-key {
  font-family: 'DM Mono', 'JetBrains Mono', monospace;
  font-weight: 700;
  color: var(--text);
}
.iw-context-title {
  color: var(--text);
}

@media (max-width: 760px) {
  .iw-head {
    align-items: flex-start;
  }
  .iw-context {
    width: 100%;
    flex-wrap: wrap;
  }
  .iw-context > span {
    max-width: 100%;
  }
}
</style>
