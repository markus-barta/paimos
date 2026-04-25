<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-146. Inline error banner for failed AI optimize attempts.

 The composable is a singleton, so `lastError` is shared across every
 surface (issue detail, create modal, side panel). Mounting this
 component anywhere a user can click an AI button gives them visible
 feedback when a request fails. Trim guard prevents whitespace-only
 errors from rendering as a blank banner — a defensive belt to the
 errMsg() suspenders in api/client.ts.
-->
<script setup lang="ts">
import { useAiOptimize } from '@/composables/useAiOptimize'

const aiOptimize = useAiOptimize()
</script>

<template>
  <!-- v-if relies on `lastError` being either null or a non-empty
       trimmed string. `errMsg()` in api/client.ts guarantees that
       contract — never returns "" or whitespace-only — so the
       template doesn't need to re-check. -->
  <div
    v-if="aiOptimize.lastError"
    class="ai-banner-error"
    role="alert"
  >
    <span>AI optimization failed: {{ aiOptimize.lastError }}</span>
    <button
      type="button"
      class="ai-banner-error-x"
      aria-label="Dismiss"
      @click="aiOptimize.clearError()"
    >×</button>
  </div>
</template>

<style scoped>
.ai-banner-error {
  display: flex; justify-content: space-between; align-items: center;
  gap: .5rem;
  background: #fef2f2; color: #b91c1c;
  border: 1px solid #fecaca; border-radius: 8px;
  padding: .5rem .85rem;
  font-size: 13px;
  margin-bottom: .75rem;
}
.ai-banner-error-x {
  background: none; border: none; color: #b91c1c;
  cursor: pointer; font-size: 16px; line-height: 1; padding: 0 .25rem;
}
.ai-banner-error-x:hover { color: #7f1d1d; }
</style>
