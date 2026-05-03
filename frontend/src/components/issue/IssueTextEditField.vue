<script setup lang="ts">
import { ref } from "vue";
import AppIcon from "@/components/AppIcon.vue";
import AiActionMenu from "@/components/ai/AiActionMenu.vue";
import AiSurfaceFeedback from "@/components/ai/AiSurfaceFeedback.vue";
import { vAutoGrow } from "@/directives/autoGrow";
import type { AiApplyInfo } from "@/services/aiActionApply";

type UploadStatus = "pending" | "done" | "failed";
type UploadField = "description" | "acceptance_criteria";

interface AiApplyResult {
  undoLabel?: string;
  undo?: () => void | Promise<void>;
  undoAutoDismissMs?: number;
}

type AiApplyHandler = (
  info: AiApplyInfo,
) => void | Promise<void> | AiApplyResult | Promise<AiApplyResult | void>;

export interface IssueTextUploadJob {
  seq: number;
  field: UploadField;
  filename: string;
  file: File;
  isImage: boolean;
  progress: number;
  status: UploadStatus;
  error?: string;
  insertAt: number;
}

const props = withDefaults(
  defineProps<{
    modelValue: string;
    label: string;
    field: "description" | "acceptance_criteria" | "notes";
    hostKey: string;
    issueId: number;
    placeholder?: string;
    rows?: number;
    isMonospace?: boolean;
    attachmentsEnabled?: boolean;
    enableUploads?: boolean;
    jobs?: IssueTextUploadJob[];
    apply: AiApplyHandler;
    onAccept: (text: string) => void;
  }>(),
  {
    placeholder: "",
    rows: 4,
    isMonospace: false,
    attachmentsEnabled: false,
    enableUploads: false,
    jobs: () => [],
  },
);

const emit = defineEmits<{
  "update:modelValue": [value: string];
  "upload-files": [files: FileList | File[], insertAt: number];
  "retry-job": [job: IssueTextUploadJob];
  "dismiss-job": [job: IssueTextUploadJob];
}>();

const textareaRef = ref<HTMLTextAreaElement | null>(null);
const dragOver = ref(false);

function iconFor(job: IssueTextUploadJob) {
  if (job.status === "failed") return "alert-circle";
  if (job.status === "done") return "check";
  return job.isImage ? "image" : "paperclip";
}

function onInput(e: Event) {
  emit("update:modelValue", (e.target as HTMLTextAreaElement).value);
}

function onPaste(e: ClipboardEvent) {
  if (!props.enableUploads || !props.attachmentsEnabled) return;
  const files = e.clipboardData?.files;
  if (!files || !files.length) return;
  e.preventDefault();
  emit(
    "upload-files",
    files,
    textareaRef.value?.selectionStart ?? props.modelValue.length,
  );
}

function onDrop(e: DragEvent) {
  if (!props.enableUploads) return;
  const files = e.dataTransfer?.files;
  if (!files || !files.length) return;
  e.preventDefault();
  dragOver.value = false;
  if (!props.attachmentsEnabled) return;
  emit(
    "upload-files",
    files,
    textareaRef.value?.selectionStart ?? props.modelValue.length,
  );
}
</script>

<template>
  <div class="field">
    <div class="field-label-row">
      <label>{{ label }}</label>
      <AiActionMenu
        :host-key="hostKey"
        :field="field"
        :field-label="label"
        surface="issue"
        :issue-id="issueId"
        :text="() => modelValue"
        :on-accept="onAccept"
      />
    </div>

    <div v-if="jobs.length" class="upload-chips">
      <div
        v-for="job in jobs"
        :key="job.seq"
        class="upload-chip"
        :class="[`upload-chip--${job.status}`]"
      >
        <AppIcon :name="iconFor(job)" :size="13" />
        <span class="upload-chip__name" :title="job.filename">
          {{ job.filename }}
        </span>
        <template v-if="job.status === 'pending'">
          <div class="upload-chip__bar">
            <div
              class="upload-chip__bar-fill"
              :style="{ width: job.progress + '%' }"
            ></div>
          </div>
          <span class="upload-chip__pct">{{ job.progress }}%</span>
        </template>
        <span
          v-else-if="job.status === 'failed'"
          class="upload-chip__error"
          :title="job.error"
        >
          {{ job.error }}
        </span>
        <button
          v-if="job.status === 'failed'"
          class="upload-chip__btn"
          @click="emit('retry-job', job)"
          title="Retry upload"
          type="button"
        >
          <AppIcon name="refresh-cw" :size="12" />
        </button>
        <button
          v-if="job.status !== 'done'"
          class="upload-chip__btn"
          @click="emit('dismiss-job', job)"
          title="Dismiss"
          type="button"
        >
          <AppIcon name="x" :size="12" />
        </button>
      </div>
    </div>

    <div
      v-if="enableUploads"
      class="textarea-drop-wrap"
      @dragenter.prevent="attachmentsEnabled ? (dragOver = true) : null"
      @dragleave.self="dragOver = false"
    >
      <textarea
        ref="textareaRef"
        v-auto-grow
        :value="modelValue"
        :rows="rows"
        :class="{ 'textarea--mono': isMonospace }"
        :placeholder="placeholder"
        @input="onInput"
        @paste="onPaste"
        @dragover.prevent
        @drop="onDrop"
      ></textarea>
      <div v-if="dragOver && attachmentsEnabled" class="textarea-drop-overlay">
        <AppIcon name="upload" :size="20" /> Drop files here
      </div>
    </div>
    <textarea
      v-else
      ref="textareaRef"
      v-auto-grow
      :value="modelValue"
      :rows="rows"
      :class="{ 'textarea--mono': isMonospace }"
      :placeholder="placeholder"
      @input="onInput"
    ></textarea>

    <AiSurfaceFeedback :host-key="hostKey" :apply="apply" />
  </div>
