<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { api, csrfHeaders, errMsg } from '@/api/client'
import { MAX_IMAGE_SIZE } from '@/utils/constants'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import AppModal from '@/components/AppModal.vue'
import AppIcon from '@/components/AppIcon.vue'

const auth = useAuthStore()
const { confirm } = useConfirm()

// ── Profile ──────────────────────────────────────────────────────────────────
const profileForm = ref({
  first_name: '', last_name: '', email: '',
  markdown_default: false,
  monospace_fields: false,
  recent_projects_limit: 3,
  recent_timers_limit: 5,
  preview_hover_delay: 1000,
  timezone: 'auto',
  show_alt_unit_table: false,
  show_alt_unit_detail: false,
  accruals_stats_enabled: false,
})
const profileSaving = ref(false)
const profileError  = ref('')
const profileOk     = ref(false)

const avatarUploading = ref(false)
const avatarError     = ref('')
const avatarInputRef  = ref<HTMLInputElement | null>(null)

function initProfileForm() {
  if (!auth.user) return
  profileForm.value = {
    first_name:       auth.user.first_name ?? '',
    last_name:        auth.user.last_name  ?? '',
    email:            auth.user.email      ?? '',
    markdown_default: auth.user.markdown_default ?? false,
    monospace_fields: auth.user.monospace_fields ?? false,
    recent_projects_limit: auth.user.recent_projects_limit ?? 3,
    recent_timers_limit: auth.user.recent_timers_limit ?? 5,
    preview_hover_delay: auth.user.preview_hover_delay ?? 1000,
    timezone: auth.user.timezone ?? 'auto',
    show_alt_unit_table: auth.user.show_alt_unit_table ?? false,
    show_alt_unit_detail: auth.user.show_alt_unit_detail ?? false,
    accruals_stats_enabled: auth.user.accruals_stats_enabled ?? false,
  }
}

async function saveProfile() {
  profileError.value = ''; profileOk.value = false
  profileSaving.value = true
  try {
    const updated = await api.patch<typeof auth.user>('/auth/me', profileForm.value)
    auth.user = updated as any
    profileOk.value = true
  } catch (e: unknown) {
    profileError.value = errMsg(e, 'Failed to save profile.')
  } finally { profileSaving.value = false }
}

async function uploadAvatar(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (file.size > MAX_IMAGE_SIZE) { avatarError.value = 'Image must be smaller than 3 MB.'; return }
  avatarError.value = ''
  avatarUploading.value = true
  const fd = new FormData()
  fd.append('avatar', file)
  try {
    const res = await fetch('/api/auth/avatar', { method: 'POST', body: fd, credentials: 'same-origin', headers: csrfHeaders() })
    if (!res.ok) { const d = await res.json(); throw new Error(d.error ?? 'Upload failed.') }
    await auth.refreshMe()
  } catch (e: unknown) {
    avatarError.value = errMsg(e, 'Upload failed.')
  } finally {
    avatarUploading.value = false
    if (avatarInputRef.value) avatarInputRef.value.value = ''
  }
}

async function removeAvatar() {
  if (!await confirm({ message: 'Remove your avatar?', confirmLabel: 'Remove' })) return
  avatarError.value = ''
  try {
    await api.delete('/auth/avatar')
    await auth.refreshMe()
  } catch (e: unknown) {
    avatarError.value = errMsg(e, 'Failed to remove avatar.')
  }
}

// ── Change password ──────────────────────────────────────────────────────────
const pwForm   = ref({ current: '', next: '', confirm: '' })
const pwError  = ref('')
const pwOk     = ref(false)
const pwSaving = ref(false)

async function changePassword() {
  pwError.value = ''; pwOk.value = false
  if (!pwForm.value.current || !pwForm.value.next) { pwError.value = 'All fields required.'; return }
  if (pwForm.value.next.length < 6) { pwError.value = 'New password must be at least 6 characters.'; return }
  if (pwForm.value.next !== pwForm.value.confirm) { pwError.value = 'New passwords do not match.'; return }
  pwSaving.value = true
  try {
    await api.post('/auth/password', { current_password: pwForm.value.current, new_password: pwForm.value.next })
    pwOk.value = true
    pwForm.value = { current: '', next: '', confirm: '' }
  } catch (e: unknown) {
    pwError.value = errMsg(e, 'Failed to change password.')
  } finally { pwSaving.value = false }
}

