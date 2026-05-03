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

import { defineStore } from "pinia";
import { computed, ref } from "vue";

let refreshAction: (() => void) | null = null;

export const useIssueRefreshPromptStore = defineStore(
  "issueRefreshPrompt",
  () => {
    const visible = ref(false);
    const count = ref<number | null>(null);

    const label = computed(() =>
      count.value && count.value > 0
        ? `${count.value} issue${count.value === 1 ? "" : "s"} updated`
        : "Issue list changed",
    );

    function show(nextCount: number | null | undefined, action: () => void) {
      count.value = nextCount ?? null;
      refreshAction = action;
      visible.value = true;
    }

    function clear(action?: () => void) {
      if (action && refreshAction !== action) return;
      visible.value = false;
      count.value = null;
      refreshAction = null;
    }

    function triggerRefresh() {
      if (!visible.value || !refreshAction) return false;
      refreshAction();
      return true;
    }

    return {
      visible,
      count,
      label,
      show,
      clear,
      triggerRefresh,
    };
  },
);
