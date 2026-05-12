<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 PAI-389 — persistent frame for super-admin impersonation sessions.
 Mounted in AppLayout so every authenticated surface shows who the
 effective user is, who the real operator is, and how to exit.
-->
<script setup lang="ts">
import { ref } from 'vue'
import { errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import AppIcon from '@/components/AppIcon.vue'

const auth = useAuthStore()
const exiting = ref(false)
const error = ref('')

function roleText(role?: string) {
  return (role || 'unknown').replace('_', ' ')
}

async function exitImpersonation() {
  if (exiting.value) return
  exiting.value = true
  error.value = ''
  try {
    await auth.stopImpersonation()
  } catch (e: unknown) {
    error.value = errMsg(e, 'Could not exit impersonation.')
  } finally {
    exiting.value = false
  }
}
</script>

<template>
  <div v-if="auth.impersonation" class="impersonation-banner" role="status">
    <AppIcon name="shield" :size="14" />
    <span class="ib-label">Acting as</span>
    <strong>{{ auth.impersonation.target.username }}</strong>
    <span class="ib-role">{{ roleText(auth.impersonation.target.role) }}</span>
    <span class="ib-sep">·</span>
    <span class="ib-actor">Operator: <strong>{{ auth.impersonation.actor.username }}</strong></span>
    <span v-if="error" class="ib-error">{{ error }}</span>
    <span class="ib-spacer" />
    <button type="button" class="ib-exit" :disabled="exiting" @click="exitImpersonation">
      <AppIcon name="log-out" :size="13" />
      {{ exiting ? 'Exiting…' : 'Exit' }}
    </button>
  </div>
</template>

<style scoped>
.impersonation-banner {
  display: flex;
  align-items: center;
  gap: .55rem;
  padding: .44rem 1rem;
  background: #fff7ed;
  border-bottom: 1px solid #fed7aa;
  color: #7c2d12;
  font-size: 12.5px;
  font-weight: 500;
}
.ib-label {
  font-size: 11px;
  font-weight: 800;
  letter-spacing: .08em;
  text-transform: uppercase;
}
.ib-role {
  padding: .08rem .4rem;
  border: 1px solid #fdba74;
  border-radius: 999px;
  color: #9a3412;
  font-size: 11px;
  text-transform: capitalize;
}
.ib-sep { opacity: .55; }
.ib-actor { color: #9a3412; }
.ib-error {
  color: #b91c1c;
  font-size: 12px;
}
.ib-spacer { flex: 1; }
.ib-exit {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  height: 26px;
  padding: 0 .65rem;
  border: 1px solid #fb923c;
  border-radius: var(--radius);
  background: #fff;
  color: #9a3412;
  font: inherit;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}
.ib-exit:hover:not(:disabled) {
  background: #ffedd5;
}
.ib-exit:disabled {
  cursor: wait;
  opacity: .65;
}
.impersonation-banner :deep(svg) {
  flex-shrink: 0;
}
@media (max-width: 700px) {
  .impersonation-banner {
    flex-wrap: wrap;
    gap: .35rem .5rem;
  }
  .ib-spacer {
    display: none;
  }
  .ib-exit {
    margin-left: auto;
  }
}
</style>
