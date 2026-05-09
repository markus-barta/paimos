<script setup lang="ts">
// PAI-343 — lesson-capture modal.
//
// Surfaces the guided memory-authoring form when a ticket transitions
// to a terminal status (done / delivered / cancelled) AND the
// server-side trigger detection (GET /api/issues/:id/lesson-capture-
// prompt) decided the ticket teaches a lesson. The user can:
//
//   - decline → the parent component closes the modal and proceeds
//     with the status transition normally (no memory created).
//   - accept → the form submits via POST /api/projects/:id/memory and
//     POST /api/issues/:ticket-id/relations (bidirectional link).
//
// The modal is opt-in by design. The ticket close happens whether the
// user fills out the form or not — capture is additive.

import { computed, ref, watch } from "vue";
import AppModal from "@/components/AppModal.vue";
import { errMsg } from "@/api/client";
import {
  submitLessonCapture,
  suggestMemorySlug,
  type MemoryType,
} from "@/services/lessonCapture";

const props = defineProps<{
  open: boolean;
  /** Project the memory will be created against. Required — the
   * convenience endpoint scopes everything by project_id. */
  projectId: number | null;
  /** Database id of the ticket being closed. */
  ticketId: number;
  /** Pretty key (e.g. "PAI-342") used as the originating_tickets
   * cross-link entry. */
  ticketKey?: string;
  /** Initial slug suggestion from the server's prompt endpoint —
   * editable by the user before submit. */
  suggestedName?: string;
  /** Human-readable explanation of why the prompt fired (shown as
   * a small note above the form). */
  reason?: string;
}>();

const emit = defineEmits<{
  /** User dismissed the modal — parent should close it and proceed
   * with the status transition. */
  close: [];
  /** Lesson was successfully captured — parent can show a toast +
   * route to the new memory. Payload mirrors the created memory id /
   * slug for routing. */
  saved: [{ memoryId: number; slug: string }];
}>();

const rule = ref("");
const why = ref("");
const how = ref("");
const memType = ref<MemoryType>("feedback");
const tagsRaw = ref("");
const slug = ref("");
const slugUserEdited = ref(false);
const submitting = ref(false);
const submitError = ref("");

const TYPE_OPTIONS: { value: MemoryType; label: string }[] = [
  { value: "feedback", label: "feedback — rule learned from working with the user" },
  { value: "project", label: "project — fact specific to this project" },
  { value: "reference", label: "reference — link to canonical docs / SOPs" },
];

const suggestedSlug = computed(() => suggestMemorySlug(memType.value, rule.value));

// Re-suggest the slug whenever the user changes type / rule, BUT
// only if they haven't manually edited the slug field. Once edited,
// we never overwrite their value.
watch([rule, memType], () => {
  if (!slugUserEdited.value) {
    slug.value = suggestedSlug.value;
  }
});

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      // Reset on each open so a previous decline doesn't bleed
      // half-typed text into the next ticket-close.
      rule.value = "";
      why.value = "";
      how.value = "";
      memType.value = "feedback";
      tagsRaw.value = "";
      slug.value = props.suggestedName || "feedback_lesson";
      slugUserEdited.value = false;
      submitError.value = "";
    }
  },
);

function onSlugInput(e: Event) {
  const v = (e.target as HTMLInputElement).value;
  slug.value = v;
  slugUserEdited.value = true;
}

const valid = computed(
  () =>
    rule.value.trim().length > 0 &&
    why.value.trim().length > 0 &&
    how.value.trim().length > 0 &&
    slug.value.trim().length > 0 &&
    !!props.projectId,
);

async function onSubmit() {
  if (!valid.value || !props.projectId) {
    submitError.value = "Rule, Why, How and slug are all required.";
    return;
  }
  submitting.value = true;
  submitError.value = "";
  try {
    const memory = await submitLessonCapture({
      projectId: props.projectId,
      ticketId: props.ticketId,
      ticketKey: props.ticketKey,
      slug: slug.value,
      rule: rule.value,
      why: why.value,
      how: how.value,
      type: memType.value,
      tags: tagsRaw.value
        .split(",")
        .map((t) => t.trim())
        .filter(Boolean),
    });
    emit("saved", { memoryId: memory.id, slug: memory.slug });
  } catch (e) {
    submitError.value = errMsg(e, "Failed to save memory.");
  } finally {
    submitting.value = false;
  }
}