</template>

<style scoped>
.field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.field label {
  font-size: 11px;
  font-weight: 700;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.field-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-bottom: 0.25rem;
}
.field-label-row > label {
  margin-bottom: 0;
}
textarea {
  resize: vertical;
  min-height: 80px;
}
.textarea--mono {
  font-family: "DM Mono", "Menlo", monospace !important;
  font-size: 13px;
}
.textarea-drop-wrap {
  position: relative;
}
.textarea-drop-overlay {
  position: absolute;
  inset: 0;
  z-index: 5;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  background: rgba(46, 109, 164, 0.08);
  border: 2px dashed var(--bp-blue);
  border-radius: var(--radius);
  color: var(--bp-blue);
  font-size: 13px;
  font-weight: 600;
  pointer-events: none;
  animation: drop-overlay-in 140ms cubic-bezier(0.2, 0.7, 0.2, 1);
}
@keyframes drop-overlay-in {
  from {
    opacity: 0;
    transform: scale(0.985);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}
.upload-chips {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  margin-bottom: 0.15rem;
}
.upload-chip {
  display: flex;
  align-items: center;
  gap: 0.55rem;
  padding: 0.38rem 0.55rem;
  background: var(--bg-card, #fff);
  border: 1px solid var(--border);
  border-radius: calc(var(--radius) - 2px);
  font-size: 12px;
  color: var(--text);
  font-weight: 500;
  line-height: 1;
  animation: upload-chip-in 180ms cubic-bezier(0.2, 0.7, 0.2, 1);
}
@keyframes upload-chip-in {
  from {
    opacity: 0;
    transform: translateY(-3px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
.upload-chip--pending {
  border-color: rgba(46, 109, 164, 0.28);
  background: linear-gradient(
    180deg,
    rgba(46, 109, 164, 0.05),
    rgba(46, 109, 164, 0.02)
  );
}
.upload-chip--done {
  border-color: rgba(30, 132, 73, 0.35);
  background: rgba(30, 132, 73, 0.06);
  color: #1e7a3a;
  transition: opacity 0.5s ease;
  animation: upload-chip-out-delayed 1.5s forwards;
}
@keyframes upload-chip-out-delayed {
  0%,
  70% {
    opacity: 1;
  }
  100% {
    opacity: 0;
    transform: translateY(-2px);
  }
}
.upload-chip--failed {
  border-color: rgba(192, 57, 43, 0.4);
  background: #fdeeec;
  color: #a02b1c;
}
.upload-chip__name {
  flex: 0 1 auto;
  min-width: 0;
  max-width: 260px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-variant-numeric: tabular-nums;
}
.upload-chip__bar {
  flex: 1 1 auto;
  min-width: 60px;
  max-width: 220px;
  height: 4px;
  background: rgba(46, 109, 164, 0.12);
  border-radius: 999px;
  overflow: hidden;
  position: relative;
}
.upload-chip__bar-fill {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 0;
  background: var(--bp-blue, #2e6da4);
  border-radius: 999px;
  width: 0;
  transition: width 140ms linear;
}
.upload-chip__bar-fill::after {
  content: "";
  position: absolute;
  inset: 0;
  background: linear-gradient(
    90deg,
    transparent,
    rgba(255, 255, 255, 0.6),
    transparent
  );
  animation: upload-chip-shimmer 1.1s linear infinite;
}
@keyframes upload-chip-shimmer {
  0% {
    transform: translateX(-100%);
  }
  100% {
    transform: translateX(100%);
  }
}
.upload-chip__pct {
  font-size: 11px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  min-width: 32px;
  text-align: right;
}
.upload-chip__error {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: #a02b1c;
}
.upload-chip__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: none;
  color: inherit;
  opacity: 0.6;
  cursor: pointer;
  padding: 2px;
  border-radius: 4px;
  transition:
    opacity 0.12s,
    background 0.12s;
}
.upload-chip__btn:hover {
  opacity: 1;
  background: rgba(0, 0, 0, 0.06);
}
.upload-chip--failed .upload-chip__btn:hover {
  background: rgba(160, 43, 28, 0.1);
}
</style>
