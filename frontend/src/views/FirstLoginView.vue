<script setup lang="ts">
/*
 * FirstLoginView (PAI-321)
 *
 * Forced password-change screen for newly created accounts. Reached
 * by the router guard whenever:
 *   - the global `mustChangePassword` ref is true (set by the API
 *     client's 403 interceptor), OR
 *   - any other route is requested while the flag is set.
 *
 * The screen is the only authenticated route a user with
 * must_change_password=1 can reach until they POST /api/auth/password
 * successfully. The backend gate is the source of truth — this page
 * is the user-facing affordance.
 *
 * Reuses the existing /api/auth/password endpoint and the same shape
 * of password rules (≥8 chars, current password required) so there's
 * no parallel password-policy implementation. The intent over the
 * older reset-password screen is mainly the framing: this is a
 * "set your real password" moment, not a "you forgot it" recovery.
 */
import { ref, computed } from "vue";
import { useRouter } from "vue-router";
import { api, errMsg, mustChangePassword } from "@/api/client";
import { useAuthStore } from "@/stores/auth";

const router = useRouter();
const auth = useAuthStore();

const currentPassword = ref("");
const newPassword = ref("");
const confirmPassword = ref("");
const submitting = ref(false);
const error = ref("");
const showWhy = ref(false);

const username = computed(() => auth.user?.username ?? "");

// Mirror the backend's minimum-length rule so the inline hint and the
// disabled state agree with what the server will accept. Anything
// stricter is intentionally avoided here — diverging the two is the
// fastest way to ship a frustrating "your password meets the rules
// shown but the server rejected it anyway" loop.
const MIN_LENGTH = 8;

const lengthOK = computed(() => newPassword.value.length >= MIN_LENGTH);
const matchOK = computed(
  () =>
    newPassword.value.length > 0 && newPassword.value === confirmPassword.value,
);
const distinctOK = computed(
  () =>
    newPassword.value.length === 0 ||
    newPassword.value !== currentPassword.value,
);
const formValid = computed(
  () =>
    currentPassword.value.length > 0 &&
    lengthOK.value &&
    matchOK.value &&
    distinctOK.value,
);

async function submit() {
  if (!formValid.value || submitting.value) return;
  submitting.value = true;
  error.value = "";
  try {
    await api.post("/auth/password", {
      current_password: currentPassword.value,
      new_password: newPassword.value,
    });
    // Success — clear the global gate so the router guard releases us.
    mustChangePassword.value = false;
    // Refresh the auth store so any role/access changes the admin
    // applied alongside the create are also picked up.
    await auth.refreshMe();
    // Land internal users on the dashboard, externals on the portal.
    router.replace(auth.user?.role === "external" ? "/portal" : "/");
  } catch (e: unknown) {
    error.value = errMsg(e, "Could not change password.");
  } finally {
    submitting.value = false;
  }
}

async function signOut() {
  await auth.logout();
}
</script>

<template>
  <div class="fl-shell">
    <div class="fl-card" role="region" aria-labelledby="fl-title">
      <div class="fl-icon" aria-hidden="true">
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
          <rect x="4" y="11" width="16" height="9" rx="2" />
          <path d="M8 11V7a4 4 0 0 1 8 0v4" />
        </svg>
      </div>

      <h1 id="fl-title" class="fl-title">Set a permanent password</h1>
      <p class="fl-subtitle">
        Welcome<template v-if="username">, <strong>{{ username }}</strong></template>!
        Before you continue, please replace the temporary password your
        administrator set with one only you know.
      </p>

      <form class="fl-form" @submit.prevent="submit" autocomplete="off">
        <div class="fl-field">
          <label for="fl-current">Current password</label>
          <input
            id="fl-current"
            v-model="currentPassword"
            type="password"
            autocomplete="current-password"
            required
            autofocus
          />
          <p class="fl-help">
            The temporary password your admin shared with you.
          </p>
        </div>

        <div class="fl-field">
          <label for="fl-new">New password</label>
          <input
            id="fl-new"
            v-model="newPassword"
            type="password"
            autocomplete="new-password"
            required
            :minlength="MIN_LENGTH"
          />
          <ul class="fl-rules">
            <li :class="{ ok: lengthOK }">
              At least {{ MIN_LENGTH }} characters
            </li>
            <li :class="{ ok: distinctOK }">
              Different from your temporary password
            </li>
          </ul>
        </div>

        <div class="fl-field">
          <label for="fl-confirm">Confirm new password</label>
          <input
            id="fl-confirm"
            v-model="confirmPassword"
            type="password"
            autocomplete="new-password"
            required
          />
          <p v-if="confirmPassword.length > 0 && !matchOK" class="fl-help fl-help--err">
            The two passwords don't match.
          </p>
        </div>

        <p v-if="error" class="fl-error" role="alert">{{ error }}</p>

        <button
          type="submit"
          class="fl-primary"
          :disabled="!formValid || submitting"
        >
          {{ submitting ? "Saving…" : "Save and continue" }}
        </button>
      </form>

      <div class="fl-aside">
        <button type="button" class="fl-link" @click="showWhy = !showWhy">
          {{ showWhy ? "Hide details" : "Why am I seeing this?" }}
        </button>
        <p v-if="showWhy" class="fl-aside-body">
          Your account was created by an administrator with a temporary
          password. To make sure no one else can sign in as you, we ask
          new accounts to set a private password before doing anything
          else. After you save, this prompt will not appear again on
          future sign-ins.
        </p>
      </div>

      <button type="button" class="fl-secondary" @click="signOut">
        Sign out instead
      </button>
    </div>
  </div>
