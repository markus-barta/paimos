<script setup lang="ts">
import { computed } from "vue";
import type { IssueAIWorkStatus } from "@/types";

const props = defineProps<{
  run: IssueAIWorkStatus;
}>();

const emit = defineEmits<{
  (e: "open"): void;
}>();

const LABELS: Record<string, string> = {
  queued: "Queued",
  running: "Running",
  drafted: "Draft ready",
  tests_passed: "Tests ok",
  tests_failed: "Tests failed",
  deployed: "Deployed",
  failed: "Failed",
  cancelled: "Cancelled",
};

const PHRASES: Record<string, string> = {
  queued: "AI queued",
  running: "AI running",
  drafted: "AI draft ready",
  tests_passed: "AI tests ok",
  tests_failed: "AI tests failed",
  deployed: "AI deployed",
  failed: "AI failed",
  cancelled: "AI cancelled",
};

const statusLabel = computed(() => LABELS[props.run.status] ?? props.run.status);
const providerLabel = computed(() => props.run.provider_label?.trim() ?? "");
const label = computed(() =>
  providerLabel.value ? `${providerLabel.value} ${statusLabel.value.toLowerCase()}` : statusLabel.value,
);
const phrase = computed(() =>
  providerLabel.value
    ? `${providerLabel.value} ${statusLabel.value.toLowerCase()}`
    : (PHRASES[props.run.status] ?? `AI ${props.run.status}`),
);

function testPhrase(): string {
  const summary = props.run.tests_summary ?? "";
  if (props.run.status === "tests_failed" || /fail/i.test(summary)) return "tests failed";
  if (
    props.run.status === "tests_passed" ||
    props.run.status === "deployed" ||
    /pass/i.test(summary)
  ) {
    return "tests passed";
  }
  return summary ? "tests captured" : "";
}

const title = computed(() => {
  const bits = [phrase.value];
  if (providerLabel.value) bits.push(props.run.action_key);
  if (props.run.profile_id) bits.push(`profile ${props.run.profile_id}`);
  if (props.run.effort) bits.push(`effort ${props.run.effort}`);
  if (props.run.prompt_preset_ref) bits.push(`prompt ${props.run.prompt_preset_ref}`);
  if (props.run.context_pack) bits.push(`context ${props.run.context_pack}`);
  if (props.run.version) bits.push(`runner v${props.run.version}`);
  if (props.run.deploy_target) bits.push(`target ${props.run.deploy_target}`);
  const tests = testPhrase();
  if (tests) bits.push(tests);
  if (props.run.device_id) bits.push(props.run.device_id);
  if (props.run.error) bits.push(props.run.error);
  bits.push("open run history");
  return bits.join(" • ");
});
</script>

<template>
  <button
    type="button"
    class="ai-work-badge"
    :class="`ai-work-badge--${run.status}`"
    :title="title"
    :aria-label="`${phrase}; open run history`"
    @click.stop="emit('open')"
  >
    {{ label }}
  </button>
</template>

<style scoped>
.ai-work-badge {
  border: 0;
  cursor: pointer;
  font-family: inherit;
  font-size: 10px;
  font-weight: 700;
  padding: 2px 7px;
  border-radius: 10px;
  white-space: nowrap;
  line-height: 1;
  color: var(--text-muted);
  background: color-mix(in srgb, var(--text-muted) 12%, transparent);
}
.ai-work-badge:hover {
  text-decoration: underline;
}
.ai-work-badge--queued,
.ai-work-badge--running {
  color: var(--bp-blue, #2563eb);
  background: color-mix(in srgb, var(--bp-blue, #2563eb) 16%, transparent);
}
.ai-work-badge--drafted {
  color: #6d28d9;
  background: color-mix(in srgb, #8b5cf6 18%, transparent);
}
.ai-work-badge--tests_passed,
.ai-work-badge--deployed {
  color: #1e8449;
  background: color-mix(in srgb, #2ecc71 22%, transparent);
}
.ai-work-badge--tests_failed,
.ai-work-badge--failed {
  color: #c0392b;
  background: #fef2f2;
}
.ai-work-badge--cancelled {
  color: #697586;
  background: color-mix(in srgb, #697586 16%, transparent);
}
</style>
