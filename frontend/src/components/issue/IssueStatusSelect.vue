<script setup lang="ts">
import MetaSelect from "@/components/MetaSelect.vue";
import type { MetaOption } from "@/components/MetaSelect.vue";
import {
  STATUS_DOT_STYLE,
  STATUS_LABEL,
} from "@/composables/useIssueDisplay";

defineProps<{
  modelValue: string;
  disabled?: boolean;
  loading?: boolean;
  size?: "sm" | "md";
  openOnMount?: boolean;
}>();

const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const STATUS_OPTIONS: MetaOption[] = [
  "new",
  "backlog",
  "in-progress",
  "qa",
  "done",
  "delivered",
  "accepted",
  "invoiced",
  "cancelled",
].map((value) => ({
  value,
  label: STATUS_LABEL[value] ?? value,
  dotColor: STATUS_DOT_STYLE[value]?.color,
  dotOutline: STATUS_DOT_STYLE[value]?.outline,
}));
</script>

<template>
  <MetaSelect
    :model-value="modelValue"
    :options="STATUS_OPTIONS"
    :disabled="disabled"
    :loading="loading"
    :size="size"
    :open-on-mount="openOnMount"
    @update:model-value="emit('update:modelValue', $event)"
  />
</template>