// ── 2FA / TOTP ───────────────────────────────────────────────────────────────
const totpEnabled      = ref(false)
const totpSetupOpen    = ref(false)
const totpQR           = ref('')
const totpSecret       = ref('')
const totpCode         = ref('')
const totpCodeError    = ref('')
const totpSaving       = ref(false)
const totpDisableOpen  = ref(false)
const totpDisablePw    = ref('')
const totpDisableError = ref('')
const totpDisabling    = ref(false)

async function loadTOTPStatus() {
  try {
    totpEnabled.value = (await api.get<{ enabled: boolean }>('/auth/totp/status')).enabled
    auth.setTOTPEnabled(totpEnabled.value)
  } catch {}
}
async function openTOTPSetup() {
  totpCodeError.value = ''; totpCode.value = ''
  try {
    const r = await api.get<{ secret: string; qr_png_base64: string }>('/auth/totp/setup')
    totpSecret.value = r.secret; totpQR.value = r.qr_png_base64; totpSetupOpen.value = true
  } catch (e: unknown) { totpCodeError.value = errMsg(e) }
}
async function enableTOTP() {
  if (totpCode.value.length !== 6) return
  totpCodeError.value = ''; totpSaving.value = true
  try {
    await api.post('/auth/totp/enable', { code: totpCode.value })
    totpEnabled.value = true; totpSetupOpen.value = false; totpCode.value = ''; totpQR.value = ''
    auth.setTOTPEnabled(true)
  } catch { totpCodeError.value = 'Invalid code — try again.'; totpCode.value = '' }
  finally { totpSaving.value = false }
}
async function disableTOTP() {
  totpDisableError.value = ''; totpDisabling.value = true
  try {
    await api.post('/auth/totp/disable', { password: totpDisablePw.value })
    totpEnabled.value = false; totpDisableOpen.value = false; totpDisablePw.value = ''
    auth.setTOTPEnabled(false)
  } catch { totpDisableError.value = 'Incorrect password.' }
  finally { totpDisabling.value = false }
}

// ── API Keys ─────────────────────────────────────────────────────────────────
interface APIKey { id: number; name: string; key_prefix: string; created_at: string; last_used_at: string | null }
const apiKeys        = ref<APIKey[]>([])
const newKeyName     = ref('')
const newKeyCreating = ref(false)
const newKeyError    = ref('')
const newKeyResult   = ref<{ key: string } | null>(null)
const newKeyInputRef = ref<HTMLInputElement | null>(null)

async function loadAPIKeys() {
  try { apiKeys.value = await api.get<APIKey[]>('/auth/api-keys') } catch {}
}
async function createAPIKey() {
  newKeyError.value = ''
  if (!newKeyName.value.trim()) { newKeyError.value = 'Name required.'; return }
  newKeyCreating.value = true
  try {
    const r = await api.post<{ id: number; name: string; key_prefix: string; key: string }>('/auth/api-keys', { name: newKeyName.value.trim() })
    apiKeys.value.unshift({ id: r.id, name: r.name, key_prefix: r.key_prefix, created_at: new Date().toISOString().slice(0,19).replace('T',' '), last_used_at: null })
    newKeyResult.value = { key: r.key }
    newKeyName.value = ''
    await nextTick(); newKeyInputRef.value?.select()
  } catch (e: unknown) { newKeyError.value = errMsg(e, 'Failed.') }
  finally { newKeyCreating.value = false }
}
async function revokeAPIKey(id: number) {
  if (!await confirm({ message: 'Revoke this API key? Any integrations using it will stop working immediately.', confirmLabel: 'Revoke', danger: true })) return
  await api.delete(`/auth/api-keys/${id}`)
  apiKeys.value = apiKeys.value.filter(k => k.id !== id)
}
async function copyKey(key: string) { await navigator.clipboard.writeText(key) }

// ── Init ─────────────────────────────────────────────────────────────────────
async function init() {
  initProfileForm()
  await loadTOTPStatus()
  await loadAPIKeys()
}
init()
</script>

