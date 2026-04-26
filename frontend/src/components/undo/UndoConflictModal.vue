<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import { useI18n } from "vue-i18n";
import AppModal from "@/components/AppModal.vue";
import AppIcon from "@/components/AppIcon.vue";
import type {
  UndoConflictResponse,
  UndoResolutionPayload,
} from "@/services/undoActivity";

const props = defineProps<{
  conflict: UndoConflictResponse | null;
  loading?: boolean;
}>();

const emit = defineEmits<{
  (e: "apply", payload: UndoResolutionPayload): void;
  (e: "cancel"): void;
}>();

const { t } = useI18n();

const fieldChoices = reactive<Record<string, string>>({});
const cascadeChoices = reactive<Record<string, string>>({});

watch(
  () => props.conflict,
  (next) => {
    for (const key of Object.keys(fieldChoices)) delete fieldChoices[key];
    for (const key of Object.keys(cascadeChoices)) delete cascadeChoices[key];
    if (!next) return;
    for (const item of next.conflicts) {
      fieldChoices[item.field] =
        item.options.find((option) => option.default)?.id ??
        item.options[0]?.id ??
        "";
    }
    for (const item of next.cascading_blockers) {
      cascadeChoices[item.pattern] =
        item.options.find((option) => option.default)?.id ??
        item.options[0]?.id ??
        "";
    }
  },
  { immediate: true },
);

const open = computed(() => !!props.conflict);
const title = computed(() => {
  if (!props.conflict) return t("undo.conflict.fallbackTitle");
  return props.conflict.mode === "redo"
    ? t("undo.conflict.titleRedo")
    : t("undo.conflict.titleUndo");
});

function apply() {
  emit("apply", {
    field_choices: { ...fieldChoices },
    cascade_choices: { ...cascadeChoices },
  });
}
</script>

<template>
  <AppModal :open="open" :title="title" @close="$emit('cancel')">
    <section v-if="conflict" class="undo-modal">
      <header class="undo-modal__hero">
        <div class="undo-modal__eyebrow">
          <AppIcon name="rewind" :size="13" />
          <span>{{ conflict.mutation_type }}</span>
        </div>
        <h3>
          {{
            conflict.mode === "redo"
              ? t("undo.conflict.heroRedo")
              : t("undo.conflict.heroUndo")
          }}
        </h3>
        <p>
          {{ t("undo.conflict.heroBody") }}
        </p>
      </header>

      <div v-if="conflict.conflicts.length" class="undo-modal__section">
        <div class="undo-modal__section-head">
          {{ t("undo.conflict.fieldHeader") }}
        </div>
        <article
          v-for="item in conflict.conflicts"
          :key="item.field"
          class="undo-card"
        >
          <div class="undo-card__field">{{ item.field }}</div>
          <div class="undo-card__values">
            <div>
              <span>{{ t("undo.conflict.current") }}</span
              ><strong>{{ item.current_value ?? "—" }}</strong>
            </div>
            <div>
              <span>{{ t("undo.conflict.target") }}</span
              ><strong>{{ item.target_value ?? "—" }}</strong>
            </div>
          </div>
          <div class="undo-card__choices">
            <label
              v-for="option in item.options"
              :key="option.id"
              class="undo-choice"
            >
              <input
                v-model="fieldChoices[item.field]"
                type="radio"
                :name="`field-${item.field}`"
                :value="option.id"
              />
              <span>{{ option.label }}</span>
            </label>
          </div>
        </article>
      </div>

      <div
        v-if="conflict.cascading_blockers.length"
        class="undo-modal__section"
      >
        <div class="undo-modal__section-head">
          {{ t("undo.conflict.cascadeHeader") }}
        </div>
        <article
          v-for="item in conflict.cascading_blockers"
          :key="`${item.pattern}-${item.target_id ?? 0}`"
          class="undo-card undo-card--blocker"
        >
          <div class="undo-card__field">{{ item.pattern }}</div>
          <p class="undo-card__desc">{{ item.description }}</p>
          <div class="undo-card__choices">
            <label
              v-for="option in item.options"
              :key="option.id"
              class="undo-choice"
            >
              <input
                v-model="cascadeChoices[item.pattern]"
                type="radio"
                :name="`cascade-${item.pattern}`"
                :value="option.id"
              />
              <span>{{ option.label }}</span>
            </label>
          </div>
        </article>
      </div>

      <footer class="undo-modal__footer">
        <button type="button" class="btn btn-ghost" @click="$emit('cancel')">
          {{ t("undo.conflict.cancel") }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="loading"
          @click="apply"
        >
          {{
            loading
              ? t("undo.conflict.applying")
              : t("undo.conflict.applyWithSelections")
          }}
        </button>
      </footer>
    </section>
  </AppModal>
</template>

<style scoped>
.undo-modal {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.undo-modal__hero {
  padding: 1rem 1.05rem;
  border-radius: 18px;
  background:
    radial-gradient(
      circle at top right,
      rgba(46, 109, 164, 0.18),
      transparent 46%
    ),
    linear-gradient(
      180deg,
      rgba(220, 233, 244, 0.72),
      rgba(255, 255, 255, 0.96)
    );
  border: 1px solid rgba(46, 109, 164, 0.15);
}
.undo-modal__eyebrow,
.undo-card__field,
.undo-card__values span {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 11px;
  color: var(--text-muted);
}
.undo-modal__eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  margin-bottom: 0.35rem;
}
.undo-modal__hero h3 {
  font-family: "Bricolage Grotesque", serif;
  font-size: 1.15rem;
  margin-bottom: 0.2rem;
}
.undo-modal__hero p {
  color: var(--text-muted);
}
.undo-modal__section {
  display: flex;
  flex-direction: column;
  gap: 0.65rem;
}
.undo-modal__section-head {
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-muted);
}
.undo-card {
  border: 1px solid var(--border);
  border-radius: 16px;
  padding: 0.95rem 1rem;
  background: var(--bg-card);
  box-shadow: 0 10px 30px rgba(26, 38, 54, 0.05);
}
.undo-card--blocker {
  background: linear-gradient(
    180deg,
    rgba(242, 245, 248, 0.95),
    rgba(255, 255, 255, 0.98)
  );
}
.undo-card__values {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.6rem;
  margin: 0.6rem 0;
}
.undo-card__values strong {
  display: block;
  margin-top: 0.12rem;
  font-size: 13px;
}
.undo-card__desc {
  margin: 0.45rem 0 0.7rem;
  color: var(--text-muted);
}
.undo-card__choices {
  display: flex;
  flex-direction: column;
  gap: 0.45rem;
}
.undo-choice {
  display: flex;
  align-items: center;
  gap: 0.55rem;
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.8);
}
.undo-choice:has(input:checked) {
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px rgba(46, 109, 164, 0.08);
}
.undo-choice input {
  width: auto;
}
.undo-modal__footer {
  display: flex;
  justify-content: flex-end;
  gap: 0.6rem;
  padding-top: 0.25rem;
}
</style>
