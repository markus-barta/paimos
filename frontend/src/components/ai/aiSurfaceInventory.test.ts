import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

function read(rel: string): string {
  return readFileSync(resolve(process.cwd(), 'src', rel), 'utf8')
}

function expectHost(file: string, hostKey: string): void {
  const source = read(file)
  expect(source).toContain('AiSurfaceFeedback')
  expect(source).toContain(hostKey)
}

describe('AI surface inventory wiring', () => {
  it('keeps issue, customer, cooperation, project, and settings hosts wired to the shared AI UX components', () => {
    expectHost('views/IssueDetailView.vue', '`issue-detail:${issueId}:record`')
    expectHost('views/IssueDetailView.vue', '`issue-detail:${issueId}:description`')
    expectHost('views/IssueDetailView.vue', '`issue-detail:${issueId}:acceptance_criteria`')
    expectHost('views/IssueDetailView.vue', '`issue-detail:${issueId}:notes`')

    expectHost('components/IssueSidePanel.vue', '`issue-side:${issue.id}:record`')
    expectHost('components/IssueSidePanel.vue', '`issue-side:${issue?.id ?? 0}:description`')
    expectHost('components/IssueSidePanel.vue', '`issue-side:${issue?.id ?? 0}:acceptance_criteria`')
    expectHost('components/IssueSidePanel.vue', '`issue-side:${issue?.id ?? 0}:notes`')

    expectHost('components/CreateIssueModal.vue', 'host-key="create-issue:record"')
    expectHost('components/CreateIssueModal.vue', 'host-key="create-issue:description"')
    expectHost('components/CreateIssueModal.vue', 'host-key="create-issue:acceptance_criteria"')
    expectHost('components/CreateIssueModal.vue', 'host-key="create-issue:notes"')

    expectHost('views/CustomerDetailView.vue', 'host-key="customer-detail:notes"')
    expect(read('views/CustomerDetailView.vue')).toContain('applyCustomerAiResult')
    expectHost('components/customer/CustomerCreateModal.vue', 'host-key="customer-create:notes"')
    expect(read('components/customer/CustomerCreateModal.vue')).toContain('applyCustomerAiResult')
    expectHost('components/customer/CooperationSection.vue', 'host-key="cooperation:sla_details"')
    expectHost('components/customer/CooperationSection.vue', 'host-key="cooperation:notes"')
    expect(read('components/customer/CooperationSection.vue')).toContain('applyCooperationAiResult')

    expectHost('views/ProjectDetailView.vue', 'host-key="project-detail:description"')
    expect(read('views/ProjectDetailView.vue')).toContain('applyProjectAiResult')
    expectHost('views/ProjectsView.vue', 'host-key="projects-create:description"')
    expect(read('views/ProjectsView.vue')).toContain('applyProjectCreateAiResult')

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
