<script setup lang="ts">
import { ref } from "vue";
import UndoConflictModal from "@/components/undo/UndoConflictModal.vue";
import type { UndoConflictResponse } from "@/services/undoActivity";

const active = ref<UndoConflictResponse | null>(null);

const scenarios: Array<{
  key: string;
  label: string;
  conflict: UndoConflictResponse;
}> = [
  {
    key: "field",
    label: "Field changed by other",
    conflict: {
      status: "conflict",
      log_id: 1,
      request_id: "dev-field",
      mode: "undo",
      mutation_type: "issue.update",
      conflicts: [
        {
          pattern: "field-changed-by-other",
          field: "status",
          their_value: "qa",
          current_value: "qa",
          target_value: "backlog",
          options: [
            { id: "overwrite", label: "Use my target value", default: true },
            {
              id: "keep_theirs",
              label: "Keep the newer value",
              default: false,
            },
          ],
        },
      ],
      cascading_blockers: [],
    },
  },
  {
    key: "parent",
    label: "Parent deleted",
    conflict: {
      status: "conflict",
      log_id: 2,
      request_id: "dev-parent",
      mode: "undo",
      mutation_type: "issue.update",
      conflicts: [],
      cascading_blockers: [
        {
          pattern: "parent-deleted",
          target_id: 42,
          description: "Parent issue PAI-42 no longer exists in active state.",
          options: [
            { id: "orphan", label: "Make this issue top-level", default: true },
            { id: "cancel", label: "Cancel", default: false },
          ],
        },
      ],
    },
  },
  {
    key: "target",
    label: "Target deleted",
    conflict: {
      status: "conflict",
      log_id: 3,
      request_id: "dev-target",
      mode: "redo",
      mutation_type: "issue.relation.create",
      conflicts: [],
      cascading_blockers: [
        {
          pattern: "target-deleted",
          target_id: 73,
          description: "Target issue PAI-73 is no longer active.",
          options: [
            {
              id: "skip_relation",
              label: "Skip the relation change",
              default: true,
            },
            { id: "cancel", label: "Cancel", default: false },
          ],
        },
      ],
    },
  },
  {
    key: "sprint-closed",
    label: "Sprint closed",
    conflict: {
      status: "conflict",
      log_id: 4,
      request_id: "dev-sprint",
      mode: "undo",
      mutation_type: "issue.relation.create",
      conflicts: [],
      cascading_blockers: [
        {
          pattern: "sprint-closed",
          description: "Sprint PAI-S1 is closed and cannot accept the issue again.",
          options: [
            { id: "cancel", label: "Cancel", default: true },
            { id: "move_current", label: "Move to current sprint", default: false },
          ],
        },
      ],
    },
  },
  {
    key: "field-set-deleted",
    label: "Field set deleted",
    conflict: {
      status: "conflict",
      log_id: 5,
      request_id: "dev-field-set",
      mode: "undo",
      mutation_type: "issue.update",
      conflicts: [
        {
          pattern: "field-set-deleted",
          field: "cost_unit",
          their_value: "",
          current_value: "",
          target_value: "Q1",
          options: [
            { id: "clear_field", label: "Clear the field", default: true },
            { id: "cancel", label: "Cancel", default: false },
          ],
        },
      ],
      cascading_blockers: [],
    },
  },
  {
    key: "bulk-children-modified",
    label: "Bulk children modified",
    conflict: {
      status: "conflict",
      log_id: 6,
      request_id: "dev-bulk",
      mode: "undo",
      mutation_type: "bulk.complete_epic",
      conflicts: [
        {
          pattern: "bulk-children-modified",
          field: "children",
          their_value: "7 drifted children",
          current_value: "7 drifted children",
          target_value: "Re-open 23 children",
          options: [
            { id: "revert_only_unmodified", label: "Revert only unmodified", default: true },
            { id: "revert_all", label: "Revert all", default: false },
            { id: "cancel", label: "Cancel", default: false },
          ],
        },
      ],
      cascading_blockers: [],
    },
  },
  {
    key: "time-entry-invoiced",
    label: "Time entry invoiced",
    conflict: {
      status: "conflict",
      log_id: 7,
      request_id: "dev-locked",
      mode: "undo",
      mutation_type: "time.update",
      conflicts: [
        {
          pattern: "time-entry-invoiced",
          field: "invoice_state",
          their_value: "invoiced",
          current_value: "invoiced",
          target_value: "editable",
          options: [
            { id: "cancel", label: "Irreversible after invoice", default: true },
          ],
        },
      ],
      cascading_blockers: [],
    },
  },
  {
    key: "permission-revoked",
    label: "Permission revoked",
    conflict: {
      status: "conflict",
      log_id: 8,
      request_id: "dev-forbidden",
      mode: "undo",
      mutation_type: "project.update",
      conflicts: [
        {
          pattern: "permission-revoked",
          field: "access",
          their_value: "viewer",
          current_value: "viewer",
          target_value: "editor",
          options: [
            { id: "cancel", label: "Re-acquire permission first", default: true },
          ],
        },
      ],
      cascading_blockers: [],
    },
  },
];
</script>

<template>
  <main class="undo-dev">
    <header class="undo-dev__hero">
      <span class="undo-dev__eyebrow">PAI-210 / PAI-215</span>
      <h1>Undo reference</h1>
      <p>
        Conflict shapes, conservative defaults, and the modal posture in one
        place.
      </p>
    </header>

    <section class="undo-dev__grid">
      <button
        v-for="scenario in scenarios"
        :key="scenario.key"
        class="undo-dev__card"
        @click="active = scenario.conflict"
      >
        <strong>{{ scenario.label }}</strong>
        <span
          >{{ scenario.conflict.mode }} ·
          {{ scenario.conflict.mutation_type }}</span
        >
      </button>
    </section>

    <UndoConflictModal
      :conflict="active"
      @cancel="active = null"
      @apply="active = null"
    />
  </main>
</template>

<style scoped>
.undo-dev {
  min-height: 100vh;
  padding: 2.5rem;
  background:
    radial-gradient(
      circle at top left,
      rgba(46, 109, 164, 0.12),
      transparent 32%
    ),
    linear-gradient(180deg, #f5f8fb, #eef3f8);
}
.undo-dev__hero {
  max-width: 720px;
  margin-bottom: 1.6rem;
}
.undo-dev__eyebrow {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-muted);
}
.undo-dev__hero h1 {
  font-family: "Bricolage Grotesque", serif;
  font-size: 2.2rem;
}
.undo-dev__hero p {
  color: var(--text-muted);
}
.undo-dev__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 1rem;
}
.undo-dev__card {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  text-align: left;
  padding: 1.15rem;
  border-radius: 18px;
  border: 1px solid rgba(46, 109, 164, 0.14);
  background: rgba(255, 255, 255, 0.9);
  box-shadow: 0 18px 44px rgba(30, 50, 80, 0.08);
}
.undo-dev__card span {
  color: var(--text-muted);
  font-size: 13px;
}
</style>
