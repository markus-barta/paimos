/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

export default {
  portal: {
    title: 'Customer Portal',
    logout: 'Logout',
    search: 'Search issues…',
    yourProjects: 'Your Projects',
    loading: 'Loading…',
    noProjects: 'No projects assigned yet.',
    issues: 'Issues',
    done: 'Done',
    allProjects: '← All Projects',
    newRequest: '+ New Request',
    tabs: {
      all: 'All',
      open: 'In Progress',
      review: 'Ready for Review',
      accepted: 'Accepted',
    },
    summary: {
      total: 'Total',
      estimate: 'Estimate',
      arCost: 'AR Cost',
      report: 'Acceptance Report',
    },
    reject: 'Reject',
    invoicedLabel: 'Invoiced',
    filters: {
      allStatus: 'All Status',
      allTypes: 'All Types',
    },
    table: {
      key: 'Key',
      title: 'Title',
      type: 'Type',
      status: 'Status',
      priority: 'Priority',
      estimate: 'Estimate',
      ar: 'AR',
      accepted: 'Accepted',
    },
    noIssues: 'No issues found.',
    accept: 'Accept',
    acceptedLabel: 'Accepted',
    requestModal: {
      title: 'New Request',
      titleField: 'Title',
      description: 'Description',
      cancel: 'Cancel',
      submit: 'Submit Request',
      submitting: 'Submitting…',
    },
    issueDetail: {
      back: '← Back to issues',
      description: 'Description',
      acceptanceCriteria: 'Acceptance Criteria',
      status: 'Status',
      priority: 'Priority',
      type: 'Type',
      created: 'Created',
      updated: 'Updated',
    },
  },
  status: {
    new: 'New',
    backlog: 'Backlog',
    'in-progress': 'In Progress',
    qa: 'QA',
    done: 'Done',
    delivered: 'Delivered',
    accepted: 'Accepted',
    invoiced: 'Invoiced',
    cancelled: 'Cancelled',
  },
  ai: {
    phase: {
      pending: 'Queued',
      working: 'Working',
      stalled: 'Stalled',
      failed: 'Failed',
      cancelled: 'Cancelled',
    },
    phaseScript: {
      optimize: { reading: 'Reading your text', composing: 'Composing a tighter draft', refining: 'Refining for tone consistency' },
      optimize_customer: { reading: 'Reading your text', composing: 'Composing a tighter draft', refining: 'Refining for tone consistency' },
      translate: { reading: 'Reading your source', translating: 'Translating the draft', polishing: 'Polishing the phrasing' },
      tone_check: { reading: 'Reading your wording', screening: 'Screening persuasive phrasing', softening: 'Softening the tone' },
      suggest_enhancement: { reading: 'Reading the issue', probing: 'Looking for improvement gaps', grouping: 'Grouping concrete ideas' },
      spec_out: { reading: 'Reading the issue', structuring: 'Structuring acceptance criteria', tightening: 'Tightening the checklist' },
      find_parent: { reading: 'Reading the issue', scanning: 'Scanning the project tree', ranking: 'Ranking parent candidates' },
      generate_subtasks: { reading: 'Reading the issue', sequencing: 'Sequencing the work', sizing: 'Sizing the sub-tasks' },
      estimate_effort: { reading: 'Reading scope and AC', comparing: 'Comparing similar issues', weighing: 'Weighing complexity' },
      detect_duplicates: { reading: 'Reading the issue', matching: 'Matching similar issues', ranking: 'Ranking likely duplicates' },
      ui_generation: { reading: 'Reading the request', drafting: 'Drafting the UI spec', formatting: 'Formatting the output' },
    },
    providerSlow: 'Provider taking longer than usual',
    dismiss: 'Dismiss',
    apply: 'Apply',
    details: 'Details',
    workingTitle: '{action} in progress',
    resultTitle: '{action} ready',
    failedPrefix: 'AI failed',
    modelLabel: 'Model',
    tokensLabel: 'Tokens',
    detailsHint: 'Open the result modal to inspect and apply the full payload.',
    setAsParent: 'Set the top suggestion as parent ({issueKey})?',
    applyEstimate: 'Apply this estimate to the issue?',
    showReasoning: 'Show reasoning',
    linkAsRelated: 'Link the top match to this issue ({issueKey})?',
    linkRelated: 'Link as related',
    linkBlocks: 'Blocks',
    linkDependsOn: 'Depends on',
    moreRelations: 'More relations',
    undoTitle: 'Change applied',
    undoReady: 'You can undo this AI-applied change for a short time.',
    undo: 'Undo',
  },
}
