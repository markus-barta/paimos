<script setup lang="ts">
import { ref, watch, computed, onMounted } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { ApiError, api } from '@/api/client'
import { useBranding } from '@/composables/useBranding'
import { useSidebarColors } from '@/composables/useSidebarColors'
import { postLoginRedirectOrFallback } from '@/router/redirects'
import AppIcon from '@/components/AppIcon.vue'

const { branding } = useBranding()
const { bgColor, patternColor } = useSidebarColors()
const route = useRoute()

const version = __APP_VERSION__

// PAI-120: SSO probe. The button only appears once /api/auth/oidc/status
// reports enabled=true, so an instance with no IdP configured looks
// identical to today.
const ssoEnabled = ref(false)
const ssoLabel = ref('Sign in with SSO')
onMounted(async () => {
  try {
    const r = await api.get<{ enabled: boolean; label: string }>('/auth/oidc/status')
    ssoEnabled.value = r.enabled
    if (r.label) ssoLabel.value = r.label
  } catch {
    /* no-op — SSO simply stays hidden */
  }
})

const ssoError = computed(() => {
  const e = route.query.sso_error
  if (!e) return ''
  const code = Array.isArray(e) ? e[0] : e
  switch (code) {
    case 'bad_state':
    case 'missing_verifier':
      return 'SSO handshake expired — please try again.'
    case 'email_required':
      return 'SSO did not return a verified email; sign in with a password instead.'
    case 'not_configured':
      return 'SSO is not configured on this server.'
    default:
      return 'SSO sign-in failed. Please try again.'
  }
})

const hexPatternSvg = computed(() => {
  const c = patternColor.value.replace(/#/g, '%23')
  return `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='28' height='49' viewBox='0 0 28 49'%3E%3Cg fill-rule='evenodd'%3E%3Cg fill='${c}' fill-opacity='0.5' fill-rule='nonzero'%3E%3Cpath d='M13.99 9.25l13 7.5v15l-13 7.5L1 31.75v-15l12.99-7.5zM3 17.9v12.7l10.99 6.34 11-6.35V17.9l-11-6.34L3 17.9zM0 15l12.98-7.5V0h-2v6.35L0 12.69v2.3zm0 18.5L12.98 41v8h-2v-6.85L0 35.81v-2.3zM15 0v7.5L27.99 15H28v-2.31h-.01L17 6.35V0h-2zm0 49v-8l12.99-7.5H28v2.31h-.01L17 42.15V49h-2z'/%3E%3C/g%3E%3C/g%3E%3C/svg%3E")`
})

const auth = useAuthStore()
const router = useRouter()

const username = ref('')
const password = ref('')
const error    = ref('')
const loading  = ref(false)

// 2FA second step
const totpRequired = ref(false)
const totpToken    = ref('')
const otpCode      = ref('')

const postLoginPath = computed(() => postLoginRedirectOrFallback(route.query.redirect))

function finishLogin() {
  router.push(postLoginPath.value)
}

async function submit() {
  error.value = ''
  loading.value = true
  try {
    const result = await api.post<any>('/auth/login', {
      username: username.value,
      password: password.value,
    })
    if (result.totp_required) {
      totpToken.value    = result.totp_token
      totpRequired.value = true
    } else {
      // Successful login envelope: { user, access }.
      auth.setUser(result.user)
      auth.hydrateAccess(result.access)
      finishLogin()
    }
  } catch (e) {
    error.value = e instanceof ApiError ? 'Invalid username or password.' : 'Login failed.'
  } finally {
    loading.value = false
  }
}

async function submitOTP() {
  if (otpCode.value.length !== 6) return
  error.value = ''
  loading.value = true
  try {
    const result = await api.post<any>('/auth/totp/verify', {
      totp_token: totpToken.value,
      code: otpCode.value,
    })
    auth.setUser(result.user)
    auth.hydrateAccess(result.access)
    finishLogin()
  } catch (e) {
    error.value = 'Invalid code. Please try again.'
    otpCode.value = ''
  } finally {
    loading.value = false
  }
}

// Auto-submit when 6 digits entered
watch(otpCode, v => {
  if (v.length === 6) submitOTP()
})

function backToLogin() {
  totpRequired.value = false
  totpToken.value    = ''
  otpCode.value      = ''
  error.value        = ''
}
</script>

<template>
  <div class="login-page" :style="{ background: bgColor }">
    <!-- Hex pattern background — uses sidebar theme colors -->
    <div class="hex-bg" :style="{ backgroundImage: hexPatternSvg }" aria-hidden="true"></div>

    <div class="login-card">
      <div class="login-header">
        <img :src="branding.logo" :alt="branding.company" class="login-logo" />
        <h1 class="login-title">{{ branding.product }}</h1>
        <p class="login-sub">{{ branding.company }} {{ branding.tagline }}</p>
      </div>

      <!-- Step 1: username + password -->
      <form v-if="!totpRequired" @submit.prevent="submit" class="login-form">
        <div class="field">
          <label for="username">Username</label>
          <input
            id="username"
            v-model="username"
            type="text"
            autocomplete="username"
            placeholder="your username"
            required
          />
        </div>
        <div class="field">
          <label for="password">Password</label>
          <input
            id="password"
            v-model="password"
            type="password"
            autocomplete="current-password"
            placeholder="••••••••"
            required
          />
        </div>
        <div v-if="error" class="login-error">{{ error }}</div>
        <div v-else-if="ssoError" class="login-error">{{ ssoError }}</div>
        <button type="submit" class="btn btn-primary login-btn" :disabled="loading">
          {{ loading ? 'Signing in…' : 'Sign in' }}
        </button>
        <a v-if="ssoEnabled" href="/api/auth/oidc/login" class="btn btn-ghost login-btn login-sso-btn">
          {{ ssoLabel }}
        </a>
        <RouterLink to="/forgot" class="login-forgot-link">Forgot password?</RouterLink>
      </form>

      <!-- Step 2: OTP code -->
      <div v-else class="login-form">
        <div class="totp-info">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="color: var(--bp-blue); margin: 0 auto 0.75rem; display:block">
            <rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 018 0v4"/>
          </svg>
          <p class="totp-label">Two-factor authentication</p>
          <p class="totp-sub">Enter the 6-digit code from your authenticator app.</p>
        </div>
        <div class="field">
          <label for="otp">Authentication code</label>
          <input
            id="otp"
            v-model="otpCode"
            type="text"
            inputmode="numeric"
            pattern="[0-9]*"
            maxlength="6"
            autocomplete="one-time-code"
            placeholder="000000"
            class="otp-input"
            autofocus
          />
        </div>
        <div v-if="error" class="login-error">{{ error }}</div>
        <button class="btn btn-primary login-btn" :disabled="loading || otpCode.length !== 6" @click="submitOTP">
          {{ loading ? 'Verifying…' : 'Verify' }}
        </button>
        <button class="btn btn-ghost login-btn-back" @click="backToLogin">← Back to login</button>
      </div>

      <footer class="login-footer">
        <img :src="branding.logo" alt="" class="footer-logo" aria-hidden="true" />
        <span>{{ branding.company }}</span>
        <span class="footer-sep">·</span>
        <span>v{{ version }}</span>
        <span class="footer-sep">·</span>
        <a href="https://github.com/PAIMOS/paimos" target="_blank" rel="noopener" class="footer-gh" title="GitHub">
          <AppIcon name="github" :size="12" />
        </a>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  overflow: hidden;
}

