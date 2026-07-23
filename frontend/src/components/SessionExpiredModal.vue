<script setup lang="ts">
/**
 * SessionExpiredModal (PAI-322)
 *
 * Replaces the older SessionExpiredBanner. Shown whenever the
 * `sessionExpired` ref in `@/api/client` flips to true — that happens
 * when:
 *   - any non-auth API call returns 401 (sliding window blew through
 *     the absolute cap, admin disabled the user, etc.),
 *   - the visibility-change heartbeat in App.vue sees a was-logged-in →
 *     now-not-logged-in transition,
 *   - or a sibling tab broadcasts `session-expired` over the
 *     `paimos-auth` BroadcastChannel.
 *
 * Deliberately non-dismissible. There is no close affordance and no
 * click-outside-to-close: the user has either signed out or been
 * signed out by the server, and any "X" would let them keep editing
 * a stale UI and lose work on submit. The only path forward is
 * `Sign in` → full reload to /login with the current URL captured as
 * `next`, so we land them back where they were after re-auth.
 */
import { computed } from "vue";
import { sessionExpired } from "@/api/client";
import { useAuthStore } from "@/stores/auth";

const auth = useAuthStore();

// We snapshot the username at modal-render time. By the time the
// modal shows, `auth.user` is usually already cleared by the
// fetchMe-on-401 cycle — but if the 401 raced and we still have a
// user object, surface it so the user has a "log in as <whom>" cue.
const knownName = computed(() => auth.user?.username ?? "");

function signIn() {
  // Capture the current path + query so the post-login flow can deep-
  // link the user back to where they were. LoginView reads
  // `?redirect=…` via postLoginRedirectOrFallback — match that
  // convention here.
  const here = window.location.pathname + window.location.search;
  const isLoginPage = window.location.pathname.startsWith("/login");
  const qs = !isLoginPage ? `?redirect=${encodeURIComponent(here)}` : "";
  window.location.href = `/login${qs}`;
}
</script>

<template>
  <Teleport to="body">
    <Transition name="se-modal">
      <div
        v-if="sessionExpired"
        class="se-overlay"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="se-title"
        aria-describedby="se-desc"
      >
        <div class="se-card">
          <div class="se-icon-ring" aria-hidden="true">
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <circle cx="12" cy="12" r="9" />
              <path d="M12 7v5l3 2" />
            </svg>
          </div>
          <h2 id="se-title" class="se-title">You've been signed out</h2>
          <p id="se-desc" class="se-desc">
            <template v-if="knownName">
              Your session has ended. Sign in again as
              <strong>{{ knownName }}</strong> to pick up where you left off.
            </template>
            <template v-else>
              Your session has ended. Sign in again to pick up where you left
              off.
            </template>
          </p>
          <button
            class="se-primary"
            type="button"
            autofocus
            @click="signIn"
          >
            Sign in
          </button>
          <p class="se-hint">
            Anything you were editing is preserved on the server up to the
            last save.
          </p>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
/*
 * The overlay is a fixed full-viewport scrim with a slight backdrop
 * blur. Centered card; no click-outside-to-close (no @click.self
 * handler). Stacks above any other modal (z 1100) so a stray editor
 * dialog can't visually obscure the sign-in path.
 */
.se-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 28, 48, 0.55);
  backdrop-filter: blur(2px);
  -webkit-backdrop-filter: blur(2px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1100;
  padding: 1.5rem;
}

.se-card {
  background: var(--bg-card);
  border-radius: 12px;
  box-shadow:
    0 20px 50px rgba(15, 28, 48, 0.25),
    0 4px 12px rgba(15, 28, 48, 0.12);
  width: 100%;
  max-width: 420px;
  padding: 2rem 2rem 1.75rem;
  text-align: center;
  border: 1px solid var(--border);
}

.se-icon-ring {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: var(--brand-blue-pale, #dce9f4);
  color: var(--brand-blue-dark, #1f4d75);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 1rem;
}

.se-title {
  font-size: 18px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -0.01em;
  margin-bottom: 0.5rem;
}

.se-desc {
  font-size: 14px;
  line-height: 1.55;
  color: var(--text-muted);
  margin-bottom: 1.5rem;
}
.se-desc strong {
  color: var(--text);
  font-weight: 600;
}

.se-primary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  padding: 0.7rem 1.25rem;
  font-size: 14px;
  font-weight: 600;
  color: #fff;
  background: var(--brand-blue, #2e6da4);
  border: 1px solid var(--brand-blue-dark, #1f4d75);
  border-radius: 8px;
  cursor: pointer;
  transition:
    background 0.15s,
    transform 0.05s,
    box-shadow 0.15s;
  box-shadow: 0 1px 0 rgba(0, 0, 0, 0.05);
}
.se-primary:hover {
  background: var(--brand-blue-dark, #1f4d75);
}
.se-primary:active {
  transform: translateY(1px);
}
.se-primary:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px rgba(46, 109, 164, 0.35);
}

.se-hint {
  margin-top: 1rem;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.5;
}

/*
 * Subtle entrance — the modal is a serious moment but it should still
 * feel kind. Fade + a small upward translate (8px) over 180ms; respects
 * prefers-reduced-motion by disabling the translate.
 */
.se-modal-enter-active,
.se-modal-leave-active {
  transition:
    opacity 0.18s ease,
    transform 0.18s ease;
}
.se-modal-enter-from {
  opacity: 0;
}
.se-modal-leave-to {
  opacity: 0;
}
.se-modal-enter-from .se-card,
.se-modal-leave-to .se-card {
  transform: translateY(8px);
}

@media (prefers-reduced-motion: reduce) {
  .se-modal-enter-active,
  .se-modal-leave-active {
    transition: opacity 0.18s ease;
  }
  .se-modal-enter-from .se-card,
  .se-modal-leave-to .se-card {
    transform: none;
  }
}
</style>