<template>
  <!-- Profile section -->
  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Profile</h2>
      <p class="section-desc">Your display name, contact info, and avatar shown in the sidebar.</p>
    </div>

    <div class="card profile-card">
      <!-- Avatar column -->
      <div class="profile-avatar-col">
        <div class="profile-avatar-wrap">
          <div v-if="auth.user?.avatar_path" class="profile-avatar-img-wrap">
            <img :src="auth.user.avatar_path" class="profile-avatar-img" alt="Avatar" />
          </div>
          <div v-else class="profile-avatar-placeholder">
            {{ (auth.user?.nickname || auth.user?.username || '?').slice(0, 3).toUpperCase() }}
          </div>
        </div>
        <div class="profile-avatar-actions">
          <input ref="avatarInputRef" type="file" accept="image/jpeg,image/png" class="profile-avatar-file" @change="uploadAvatar" :disabled="avatarUploading" />
          <button class="btn btn-ghost btn-sm" :disabled="avatarUploading" @click="avatarInputRef?.click()">
            {{ avatarUploading ? 'Uploading…' : (auth.user?.avatar_path ? 'Change' : 'Upload') }}
          </button>
          <button v-if="auth.user?.avatar_path" class="btn btn-ghost btn-sm danger" @click="removeAvatar">Remove</button>
        </div>
        <p class="profile-avatar-hint">JPG or PNG, max 3MB. Resized to 500x500.</p>
        <div v-if="avatarError" class="form-error">{{ avatarError }}</div>
      </div>

      <!-- Fields column -->
      <div class="profile-fields-col">
        <!-- Username — read-only, shown for reference -->
        <div class="field">
          <label>Username <span class="field-hint">(login name — contact an admin to change)</span></label>
          <input :value="auth.user?.username" readonly class="profile-input-readonly" />
        </div>
        <div class="field field-hint-only">
          <label>Nickname</label>
          <p class="field-hint-text">{{ auth.user?.nickname ? `"${auth.user.nickname}"` : 'Not set' }} — managed by an admin via Settings > Users.</p>
        </div>
        <div class="field-row">
          <div class="field">
            <label>First name</label>
            <input v-model="profileForm.first_name" placeholder="Brad" />
          </div>
          <div class="field">
            <label>Last name</label>
            <input v-model="profileForm.last_name" placeholder="Poet" />
          </div>
        </div>
        <div class="field">
          <label>Email</label>
          <input v-model="profileForm.email" type="email" placeholder="brad@paimos.com" />
        </div>

        <div class="section-divider prefs-divider">Editor preferences</div>
        <div class="prefs-grid">

        <div class="pref-toggle-row">
          <label class="pref-toggle-label" for="pref-markdown">
            <span class="pref-toggle-title">Render long text fields in Markdown</span>
            <span class="pref-toggle-desc">Descriptions, notes, acceptance criteria and comments render as formatted Markdown.</span>
          </label>
          <button
            id="pref-markdown"
            type="button"
            :class="['toggle-btn', { 'toggle-btn--on': profileForm.markdown_default }]"
            @click="profileForm.markdown_default = !profileForm.markdown_default"
            :aria-pressed="profileForm.markdown_default"
          >
            <span class="toggle-thumb" />
          </button>
        </div>

        <div class="pref-toggle-row">
          <label class="pref-toggle-label" for="pref-mono">
            <span class="pref-toggle-title">Use monospace font for long text fields</span>
            <span class="pref-toggle-desc">Apply <code>DM Mono</code> font to descriptions, notes, criteria and comments.</span>
          </label>
          <button
            id="pref-mono"
            type="button"
            :class="['toggle-btn', { 'toggle-btn--on': profileForm.monospace_fields }]"
            @click="profileForm.monospace_fields = !profileForm.monospace_fields"
            :aria-pressed="profileForm.monospace_fields"
          >
            <span class="toggle-thumb" />
          </button>
        </div>

        <div class="section-divider">Duration display</div>

        <div class="pref-toggle-row">
          <label class="pref-toggle-label" for="pref-alt-table">
            <span class="pref-toggle-title">Show alternative unit in tables</span>
            <span class="pref-toggle-desc">Display both h and PT in issue list columns (e.g. "40h (= 5 PT)").</span>
          </label>
          <button
            id="pref-alt-table"
            type="button"
            :class="['toggle-btn', { 'toggle-btn--on': profileForm.show_alt_unit_table }]"
            @click="profileForm.show_alt_unit_table = !profileForm.show_alt_unit_table"
            :aria-pressed="profileForm.show_alt_unit_table"
          >
            <span class="toggle-thumb" />
          </button>
        </div>

        <div class="pref-toggle-row">
          <label class="pref-toggle-label" for="pref-alt-detail">
            <span class="pref-toggle-title">Show alternative unit in detail views</span>
            <span class="pref-toggle-desc">Display both h and PT in issue detail metadata and billing summary.</span>
          </label>
          <button
            id="pref-alt-detail"
            type="button"
            :class="['toggle-btn', { 'toggle-btn--on': profileForm.show_alt_unit_detail }]"
            @click="profileForm.show_alt_unit_detail = !profileForm.show_alt_unit_detail"
            :aria-pressed="profileForm.show_alt_unit_detail"
          >
            <span class="toggle-thumb" />
          </button>
        </div>

        <div class="field">
          <label>Recent projects in sidebar <span class="label-hint">— how many recently visited projects to show (0–10)</span></label>
          <input
            v-model.number="profileForm.recent_projects_limit"
            type="number" min="0" max="10" step="1"
            style="width: 72px;"
          />
        </div>

        <div class="field">
          <label>Recent timers in popover <span class="label-hint">— how many recently stopped timers to show (0–20)</span></label>
          <input
            v-model.number="profileForm.recent_timers_limit"
            type="number" min="0" max="20" step="1"
            style="width: 72px;"
          />
        </div>

        <div class="field">
          <label>Preview hover delay <span class="label-hint">— milliseconds before issue preview shows on hover. Hold Shift to always preview instantly.</span></label>
          <select v-model.number="profileForm.preview_hover_delay" style="width: 120px;">
            <option :value="0">Instant</option>
            <option :value="500">500ms</option>
            <option :value="1000">1000ms (default)</option>
            <option :value="2000">2000ms</option>
          </select>
        </div>

        <div v-if="auth.user?.role === 'admin'" class="section-divider" style="grid-column:1/-1">Reports</div>

        <div v-if="auth.user?.role === 'admin'" class="pref-toggle-row" style="grid-column:1/-1">
          <label class="pref-toggle-label" for="pref-accruals">
            <span class="pref-toggle-title">Vorräte / Projektabgrenzungen anzeigen</span>
            <span class="pref-toggle-desc">Blendet AR-Stundensummen pro Status auf den Projektkarten ein, inklusive TSV-Kopie und druckbarer Bilanz. Nur für Admins.</span>
          </label>
          <button
            id="pref-accruals"
            type="button"
            :class="['toggle-btn', { 'toggle-btn--on': profileForm.accruals_stats_enabled }]"
            @click="profileForm.accruals_stats_enabled = !profileForm.accruals_stats_enabled"
            :aria-pressed="profileForm.accruals_stats_enabled"
          >
            <span class="toggle-thumb" />
          </button>
        </div>

        <div class="field">
          <label>Time display timezone <span class="label-hint">— how timestamps are displayed across the app</span></label>
          <select v-model="profileForm.timezone" style="width: 200px;">
            <option value="auto">Browser local (auto)</option>
            <option value="UTC">UTC</option>
            <option value="Europe/Vienna">Europe/Vienna</option>
            <option value="Europe/Berlin">Europe/Berlin</option>
            <option value="Europe/London">Europe/London</option>
            <option value="America/New_York">America/New_York</option>
            <option value="America/Los_Angeles">America/Los_Angeles</option>
            <option value="Asia/Tokyo">Asia/Tokyo</option>
          </select>
        </div>

        </div><!-- /prefs-grid -->

        <div v-if="profileError" class="form-error">{{ profileError }}</div>
        <div v-if="profileOk" class="ok-banner">Profile saved.</div>
        <div class="form-actions">
          <button class="btn btn-primary btn-sm" :disabled="profileSaving" @click="saveProfile">
            {{ profileSaving ? 'Saving…' : 'Save profile' }}
          </button>
        </div>
      </div>
    </div>
  </div>

  <div id="two-factor-authentication" class="section">
    <div class="section-header">
      <h2 class="section-title">Two-Factor Authentication</h2>
      <p class="section-desc">Secure your account with a TOTP authenticator app (Google Authenticator, Authy, 1Password…).</p>
    </div>
    <div class="card card-row">
      <div class="totp-status-indicator" :class="totpEnabled ? 'totp-on' : 'totp-off'">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 018 0v4"/></svg>
        {{ totpEnabled ? '2FA enabled' : '2FA disabled' }}
      </div>
      <button v-if="!totpEnabled" class="btn btn-primary btn-sm" @click="openTOTPSetup">Enable 2FA</button>
      <button v-else class="btn btn-danger btn-sm" @click="totpDisableOpen=true; totpDisablePw=''; totpDisableError=''">Disable 2FA</button>
    </div>
  </div>

  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Change Password</h2>
      <p class="section-desc">Update your account password. You must know your current password.</p>
    </div>
    <div class="card" style="max-width:360px">
      <div class="field"><label>Current password</label>
        <input v-model="pwForm.current" type="password" autocomplete="current-password" placeholder="••••••••" />
      </div>
      <div class="field"><label>New password</label>
        <input v-model="pwForm.next" type="password" autocomplete="new-password" placeholder="••••••••" />
      </div>
      <div class="field"><label>Confirm new password</label>
        <input v-model="pwForm.confirm" type="password" autocomplete="new-password" placeholder="••••••••" @keyup.enter="changePassword" />
      </div>
      <div v-if="pwError" class="form-error">{{ pwError }}</div>
      <div v-if="pwOk" class="ok-banner">Password changed successfully.</div>
      <div class="form-actions" style="margin-top:.75rem">
        <button class="btn btn-primary btn-sm" :disabled="pwSaving" @click="changePassword">{{ pwSaving ? 'Saving…' : 'Change password' }}</button>
      </div>
    </div>
  </div>

  <div class="section">
    <div class="section-header">
      <h2 class="section-title">API Keys</h2>
      <p class="section-desc">Long-lived tokens for scripts and agents. Use <code class="icode">Authorization: Bearer &lt;key&gt;</code>. Shown once on creation.</p>
    </div>
    <div class="apikey-create-row">
      <input v-model="newKeyName" type="text" placeholder="Key name, e.g. ci-script" class="apikey-name-input" @keyup.enter="createAPIKey" />
      <button class="btn btn-primary btn-sm" :disabled="newKeyCreating" @click="createAPIKey">{{ newKeyCreating ? 'Creating…' : '+ Create key' }}</button>
    </div>
    <div v-if="newKeyError" class="form-error" style="margin-bottom:.5rem">{{ newKeyError }}</div>
    <div v-if="newKeyResult" class="apikey-reveal">
      <span class="apikey-reveal-label">Copy now — shown only once:</span>
      <div class="apikey-reveal-row">
        <input ref="newKeyInputRef" type="text" readonly :value="newKeyResult.key" class="apikey-reveal-input" @click="($event.target as HTMLInputElement).select()" />
        <button class="btn btn-ghost btn-sm" @click="copyKey(newKeyResult!.key)" title="Copy">
          <AppIcon name="copy" :size="13" />
        </button>
        <button class="btn btn-ghost btn-sm" @click="newKeyResult=null"><AppIcon name="x" :size="13" /></button>
      </div>
    </div>
    <div v-if="apiKeys.length > 0" class="card" style="padding:0;overflow:hidden;margin-top:.25rem">
      <table class="settings-table">
        <thead><tr><th>Name</th><th>Prefix</th><th>Created</th><th>Last used</th><th></th></tr></thead>
        <tbody>
          <tr v-for="k in apiKeys" :key="k.id">
            <td class="fw500">{{ k.name }}</td>
            <td><code class="icode">{{ k.key_prefix }}…</code></td>
            <td class="muted">{{ k.created_at.slice(0,10) }}</td>
            <td class="muted">{{ k.last_used_at ? k.last_used_at.slice(0,10) : '—' }}</td>
            <td class="actions-cell"><button class="btn btn-ghost btn-sm danger" @click="revokeAPIKey(k.id)">Revoke</button></td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else-if="!newKeyResult" class="empty-hint">No API keys yet.</p>
  </div>

  <!-- ── Modals ──────────────────────────────────────────────────────────── -->
  <AppModal title="Enable Two-Factor Authentication" :open="totpSetupOpen" @close="totpSetupOpen=false; totpCode=''" max-width="460px">
    <div class="totp-setup">
      <p class="totp-setup-step">1. Scan this QR code with your authenticator app.</p>
      <div class="totp-qr-wrap"><img v-if="totpQR" :src="`data:image/png;base64,${totpQR}`" alt="TOTP QR" class="totp-qr" /></div>
      <p class="totp-setup-or">Or enter this secret manually:</p>
      <code class="totp-secret">{{ totpSecret }}</code>
      <p class="totp-setup-step" style="margin-top:1.25rem">2. Enter the 6-digit code from your app.</p>
      <input v-model="totpCode" type="text" inputmode="numeric" pattern="[0-9]*" maxlength="6" placeholder="000000" class="otp-input" autofocus @keyup.enter="enableTOTP" />
      <div v-if="totpCodeError" class="form-error" style="margin-top:.5rem">{{ totpCodeError }}</div>
      <div class="form-actions" style="margin-top:1rem">
        <button class="btn btn-ghost" @click="totpSetupOpen=false; totpCode=''">Cancel</button>
        <button class="btn btn-primary" :disabled="totpCode.length!==6||totpSaving" @click="enableTOTP">{{ totpSaving ? 'Verifying…' : 'Enable 2FA' }}</button>
      </div>
    </div>
  </AppModal>

  <AppModal title="Disable Two-Factor Authentication" :open="totpDisableOpen" @close="totpDisableOpen=false" confirm-key="d" @confirm="disableTOTP">
    <p style="font-size:14px;color:var(--text);margin-bottom:1rem">Enter your password to confirm disabling 2FA.</p>
    <div class="field"><label>Password</label>
      <input v-model="totpDisablePw" type="password" autocomplete="current-password" placeholder="••••••••" @keyup.enter="disableTOTP" />
    </div>
    <div v-if="totpDisableError" class="form-error" style="margin-top:.5rem">{{ totpDisableError }}</div>
    <div class="form-actions" style="margin-top:1rem">
      <button class="btn btn-ghost" @click="totpDisableOpen=false"><u>C</u>ancel</button>
      <button class="btn btn-danger" :disabled="!totpDisablePw||totpDisabling" @click="disableTOTP"><template v-if="totpDisabling">Disabling…</template><template v-else><u>D</u>isable 2FA</template></button>
    </div>
  </AppModal>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