/* Diamond/hex pattern — same as sidebar */
.hex-bg {
  position: absolute;
  inset: 0;
  background-size: 28px 49px;
  opacity: 1;
}

.login-card {
  position: relative;
  background: var(--bg-card);
  border-radius: 10px;
  box-shadow: var(--shadow-md), 0 0 0 1px rgba(0,0,0,.08);
  padding: 2.5rem 2.25rem 2rem;
  width: 100%;
  max-width: 360px;
}

.login-header {
  text-align: center;
  margin-bottom: 2rem;
}
.login-logo {
  width: 52px;
  height: 52px;
  object-fit: contain;
  margin-bottom: .75rem;
}
.login-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: -.02em;
}
.login-sub {
  font-size: 13px;
  color: var(--text-muted);
  margin-top: .2rem;
}

.login-form { display: flex; flex-direction: column; gap: 1rem; }

.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }

.login-error {
  background: #fde8e8;
  color: #c0392b;
  border-radius: var(--radius);
  padding: .5rem .75rem;
  font-size: 13px;
}

.login-btn { width: 100%; justify-content: center; padding: .65rem; font-size: 14px; margin-top: .25rem; }
.login-sso-btn { display: inline-flex; align-items: center; text-decoration: none; }
.login-btn-back { width: 100%; justify-content: center; font-size: 12px; color: var(--text-muted); margin-top: .25rem; }
.login-forgot-link {
  align-self: center;
  font-size: 12px;
  color: var(--text-muted);
  text-decoration: none;
  margin-top: .1rem;
}
.login-forgot-link:hover { color: var(--bp-blue); text-decoration: underline; }

.totp-info { text-align: center; margin-bottom: .5rem; }
.totp-label { font-size: 15px; font-weight: 700; color: var(--text); margin-bottom: .3rem; }
.totp-sub   { font-size: 13px; color: var(--text-muted); line-height: 1.5; }

.otp-input {
  text-align: center;
  font-size: 28px;
  font-weight: 700;
  letter-spacing: .35em;
  font-family: monospace;
  padding: .65rem;
}

.login-footer {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: .4rem;
  margin-top: 1.75rem;
  color: #b0bec8;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: .08em;
  text-transform: uppercase;
}
.footer-logo {
  width: 16px;
  height: 16px;
  object-fit: contain;
  opacity: .35;
}
.footer-sep { opacity: .4; }
.footer-gh { color: #b0bec8; opacity: .6; transition: opacity .15s; display: inline-flex; }
.footer-gh:hover { opacity: 1; }
</style>
