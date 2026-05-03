<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { api, ApiError } from '@/api/client'
import { useBranding } from '@/composables/useBranding'
import { useSidebarColors } from '@/composables/useSidebarColors'
import AppIcon from '@/components/AppIcon.vue'
import LoadingText from '@/components/LoadingText.vue'

const route = useRoute()
const router = useRouter()
const { branding } = useBranding()
const { bgColor, patternColor } = useSidebarColors()
const version = __APP_VERSION__

const hexPatternSvg = computed(() => {
  const c = patternColor.value.replace(/#/g, '%23')
  return `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='28' height='49' viewBox='0 0 28 49'%3E%3Cg fill-rule='evenodd'%3E%3Cg fill='${c}' fill-opacity='0.5' fill-rule='nonzero'%3E%3Cpath d='M13.99 9.25l13 7.5v15l-13 7.5L1 31.75v-15l12.99-7.5zM3 17.9v12.7l10.99 6.34 11-6.35V17.9l-11-6.34L3 17.9zM0 15l12.98-7.5V0h-2v6.35L0 12.69v2.3zm0 18.5L12.98 41v8h-2v-6.85L0 35.81v-2.3zM15 0v7.5L27.99 15H28v-2.31h-.01L17 6.35V0h-2zm0 49v-8l12.99-7.5H28v2.31h-.01L17 42.15V49h-2z'/%3E%3C/g%3E%3C/g%3E%3C/svg%3E")`
})

const token = computed(() => String(route.params.token || ''))

// 'validating' → we're calling /reset/validate on mount
// 'ready'      → token is valid, show the form
// 'invalid'    → token is expired/used/unknown, show error
// 'submitting' → form is being submitted
// 'success'    → password was updated, redirect queued
const stage = ref<'validating' | 'ready' | 'invalid' | 'submitting' | 'success'>('validating')
const invalidReason = ref<'expired' | 'used' | 'unknown' | 'rate_limited' | ''>('')

const password = ref('')
const confirm  = ref('')
const error    = ref('')

const passwordsMatch = computed(() => password.value.length >= 8 && password.value === confirm.value)

onMounted(async () => {
  if (!token.value) {
    stage.value = 'invalid'
    invalidReason.value = 'unknown'
    return
  }
  try {
    const result = await api.get<{ valid: boolean; reason?: string }>(
      `/auth/reset/validate?token=${encodeURIComponent(token.value)}`,
    )
    if (result.valid) {
      stage.value = 'ready'
    } else {
      stage.value = 'invalid'
      invalidReason.value = (result.reason as any) || 'unknown'
    }
  } catch {
    stage.value = 'invalid'
    invalidReason.value = 'unknown'
  }
})

async function submit() {
  if (!passwordsMatch.value) return
  error.value = ''
  stage.value = 'submitting'
  try {
    await api.post('/auth/reset', {
      token: token.value,
      new_password: password.value,
    })
    stage.value = 'success'
    // Redirect to /login after a short delay so the user sees the success flash
    setTimeout(() => router.push('/login'), 1500)
  } catch (e) {
    stage.value = 'ready'
    if (e instanceof ApiError && e.status === 429) {
      error.value = 'Too many attempts. Please wait a few minutes and try again.'
    } else if (e instanceof ApiError && e.status === 400) {
      error.value = 'This reset link is no longer valid. Request a new one.'
    } else {
      error.value = 'Password reset failed. Please try again.'
    }
  }
}

const invalidMessage = computed(() => {
  switch (invalidReason.value) {
    case 'expired':      return 'This reset link has expired. Request a fresh one below.'
    case 'used':         return 'This reset link has already been used. Request a fresh one if you still need to change your password.'
    case 'rate_limited': return 'Too many requests. Please wait a few minutes and try again.'
    default:             return 'This reset link is not valid. Request a fresh one below.'
  }
})
</script>

<template>
  <div class="login-page" :style="{ background: bgColor }">
    <div class="hex-bg" :style="{ backgroundImage: hexPatternSvg }" aria-hidden="true"></div>

    <div class="login-card">
      <div class="login-header">
        <img :src="branding.logo" :alt="branding.company" class="login-logo" />
        <h1 class="login-title">Choose a new password</h1>
      </div>

      <div v-if="stage === 'validating'" class="loading-box">
        <LoadingText size="sm" label="Checking reset link…" />
      </div>

      <div v-else-if="stage === 'invalid'" class="submitted-box">
        <AppIcon name="alert-circle" :size="32" class="invalid-icon" />
        <p class="submitted-title">Link not valid</p>
        <p class="submitted-sub">{{ invalidMessage }}</p>
        <RouterLink to="/forgot" class="btn btn-primary login-btn">Request new link</RouterLink>
        <RouterLink to="/login" class="login-btn-back">← Back to sign in</RouterLink>
      </div>

      <div v-else-if="stage === 'success'" class="submitted-box">
        <AppIcon name="check" :size="32" class="submitted-icon" />
        <p class="submitted-title">Password updated</p>
        <p class="submitted-sub">Redirecting you to sign in…</p>
      </div>

      <form v-else @submit.prevent="submit" class="login-form">
        <div class="field">
          <label for="password">New password</label>
          <input
            id="password"
            v-model="password"
            type="password"
            autocomplete="new-password"
            placeholder="At least 8 characters"
            minlength="8"
            required
          />
        </div>
        <div class="field">
          <label for="confirm">Confirm new password</label>
          <input
            id="confirm"
            v-model="confirm"
            type="password"
            autocomplete="new-password"
            placeholder="Repeat"
            minlength="8"
            required
          />
        </div>
        <div v-if="confirm && !passwordsMatch" class="hint">
          {{ password.length < 8 ? 'Password must be at least 8 characters.' : 'Passwords do not match.' }}
        </div>
        <div v-if="error" class="login-error">{{ error }}</div>
        <button type="submit" class="btn btn-primary login-btn" :disabled="stage === 'submitting' || !passwordsMatch">
          {{ stage === 'submitting' ? 'Updating…' : 'Set new password' }}
        </button>
        <RouterLink to="/login" class="login-forgot-link">← Back to sign in</RouterLink>
      </form>

      <footer class="login-footer">
        <img :src="branding.logo" alt="" class="footer-logo" aria-hidden="true" />
        <span>{{ branding.company }}</span>
        <span class="footer-sep">·</span>
        <span>v{{ version }}</span>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex; align-items: center; justify-content: center;
  position: relative; overflow: hidden;
}
.hex-bg { position: absolute; inset: 0; background-size: 28px 49px; opacity: 1; }
.login-card {
  position: relative;
  background: var(--bg-card);
  border-radius: 10px;
  box-shadow: var(--shadow-md), 0 0 0 1px rgba(0,0,0,.08);
  padding: 2.5rem 2.25rem 2rem;
  width: 100%; max-width: 360px;
}
.login-header { text-align: center; margin-bottom: 2rem; }
.login-logo { width: 52px; height: 52px; object-fit: contain; margin-bottom: .75rem; }
.login-title { font-size: 22px; font-weight: 700; color: var(--text); letter-spacing: -.02em; }

