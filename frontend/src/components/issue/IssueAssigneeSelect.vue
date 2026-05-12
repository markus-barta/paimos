<script setup lang="ts">
import { computed } from "vue";
import MetaSelect from "@/components/MetaSelect.vue";
import type { MetaOption } from "@/components/MetaSelect.vue";
import type { User } from "@/types";
import { assignableIssueUsers } from "@/utils/users";

type AssigneeUser = Pick<User, "id" | "username" | "role" | "status"> &
  Partial<Pick<User, "avatar_path" | "first_name" | "last_name" | "email" | "nickname">>;

const props = defineProps<{
  modelValue: string;
  users: AssigneeUser[];
  fallbackUser?: { id: number; username: string; avatar_path?: string; first_name?: string; last_name?: string; email?: string; nickname?: string } | null;
  disabled?: boolean;
  loading?: boolean;
  size?: "sm" | "md";
  openOnMount?: boolean;
}>();

const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const options = computed<MetaOption[]>(() => {
  const rows = assignableIssueUsers(props.users);
  const selected = props.modelValue
    ? props.users.find((u) => String(u.id) === props.modelValue)
      ?? (props.fallbackUser && String(props.fallbackUser.id) === props.modelValue
        ? props.fallbackUser
        : null)
    : null;
  const visible = selected && !rows.some((u) => u.id === selected.id)
    ? [selected, ...rows]
    : rows;
  return visible.map((u) => ({
    value: String(u.id),
    label: u.username,
    avatarUser: u,
  }));
});
</script>

<template>
  <MetaSelect
    :model-value="modelValue"
    :options="options"
    placeholder="Unassigned"
    searchable
    :disabled="disabled"
    :loading="loading"
    :size="size"
    :open-on-mount="openOnMount"
    @update:model-value="emit('update:modelValue', $event)"
  />
</template>
