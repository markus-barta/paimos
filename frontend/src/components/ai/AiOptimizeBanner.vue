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
 feedback when a request fails.

 Refs are destructured into top-level bindings so Vue's <script setup>
 template auto-unwrap kicks in: a nested access like
 `aiOptimize.lastError` would yield the Ref object (always truthy) in
 v-if, leaving an empty banner stuck on screen even when no error
 exists. Top-level `lastError` is unwrapped to its value in both v-if
 and interpolation.
-->
<script setup lang="ts">
import { useAiOptimize } from '@/composables/useAiOptimize'

const { lastError, clearError } = useAiOptimize()
</script>

<template>
  <div
    v-if="lastError"
    class="ai-banner-error"
    role="alert"
  >
    <span>AI optimization failed: {{ lastError }}</span>
    <button
      type="button"
      class="ai-banner-error-x"
      aria-label="Dismiss"
      @click="clearError()"
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