/* ── Profile ─────────────────────────────────────────────────────────────── */
.profile-card { display: flex; gap: 2rem; align-items: flex-start; flex-wrap: wrap; }
.profile-avatar-col { display: flex; flex-direction: column; align-items: center; gap: .6rem; min-width: 100px; }
.profile-avatar-wrap { position: relative; }
.profile-avatar-img-wrap {
  width: 72px; height: 72px; border-radius: 50%; overflow: hidden;
  border: 1px solid rgba(0,0,0,.35);
  box-shadow: 0 0 0 1px rgba(0,0,0,.35);
  flex-shrink: 0;
}
.profile-avatar-img { width: 100%; height: 100%; object-fit: cover; display: block; }
.profile-avatar-placeholder {
  width: 72px; height: 72px; border-radius: 50%;
  background: var(--bp-blue); color: #fff;
  display: flex; align-items: center; justify-content: center;
  font-size: 22px; font-weight: 700;
}
.profile-avatar-file { display: none; }
.profile-avatar-actions { display: flex; gap: .4rem; }
.profile-avatar-hint { font-size: 11px; color: var(--text-muted); text-align: center; max-width: 110px; line-height: 1.4; }
.profile-fields-col { flex: 1; min-width: 260px; display: flex; flex-direction: column; gap: .75rem; }
.prefs-divider { grid-column: 1 / -1; }
.prefs-grid {
  display: grid; grid-template-columns: 1fr; gap: .75rem;
}
@media (min-width: 900px) {
  .prefs-grid { grid-template-columns: 1fr 1fr; }
  .prefs-grid .section-divider { grid-column: 1 / -1; }
}
.field-row { display: flex; gap: 1rem; }
.field-row .field { flex: 1; }
.profile-input-readonly {
  background: var(--bg) !important;
  color: var(--text-muted) !important;
  cursor: default;
  opacity: .75;
}
.field-hint { font-size: 11px; font-weight: 400; color: var(--text-muted); }
.field-hint-text { font-size: 12px; color: var(--text-muted); margin: 0; }

