import type { Component } from "vue";

import SettingsAccountTab from "@/components/settings/SettingsAccountTab.vue";
import SettingsUsersTab from "@/components/settings/SettingsUsersTab.vue";
import SettingsTagsTab from "@/components/settings/SettingsTagsTab.vue";
import SettingsAppearanceTab from "@/components/settings/SettingsAppearanceTab.vue";
import SettingsViewsTab from "@/components/settings/SettingsViewsTab.vue";
import SettingsSprintsTab from "@/components/settings/SettingsSprintsTab.vue";
import SettingsDevelopmentTab from "@/components/settings/SettingsDevelopmentTab.vue";
import SettingsTrashTab from "@/components/settings/SettingsTrashTab.vue";
import SettingsPermissionsTab from "@/components/settings/SettingsPermissionsTab.vue";
import SettingsAITab from "@/components/settings/SettingsAITab.vue";
import SettingsAIPromptsTab from "@/components/settings/SettingsAIPromptsTab.vue";
import SettingsSystemTab from "@/components/settings/SettingsSystemTab.vue";

export type SettingsTab =
  | "account"
  | "tags"
  | "appearance"
  | "users"
  | "permissions"
  | "sprints"
  | "views"
  | "ai"
  | "ai-prompts"
  | "system"
  | "development"
  | "trash";

export type SettingsQueryTab = SettingsTab | "branding";

export interface SettingsTabDef {
  id: SettingsTab;
  label: string;
  adminOnly?: boolean;
  component: Component;
}

export const SETTINGS_TABS: SettingsTabDef[] = [
  { id: "account", label: "Account", component: SettingsAccountTab },
  { id: "tags", label: "Tags", component: SettingsTagsTab },
  { id: "appearance", label: "Visual", component: SettingsAppearanceTab },
  { id: "users", label: "Users", adminOnly: true, component: SettingsUsersTab },
  {
    id: "permissions",
    label: "Permissions",
    adminOnly: true,
    component: SettingsPermissionsTab,
  },
  {
    id: "sprints",
    label: "Sprints",
    adminOnly: true,
    component: SettingsSprintsTab,
  },
  { id: "views", label: "Views", adminOnly: true, component: SettingsViewsTab },
  { id: "ai", label: "AI", adminOnly: true, component: SettingsAITab },
  {
    id: "ai-prompts",
    label: "AI prompts",
    adminOnly: true,
    component: SettingsAIPromptsTab,
  },
  {
    id: "system",
    label: "System",
    adminOnly: true,
    component: SettingsSystemTab,
  },
  {
    id: "development",
    label: "Development",
    adminOnly: true,
    component: SettingsDevelopmentTab,
  },
  { id: "trash", label: "Trash", adminOnly: true, component: SettingsTrashTab },
];

const SETTINGS_TAB_IDS = new Set<SettingsTab>(
  SETTINGS_TABS.map((tab) => tab.id),
);

export function visibleSettingsTabs(isAdmin: boolean): SettingsTabDef[] {
  return SETTINGS_TABS.filter((tab) => !tab.adminOnly || isAdmin);
}

export function resolveSettingsTab(
  queryTab: SettingsQueryTab | undefined,
): SettingsTab {
  if (queryTab === "branding") return "appearance";
  return queryTab && SETTINGS_TAB_IDS.has(queryTab as SettingsTab)
    ? (queryTab as SettingsTab)
    : "account";
}
