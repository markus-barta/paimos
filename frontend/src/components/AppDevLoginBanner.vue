<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.

 PAI-267 — non-dismissable red banner shown whenever the current
 session was created via the dev-login route. The visual prominence
 is intentional: there must be no chance of mistaking a dev session
 for a real one. Banner clears only on real logout (the auth store
 resets `viaDevLogin` in `logout()`).

 This component is mounted unconditionally in AppLayout; the
 `v-if="auth.viaDevLogin"` gate means it renders nothing in the
 default (production) case, so the only cost on a real session is
 the auth-store ref read.
-->
<script setup lang="ts">
import { computed } from 'vue'
import { useAuthStore } from '@/stores/auth'
import AppIcon from '@/components/AppIcon.vue'

const auth = useAuthStore()

const accessSummary = computed(() => {
  if (!auth.user) return ''
  if (auth.allProjects) return 'all projects (admin)'
  const count = auth.accessibleProjects?.size ?? 0
  if (count === 0) return 'no project memberships'
  return `${count} project membership${count === 1 ? '' : 's'}`
})
</script>

<template>
  <div v-if="auth.viaDevLogin" class="dev-login-banner" role="alert">
    <AppIcon name="alert-triangle" :size="14" />
    <span class="dlb-label">DEV LOGIN ACTIVE</span>
    <span class="dlb-sep">·</span>
    <span class="dlb-user">
      <strong>{{ auth.user?.username ?? '?' }}</strong>
      ({{ auth.user?.role ?? '?' }} · {{ accessSummary }})
    </span>
    <span class="dlb-spacer" />
    <span class="dlb-hint">Sessions auto-expire after 24h. Logout clears the banner.</span>
  </div>
</template>

<style scoped>
.dev-login-banner {
  display: flex; align-items: center; gap: .55rem;
  padding: .45rem 1rem;
  background: #b91c1c;
  color: #fff;
  font-size: 12.5px;
  font-weight: 500;
  letter-spacing: .015em;
  border-bottom: 2px solid #7f1d1d;
}
.dlb-label {
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
  font-size: 11px;
}
.dlb-sep { opacity: .55; }
.dlb-spacer { flex: 1; }
.dlb-hint {
  font-size: 11px;
  opacity: .8;
  font-weight: 400;
}
.dev-login-banner :deep(svg) { flex-shrink: 0; }
@media (max-width: 700px) {
  .dlb-hint { display: none; }
}
</style>
