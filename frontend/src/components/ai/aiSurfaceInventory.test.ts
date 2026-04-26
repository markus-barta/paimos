import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

function read(rel: string): string {
  return readFileSync(resolve(process.cwd(), 'src', rel), 'utf8')
}

describe('AI surface inventory wiring', () => {
  it('keeps issue, customer, cooperation, project, and settings hosts wired to the shared AI UX components', () => {
    expect(read('views/IssueDetailView.vue')).toContain('AiSurfaceFeedback :host-key="`issue-detail:${issueId}:record`"')
    expect(read('views/IssueDetailView.vue')).toContain('AiSurfaceFeedback :host-key="`issue-detail:${issueId}:description`"')
    expect(read('views/IssueDetailView.vue')).toContain('AiSurfaceFeedback :host-key="`issue-detail:${issueId}:acceptance_criteria`"')
    expect(read('views/IssueDetailView.vue')).toContain('AiSurfaceFeedback :host-key="`issue-detail:${issueId}:notes`"')

    expect(read('components/IssueSidePanel.vue')).toContain('AiSurfaceFeedback :host-key="`issue-side:${issue?.id ?? 0}:record`"')
    expect(read('components/IssueSidePanel.vue')).toContain('AiSurfaceFeedback :host-key="`issue-side:${issue?.id ?? 0}:description`"')
    expect(read('components/IssueSidePanel.vue')).toContain('AiSurfaceFeedback :host-key="`issue-side:${issue?.id ?? 0}:acceptance_criteria`"')
    expect(read('components/IssueSidePanel.vue')).toContain('AiSurfaceFeedback :host-key="`issue-side:${issue?.id ?? 0}:notes`"')

    expect(read('components/CreateIssueModal.vue')).toContain('AiSurfaceFeedback host-key="create-issue:record"')
    expect(read('components/CreateIssueModal.vue')).toContain('AiSurfaceFeedback host-key="create-issue:description"')
    expect(read('components/CreateIssueModal.vue')).toContain('AiSurfaceFeedback host-key="create-issue:acceptance_criteria"')
    expect(read('components/CreateIssueModal.vue')).toContain('AiSurfaceFeedback host-key="create-issue:notes"')

    expect(read('views/CustomerDetailView.vue')).toContain('AiSurfaceFeedback host-key="customer-detail:notes" :apply="applyCustomerAiResult"')
    expect(read('components/customer/CustomerCreateModal.vue')).toContain('AiSurfaceFeedback host-key="customer-create:notes" :apply="applyCustomerAiResult"')
    expect(read('components/customer/CooperationSection.vue')).toContain('AiSurfaceFeedback host-key="cooperation:sla_details" :apply="applyCooperationAiResult"')
    expect(read('components/customer/CooperationSection.vue')).toContain('AiSurfaceFeedback host-key="cooperation:notes" :apply="applyCooperationAiResult"')

    expect(read('views/ProjectDetailView.vue')).toContain('AiSurfaceFeedback host-key="project-detail:description" :apply="applyProjectAiResult"')
    expect(read('views/ProjectsView.vue')).toContain('AiSurfaceFeedback host-key="projects-create:description" :apply="applyProjectCreateAiResult"')

    const promptsTab = read('components/settings/SettingsAIPromptsTab.vue')
    expect(promptsTab).toContain('AiActivityStrip')
    expect(promptsTab).toContain('AiResultStrip')
  })

  it('does not reintroduce the legacy optimize-button host path', () => {
    const files = [
      read('views/IssueDetailView.vue'),
      read('components/IssueSidePanel.vue'),
      read('components/CreateIssueModal.vue'),
      read('views/CustomerDetailView.vue'),
      read('components/customer/CustomerCreateModal.vue'),
      read('components/customer/CooperationSection.vue'),
      read('views/ProjectDetailView.vue'),
      read('views/ProjectsView.vue'),
      read('components/settings/SettingsAIPromptsTab.vue'),
    ]
    for (const file of files) {
      expect(file).not.toContain('AiOptimizeButton')
    }
  })
})