</template>

<style scoped>
/*
 * Stand-alone screen — no AppLayout chrome. Centered card on a soft
 * gradient that matches the login screen's tone. Designed to feel
 * intentional (not a system error or modal) so the user understands
 * this is a step they're meant to complete, not a problem to dismiss.
 */
.fl-shell {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 2rem 1rem;
  background:
    radial-gradient(ellipse at top, rgba(46, 109, 164, 0.08), transparent 60%),
    var(--bg);
}

.fl-card {
  width: 100%;
  max-width: 440px;
  background: var(--bg-card);
  border-radius: 14px;
  border: 1px solid var(--border);
  box-shadow:
    0 24px 60px rgba(15, 28, 48, 0.12),
    0 4px 12px rgba(15, 28, 48, 0.06);
  padding: 2.25rem 2rem 1.75rem;
}

.fl-icon {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: var(--brand-blue-pale, #dce9f4);
  color: var(--brand-blue-dark, #1f4d75);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 1rem;
}

.fl-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -0.015em;
  margin-bottom: 0.4rem;
}

.fl-subtitle {
  font-size: 14px;
  line-height: 1.55;
  color: var(--text-muted);
  margin-bottom: 1.5rem;
}
.fl-subtitle strong {
  color: var(--text);
  font-weight: 600;
}

.fl-form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  margin-bottom: 1.25rem;
}

.fl-field label {
  display: block;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-bottom: 0.35rem;
}

.fl-help {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 0.4rem;
  line-height: 1.45;
}
.fl-help--err {
  color: #c0392b;
}

/* Inline rule list with a green tick on satisfied items. Reads like a
   contract the user is filling out, not a wall of red errors. */
.fl-rules {
  list-style: none;
  padding: 0;
  margin: 0.4rem 0 0;
  font-size: 12px;
  color: var(--text-muted);
}
.fl-rules li {
  position: relative;
  padding-left: 1.1rem;
  line-height: 1.55;
}
.fl-rules li::before {
  content: "○";
  position: absolute;
  left: 0;
  top: 0;
  color: var(--border);
  font-size: 11px;
}
.fl-rules li.ok {
  color: #15803d;
}
.fl-rules li.ok::before {
  content: "✓";
  color: #15803d;
  font-weight: 700;
}

.fl-error {
  background: #fdecea;
  color: #c0392b;
  border: 1px solid #f5c6c0;
  border-radius: 6px;
  padding: 0.55rem 0.75rem;
  font-size: 13px;
  margin: 0;
}

.fl-primary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  padding: 0.75rem 1.25rem;
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
.fl-primary:hover:not(:disabled) {
  background: var(--brand-blue-dark, #1f4d75);
}
.fl-primary:active:not(:disabled) {
  transform: translateY(1px);
}
.fl-primary:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.fl-primary:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px rgba(46, 109, 164, 0.35);
}

.fl-aside {
  margin-top: 1rem;
  padding-top: 1rem;
  border-top: 1px solid var(--border);
}
.fl-link {
  background: none;
  border: none;
  padding: 0;
  color: var(--brand-blue);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}
.fl-link:hover {
  color: var(--brand-blue-dark);
  text-decoration: underline;
}
.fl-aside-body {
  margin-top: 0.5rem;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.55;
}

.fl-secondary {
  display: block;
  width: 100%;
  margin-top: 1rem;
  padding: 0.5rem;
  font-size: 12px;
  color: var(--text-muted);
  background: transparent;
  border: none;
  cursor: pointer;
}
.fl-secondary:hover {
  color: var(--text);
  text-decoration: underline;
}
</style>