function onDecline() {
  emit("close");
}
</script>

<template>
  <AppModal
    :open="open"
    title="Capture this lesson?"
    max-width="640px"
    @close="onDecline"
  >
    <div class="lc-body">
      <p class="lc-prompt">
        This ticket looks like it might teach a lesson worth saving as a
        memory. Capturing it now means future work picks up the rule
        automatically (PAI-342 ticket-memory linking).
      </p>
      <div v-if="reason" class="lc-reason">
        <span class="lc-reason-label">Triggered by:</span>
        <code class="lc-reason-value">{{ reason }}</code>
      </div>

      <div class="lc-field">
        <label>Rule (one sentence)</label>
        <input
          v-model="rule"
          type="text"
          placeholder="e.g. Use --line-buffered when piping log streams"
          maxlength="200"
        />
      </div>

      <div class="lc-field">
        <label>Why (the cause + reasoning)</label>
        <textarea
          v-model="why"
          rows="3"
          placeholder="Explain the root cause this rule guards against."
        />
      </div>

      <div class="lc-field">
        <label>How to apply (when to act on this)</label>
        <textarea
          v-model="how"
          rows="3"
          placeholder="Describe the situations where this rule applies."
        />
      </div>

      <div class="lc-row">
        <div class="lc-field" style="flex: 1">
          <label>Type</label>
          <select v-model="memType" class="v2-select">
            <option v-for="o in TYPE_OPTIONS" :key="o.value" :value="o.value">
              {{ o.label }}
            </option>
          </select>
        </div>
      </div>

      <div class="lc-field">
        <label>Tags (comma-separated, optional)</label>
        <input v-model="tagsRaw" type="text" placeholder="e.g. cli, logging, bug" />
      </div>

      <div class="lc-field">
        <label>Memory name (slug)</label>
        <input :value="slug" type="text" @input="onSlugInput" />
        <div class="lc-hint">
          Suggested: <code>{{ suggestedSlug }}</code>
        </div>
      </div>

      <div v-if="submitError" class="lc-error">{{ submitError }}</div>

      <div class="lc-actions">
        <button class="btn btn-ghost" type="button" @click="onDecline">
          No, just close the ticket
        </button>
        <button
          class="btn btn-primary"
          type="button"
          :disabled="!valid || submitting"
          @click="onSubmit"
        >
          {{ submitting ? "Saving…" : "Save as memory" }}
        </button>
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.lc-body { display: flex; flex-direction: column; gap: 1rem; }
.lc-prompt { font-size: 13px; color: var(--text-muted); line-height: 1.5; margin: 0; }
.lc-reason { font-size: 12px; color: var(--text-muted); display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
.lc-reason-label { font-weight: 600; }
.lc-reason-value { background: var(--surface-2); padding: 0.1rem 0.4rem; border-radius: 4px; font-size: 11px; }
.lc-field { display: flex; flex-direction: column; gap: 0.25rem; }
.lc-field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.04em; }
.lc-field input, .lc-field textarea, .lc-field select { font-size: 13px; padding: 0.4rem 0.6rem; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--text); font-family: inherit; }
.lc-field textarea { resize: vertical; min-height: 4rem; }
.lc-hint { font-size: 11px; color: var(--text-muted); }
.lc-row { display: flex; gap: 1rem; }
.lc-error { color: #b42318; font-size: 12px; background: #fef3f2; border: 1px solid #fecdca; border-radius: 6px; padding: 0.45rem 0.6rem; }
.lc-actions { display: flex; gap: 0.5rem; justify-content: flex-end; margin-top: 0.5rem; }
</style>