.login-form { display: flex; flex-direction: column; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }

.hint { font-size: 12px; color: var(--text-muted); margin-top: -.5rem; }
.login-error { background: #fde8e8; color: #c0392b; border-radius: var(--radius); padding: .5rem .75rem; font-size: 13px; }

.login-btn { width: 100%; justify-content: center; padding: .65rem; font-size: 14px; margin-top: .25rem; }
.login-btn-back {
  display: block; width: 100%; text-align: center; font-size: 12px;
  color: var(--text-muted); margin-top: 1rem; text-decoration: none;
}
.login-btn-back:hover { color: var(--bp-blue); }
.login-forgot-link {
  align-self: center; font-size: 12px; color: var(--text-muted);
  text-decoration: none; margin-top: .1rem;
}
.login-forgot-link:hover { color: var(--bp-blue); text-decoration: underline; }

.loading-box { display: flex; flex-direction: column; align-items: center; gap: .75rem; padding: 2rem 0; }
.submitted-box { text-align: center; padding: .5rem 0; }
.submitted-icon { color: #2ecc71; margin-bottom: .75rem; }
.invalid-icon   { color: #e74c3c; margin-bottom: .75rem; }
.submitted-title { font-size: 17px; font-weight: 700; color: var(--text); margin-bottom: .5rem; }
.submitted-sub   { font-size: 13px; color: var(--text-muted); line-height: 1.5; margin-bottom: 1rem; }

.login-footer {
  display: flex; align-items: center; justify-content: center; gap: .4rem;
  margin-top: 1.75rem; color: #b0bec8; font-size: 11px; font-weight: 600;
  letter-spacing: .08em; text-transform: uppercase;
}
.footer-logo { width: 16px; height: 16px; object-fit: contain; opacity: .35; }
.footer-sep { opacity: .4; }
</style>
