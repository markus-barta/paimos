# AI UX Surfaces

`PAI-206` inventory of host surfaces wired into the AI UX layer.

| Surface | File | Status |
|---|---|---|
| issue detail header | `frontend/src/views/IssueDetailView.vue` | live |
| issue detail description | `frontend/src/views/IssueDetailView.vue` | live |
| issue detail acceptance criteria | `frontend/src/views/IssueDetailView.vue` | live |
| issue detail notes | `frontend/src/views/IssueDetailView.vue` | live |
| issue side panel header | `frontend/src/components/IssueSidePanel.vue` | live |
| issue side panel description | `frontend/src/components/IssueSidePanel.vue` | live |
| issue side panel acceptance criteria | `frontend/src/components/IssueSidePanel.vue` | live |
| issue side panel notes | `frontend/src/components/IssueSidePanel.vue` | live |
| create issue description | `frontend/src/components/CreateIssueModal.vue` | live |
| create issue acceptance criteria | `frontend/src/components/CreateIssueModal.vue` | live |
| create issue notes | `frontend/src/components/CreateIssueModal.vue` | live |
| customer detail edit notes | `frontend/src/views/CustomerDetailView.vue` | live |
| customer create notes | `frontend/src/components/customer/CustomerCreateModal.vue` | live |
| cooperation SLA details | `frontend/src/components/customer/CooperationSection.vue` | live |
| cooperation notes | `frontend/src/components/customer/CooperationSection.vue` | live |
| project detail description | `frontend/src/views/ProjectDetailView.vue` | live |
| project create description | `frontend/src/views/ProjectsView.vue` | live |
| AI prompts dry-run preview | `frontend/src/components/settings/SettingsAIPromptsTab.vue` | live |

## Feedback model

- every host passes a stable `hostKey`
- activity, errors, and result overlays render only on the initiating host
- issue history shows issue-scoped AI calls from the backend paper trail
