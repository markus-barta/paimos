<script setup lang="ts">
/**
 * SessionExpiredBanner
 *
 * Mounted at the top of AppLayout. Renders only when the module-level
 * `sessionExpired` ref in `@/api/client` is true. The ref flips whenever
 * any non-auth API call returns 401 (mid-session token death, admin
 * revoke, 24h TTL expiry, etc.) — see client.ts maybeMarkSessionExpired.
 *
 * Clicking "Sign in" does a full browser reload to /login — simpler and
 * safer than trying to tear down Pinia stores piece by piece. The
 * banner is deliberately non-dismissible: any "X close" would let the
 * user forget they're logged out and keep losing edits.
 */
import { sessionExpired } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'

function signInAgain() {
  // Full reload + /login — wipes all in-memory state. Router guard will
  // run `fetchMe`, get 401, redirect to /login — same endpoint, but the
  // full reload removes any chance of stale reactive state surviving.
  window.location.href = '/login'
}
</script>

<template>
  <div v-if="sessionExpired" class="session-expired-banner" role="alert" aria-live="assertive">
    <AppIcon name="alert-circle" :size="18" class="se-icon" />
    <span class="se-text">
      <strong>Session expired.</strong>
      Your content may be out of date — please sign in again.
    </span>
    <button class="se-button" @click="signInAgain" type="button">
      Sign in
    </button>
  </div>
</template>

<style scoped>
/*
 * Layout child, not a fixed overlay — this sits ABOVE the rest of the
 * layout and pushes everything down when it appears. No z-index, no
 * position:fixed, no content overlap. Zero friction to the layout grid
 * because it's a sibling of .layout and the .layout itself is the
 * flex/grid container in AppLayout.vue.
 */
.session-expired-banner {
  display: flex;
  align-items: center;
  gap: .85rem;
  width: 100%;
  padding: .85rem 1.5rem;
  color: #fff;
  font-size: 14px;
  background: var(--bp-blue, #2e6da4);
  box-shadow: 0 2px 6px rgba(0, 0, 0, .2);
  /*
   * Subtle 2s ease-in-out fade between the main brand blue and the
   * darker variant. Smooth (not hard-blink) — photosensitive users +
   * taste both argue against true on/off strobing.
   */
  animation: session-expired-pulse 2s ease-in-out infinite;
  /* Guarantee the banner paints above any sibling overlays. */
  position: relative;
  z-index: 1000;
}

@keyframes session-expired-pulse {
  0%,
  100% {
    background: var(--bp-blue, #2e6da4);
  }
  50% {
    background: var(--bp-blue-dark, #1f4d75);
  }
}

.se-icon {
  flex-shrink: 0;
  color: #fff;
}

.se-text {
  flex: 1;
  line-height: 1.4;
}
.se-text strong {
  font-weight: 700;
  margin-right: .35rem;
}

.se-button {
  flex-shrink: 0;
  background: #fff;
  color: var(--bp-blue-dark, #1f4d75);
  border: none;
  border-radius: 4px;
  padding: .45rem 1rem;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  transition: background .12s, transform .05s;
}
.se-button:hover {
  background: #f0f4f8;
}
.se-button:active {
  transform: translateY(1px);
}

/* Respect the prefers-reduced-motion accessibility setting — pulsing
   animations are the exact thing that setting exists for. */
@media (prefers-reduced-motion: reduce) {
  .session-expired-banner {
    animation: none;
  }
}
</style>