/* ── 2FA ─────────────────────────────────────────────────────────────────── */
.totp-status-indicator { display: inline-flex; align-items: center; gap: .45rem; font-size: 13px; font-weight: 600; padding: .3rem .7rem; border-radius: 20px; }
.totp-on  { background: #d4edda; color: #155724; }
.totp-off { background: #e9ecef; color: #495057; }
.totp-setup { display: flex; flex-direction: column; gap: .5rem; }
.totp-setup-step { font-size: 13px; font-weight: 600; color: var(--text); }
.totp-setup-or   { font-size: 12px; color: var(--text-muted); text-align: center; }
.totp-qr-wrap    { display: flex; justify-content: center; padding: .75rem 0; }
.totp-qr         { width: 180px; height: 180px; border-radius: 8px; border: 1px solid var(--border); }
.totp-secret     { display: block; text-align: center; font-family: monospace; font-size: 13px; font-weight: 700; letter-spacing: .12em; color: var(--text); background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius); padding: .5rem .75rem; }
.otp-input       { text-align: center; font-size: 22px; font-weight: 700; letter-spacing: .3em; font-family: monospace; padding: .5rem; width: 100%; }

/* ── API Keys ────────────────────────────────────────────────────────────── */
.apikey-create-row { display: flex; align-items: center; gap: .65rem; margin-bottom: .5rem; }
.apikey-name-input { flex: 1; max-width: 280px; font-size: 13px; }
.apikey-reveal { background: #fffbea; border: 1px solid #f6d860; border-radius: var(--radius); padding: .75rem 1rem; margin-bottom: .5rem; display: flex; flex-direction: column; gap: .4rem; }
.apikey-reveal-label { font-size: 12px; font-weight: 600; color: #856404; }
.apikey-reveal-row { display: flex; align-items: center; gap: .5rem; }
.apikey-reveal-input { flex: 1; font-family: 'DM Mono','Fira Code',monospace; font-size: 12px; background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius); padding: .35rem .6rem; color: var(--text); }

/* ── Editor preference toggles ────────────────────────────────────────────── */
.section-divider {
  font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .08em;
  color: var(--text-muted); margin: 1.25rem 0 .75rem;
  border-top: 1px solid var(--border); padding-top: .75rem;
}
.pref-toggle-row {
  display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem;
  padding: .6rem 0; border-bottom: 1px solid var(--border);
}
.pref-toggle-row:last-of-type { border-bottom: none; }
.pref-toggle-label { flex: 1; cursor: pointer; }
.pref-toggle-title { display: block; font-size: 13px; font-weight: 500; color: var(--text); margin-bottom: .15rem; }
.pref-toggle-desc  { display: block; font-size: 12px; color: var(--text-muted); line-height: 1.5; }
.pref-toggle-desc code { font-family: 'DM Mono', monospace; font-size: 11px; background: var(--bg); padding: .05rem .3rem; border-radius: 3px; }

.toggle-btn {
  position: relative; flex-shrink: 0;
  width: 36px; height: 20px;
  background: var(--border); border: none; border-radius: 99px;
  cursor: pointer; transition: background .2s; padding: 0;
  margin-top: .1rem;
}
.toggle-btn--on { background: var(--bp-blue); }
.toggle-thumb {
  position: absolute; top: 3px; left: 3px;
  width: 14px; height: 14px; border-radius: 50%;
  background: #fff; box-shadow: 0 1px 3px rgba(0,0,0,.25);
  transition: transform .2s;
  display: block;
}
.toggle-btn--on .toggle-thumb { transform: translateX(16px); }
</style>
