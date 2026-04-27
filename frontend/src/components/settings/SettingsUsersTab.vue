<script setup lang="ts">
import { ref, computed } from 'vue'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useSort } from '@/composables/useSort'
import AppModal from '@/components/AppModal.vue'
import AppIcon from '@/components/AppIcon.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import type { User } from '@/types'

const auth = useAuthStore()

const ROLE_OPTIONS: MetaOption[] = [{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }, { value: 'external', label: 'External' }]

const users          = ref<User[]>([])
const usersLoaded    = ref(false)
const showCreateUser = ref(false)
const createUserForm  = ref({ username: '', password: '', role: 'member' })
const createUserError = ref('')
const creatingUser    = ref(false)
const editUserTarget  = ref<User | null>(null)
const editUserForm    = ref({ username: '', nickname: '', email: '', password: '', role: 'member', internal_rate_hourly: null as number | null, locale: 'en' })
const editUserError   = ref('')
const updatingUser    = ref(false)
const resetTotpConfirm = ref(false)
const resetTotpLoading = ref(false)
const disableUserTarget = ref<User | null>(null)
const disablingUser     = ref(false)
const deleteUserTarget  = ref<User | null>(null)
const deletingUser      = ref(false)
const isSelf = (u: User) => u.id === auth.user?.id

// Inline nickname editing
const nickEditId  = ref<number | null>(null)
const nickEditVal = ref('')
function startNickEdit(u: User) {
  nickEditId.value  = u.id
  nickEditVal.value = u.nickname || ''
}
function cancelNickEdit() { nickEditId.value = null }
async function saveNick(u: User) {
  const val = nickEditVal.value.trim().slice(0, 3)
  nickEditId.value = null
  if (val === (u.nickname || '')) return
  try {
    const updated = await api.put<User>(`/users/${u.id}`, { nickname: val })
    const idx = users.value.findIndex(x => x.id === u.id)
    if (idx !== -1) users.value[idx] = updated
  } catch { /* ignore */ }
}

function relativeTime(ts: string): string {
  const diff = Date.now() - new Date(ts).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d ago`
  return `${Math.floor(days / 30)}mo ago`
}

const { sorted: sortedUsers, sortIndicator: userSortInd, thProps: userThProps } = useSort(users, {
  username:   { value: u => u.username,   type: 'string' },
  role:       { value: u => u.role,       type: { order: ['admin','member','external'] } },
  rate:       { value: u => u.internal_rate_hourly ?? 0, type: 'number' },
  created_at: { value: u => u.created_at, type: 'date' },
})

// ── Per-user project access (unified matrix; replaces legacy Projects flow) ─

type AccessLevel = 'none' | 'viewer' | 'editor'
type FilterMode = 'explicit' | 'all'

interface MembershipRow {
  project_id: number
  project_key: string
  project_name: string
  access_level: AccessLevel
}

const ACCESS_LEVELS: { value: AccessLevel; label: string }[] = [
  { value: 'none',   label: 'None'   },
  { value: 'viewer', label: 'Viewer' },
  { value: 'editor', label: 'Editor' },
]

const membershipsTarget   = ref<User | null>(null)
const memberships         = ref<MembershipRow[]>([])
const membershipsLoading  = ref(false)
const filter              = ref<FilterMode>('all')
const addProjectId        = ref<number | null>(null)
const addLevel            = ref<AccessLevel>('viewer')

const isExternal = computed(() => membershipsTarget.value?.role === 'external')
// Role default mirrors backend logic in ListUserMemberships: admin/member → editor, external → none.
const roleDefault = computed<AccessLevel>(() => isExternal.value ? 'none' : 'editor')
const roleDefaultLabel = computed(() => roleDefault.value)
const roleHint = computed(() => {
  if (!membershipsTarget.value) return ''
  return isExternal.value
    ? 'Externals start with no access. Grant per project below.'
    : 'Members default to editor. Use this to override per project.'
})
// Externals can only be None or Viewer; the Editor level is hidden for them.
const visibleLevels = computed(() =>
  isExternal.value ? ACCESS_LEVELS.filter(l => l.value !== 'editor') : ACCESS_LEVELS
)
const explicitRows = computed(() =>
  memberships.value
    .filter(r => r.access_level !== roleDefault.value)
    .slice()
    .sort((a, b) => a.project_key.localeCompare(b.project_key))
)
const defaultRows = computed(() =>
  memberships.value
    .filter(r => r.access_level === roleDefault.value)
    .slice()
    .sort((a, b) => a.project_key.localeCompare(b.project_key))
)
// Pickable in the Add bar = projects without an explicit grant yet. Includes
// defaulted rows: picking one promotes it to the chosen level.
const addableProjects = computed(() => {
  const explicitIds = new Set(explicitRows.value.map(r => r.project_id))
  return memberships.value
    .filter(r => !explicitIds.has(r.project_id))
    .slice()
    .sort((a, b) => a.project_key.localeCompare(b.project_key))
})

async function openMemberships(u: User) {
  membershipsTarget.value = u
  membershipsLoading.value = true
  memberships.value = []
  // External rows are sparse (no seeded grants) — explicit-only is the
  // useful default. Staff defaults to editor everywhere — show all so
  // overrides are visible.
  filter.value = u.role === 'external' ? 'explicit' : 'all'
  addProjectId.value = null
  addLevel.value = 'viewer'
  try {
    memberships.value = await api.get<MembershipRow[]>(`/users/${u.id}/memberships`)
  } catch { /* ignore */ }
  membershipsLoading.value = false
}

async function setMembership(row: MembershipRow, lvl: AccessLevel) {
  if (!membershipsTarget.value) return
  const uid = membershipsTarget.value.id
  const prev = row.access_level
  row.access_level = lvl // optimistic
  try {
    await api.put(`/users/${uid}/memberships/${row.project_id}`, { access_level: lvl })
  } catch {
    row.access_level = prev
  }
}

async function resetMembership(row: MembershipRow) {
  if (!membershipsTarget.value) return
  const uid = membershipsTarget.value.id
  const prev = row.access_level
  try {
    await api.delete(`/users/${uid}/memberships/${row.project_id}`)
    // After deleting the explicit row the backend falls back to the role
    // default — re-read memberships so the row reflects that default.
    memberships.value = await api.get<MembershipRow[]>(`/users/${uid}/memberships`)
  } catch {
    row.access_level = prev
  }
}

async function addRow() {
  if (!membershipsTarget.value || !addProjectId.value) return
  const uid = membershipsTarget.value.id
  const pid = addProjectId.value
  const lvl: AccessLevel = isExternal.value ? 'viewer' : addLevel.value
  const row = memberships.value.find(r => r.project_id === pid)
  const prev = row?.access_level
  if (row) row.access_level = lvl // optimistic
  try {
    await api.put(`/users/${uid}/memberships/${pid}`, { access_level: lvl })
    addProjectId.value = null
  } catch {
    if (row && prev !== undefined) row.access_level = prev
  }
}

async function loadUsers() {
  if (usersLoaded.value) return
  users.value = await api.get<User[]>('/users')
  usersLoaded.value = true
}
function openEditUser(u: User) {
  editUserTarget.value = u
  editUserForm.value = { username: u.username, nickname: u.nickname || '', email: u.email || '', password: '', role: u.role, internal_rate_hourly: u.internal_rate_hourly ?? null, locale: u.locale || 'en' }
  editUserError.value = ''
  resetTotpConfirm.value = false
}
async function createUser() {
  createUserError.value = ''
  if (!createUserForm.value.username || !createUserForm.value.password) { createUserError.value = 'Username and password required.'; return }
  creatingUser.value = true
  try {
    const u = await api.post<User>('/users', createUserForm.value)
    users.value.push(u); showCreateUser.value = false
    createUserForm.value = { username: '', password: '', role: 'member' }
  } catch (e: unknown) { createUserError.value = errMsg(e) }
  finally { creatingUser.value = false }
}
async function updateUser() {
  if (!editUserTarget.value) return
  editUserError.value = ''; updatingUser.value = true
  try {
    const payload: Record<string, string | number | null> = {}
    if (editUserForm.value.username !== editUserTarget.value.username)       payload.username = editUserForm.value.username
    if (editUserForm.value.nickname !== (editUserTarget.value.nickname || '')) payload.nickname = editUserForm.value.nickname
    if (editUserForm.value.email    !== (editUserTarget.value.email    || '')) payload.email    = editUserForm.value.email
    if (editUserForm.value.role     !== editUserTarget.value.role)            payload.role     = editUserForm.value.role
    if (editUserForm.value.password)                                          payload.password  = editUserForm.value.password
    if (editUserForm.value.internal_rate_hourly !== editUserTarget.value.internal_rate_hourly)
      payload.internal_rate_hourly = editUserForm.value.internal_rate_hourly
    if (editUserForm.value.locale !== (editUserTarget.value.locale || 'en'))
      payload.locale = editUserForm.value.locale
    const u = await api.put<User>(`/users/${editUserTarget.value.id}`, payload)
    const idx = users.value.findIndex(x => x.id === u.id)
    if (idx >= 0) users.value[idx] = u
    editUserTarget.value = null
  } catch (e: unknown) { editUserError.value = errMsg(e) }
  finally { updatingUser.value = false }
}
async function resetUserTOTP() {
  if (!editUserTarget.value) return
  resetTotpLoading.value = true
  try {
    await api.post(`/users/${editUserTarget.value.id}/reset-totp`, {})
    const idx = users.value.findIndex(x => x.id === editUserTarget.value!.id)
    if (idx >= 0) users.value[idx] = { ...users.value[idx], totp_enabled: false }
    editUserTarget.value = { ...editUserTarget.value, totp_enabled: false }
    resetTotpConfirm.value = false
  } catch (e: unknown) { editUserError.value = errMsg(e, 'Failed to reset 2FA.') }
  finally { resetTotpLoading.value = false }
}
async function enableUser(u: User) {
  await api.put<User>(`/users/${u.id}`, { status: 'active' })
  const idx = users.value.findIndex(x => x.id === u.id)
  if (idx >= 0) users.value[idx] = { ...users.value[idx], status: 'active' }
}
async function confirmDisableUser() {
  if (!disableUserTarget.value) return
  disablingUser.value = true
  try {
    await api.post(`/users/${disableUserTarget.value.id}/disable`, {})
    const idx = users.value.findIndex(x => x.id === disableUserTarget.value!.id)
    if (idx >= 0) users.value[idx] = { ...users.value[idx], status: 'inactive' }
    disableUserTarget.value = null
  } catch (e: unknown) { /* swallow */ }
  finally { disablingUser.value = false }
}
async function confirmDeleteUser() {
  if (!deleteUserTarget.value) return
  deletingUser.value = true
  try {
    await api.delete(`/users/${deleteUserTarget.value.id}`)
    users.value = users.value.filter(x => x.id !== deleteUserTarget.value!.id)
    deleteUserTarget.value = null
  } catch (e: unknown) { /* swallow */ }
  finally { deletingUser.value = false }
}

// Init
loadUsers()
</script>

<template>
  <div class="section">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">Users</h2>
        <p class="section-desc">Manage team members and their roles.</p>
      </div>
      <button class="btn btn-primary btn-sm" @click="showCreateUser=true">+ New user</button>
    </div>
    <div class="card" style="padding:0;overflow:hidden">
      <table class="settings-table">
        <thead>
          <tr>
            <th v-bind="userThProps('username')">Username <span class="sort-ind"><AppIcon :name="userSortInd('username')" :size="11" /></span></th>
            <th>Nickname</th>
            <th v-bind="userThProps('role')">Role <span class="sort-ind"><AppIcon :name="userSortInd('role')" :size="11" /></span></th>
            <th>Status</th>
            <th v-bind="userThProps('rate')">Rate <span class="sort-ind"><AppIcon :name="userSortInd('rate')" :size="11" /></span></th>
            <th v-bind="userThProps('created_at')">Created <span class="sort-ind"><AppIcon :name="userSortInd('created_at')" :size="11" /></span></th>
            <th>Last Login</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in sortedUsers" :key="u.id" :class="{ 'row-muted': u.status !== 'active' }">
            <td>
              <div class="user-avatar-row">
                <UserAvatar :user="u" size="sm" :class="{ 'ua--inactive': u.status !== 'active' }" />
                <span class="fw500">{{ u.username }}</span>
                <span v-if="isSelf(u)" class="you-badge">you</span>
              </div>
            </td>
            <td class="nick-cell">
              <template v-if="nickEditId === u.id">
                <input
                  class="nick-input"
                  v-model="nickEditVal"
                  maxlength="3"
                  placeholder="abc"
                  @keydown.enter.prevent="saveNick(u)"
                  @keydown.escape.prevent="cancelNickEdit"
                  @blur="saveNick(u)"
                  autofocus
                />
              </template>
              <span v-else class="nick-display" @click="startNickEdit(u)" :title="'Click to edit nickname'">
                {{ u.nickname || '—' }}
              </span>
            </td>
            <td><span class="role-label" :class="`role-${u.role}`">{{ u.role.toUpperCase() }}</span></td>
            <td>
              <span class="badge" :class="{
                'badge-active': u.status === 'active',
                'badge-archived': u.status === 'inactive',
                'badge-deleted': u.status === 'deleted',
              }">{{ u.status }}</span>
            </td>
            <td class="muted">{{ u.internal_rate_hourly != null ? `€${u.internal_rate_hourly}` : '—' }}</td>
            <td class="muted">{{ u.created_at.slice(0,10) }}</td>
            <td class="muted" :title="u.last_login_at ?? ''">{{ u.last_login_at ? relativeTime(u.last_login_at) : 'Never' }}</td>
            <td class="actions-cell">
              <button class="btn btn-ghost btn-sm" @click="openEditUser(u)">Edit</button>
              <button v-if="!isSelf(u)" class="btn btn-ghost btn-sm" @click="openMemberships(u)" title="Per-project access levels">Access</button>
              <template v-if="!isSelf(u)">
                <button v-if="u.status === 'active'" class="btn btn-ghost btn-sm" @click="disableUserTarget=u" title="Disable account">Disable</button>
                <button v-if="u.status === 'inactive'" class="btn btn-ghost btn-sm" @click="enableUser(u)" title="Re-enable account">Enable</button>
                <button class="btn btn-ghost btn-sm danger" @click="deleteUserTarget=u" title="Soft-delete account">Delete</button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <!-- ── Modals ──────────────────────────────────────────────────────────── -->
  <AppModal title="Disable Account" :open="!!disableUserTarget" @close="disableUserTarget=null" confirm-key="d" @confirm="confirmDisableUser">
    <p style="font-size:14px;color:var(--text);margin-bottom:1.25rem">
      Disable <strong>{{ disableUserTarget?.username }}</strong>? They won't be able to log in. All their data is preserved and the account can be re-enabled.
    </p>
    <div class="form-actions">
      <button class="btn btn-ghost" @click="disableUserTarget=null"><u>C</u>ancel</button>
      <button class="btn btn-danger" :disabled="disablingUser" @click="confirmDisableUser"><template v-if="disablingUser">Disabling…</template><template v-else><u>D</u>isable account</template></button>
    </div>
  </AppModal>

  <AppModal title="Delete Account" :open="!!deleteUserTarget" @close="deleteUserTarget=null" confirm-key="d" @confirm="confirmDeleteUser">
    <p style="font-size:14px;color:var(--text);margin-bottom:1.25rem">
      Soft-delete <strong>{{ deleteUserTarget?.username }}</strong>? The account will be hidden from the UI and login will be blocked. All their issues, comments and history are preserved. Restore is possible via database update.
    </p>
    <div class="form-actions">
      <button class="btn btn-ghost" @click="deleteUserTarget=null"><u>C</u>ancel</button>
      <button class="btn btn-danger" :disabled="deletingUser" @click="confirmDeleteUser"><template v-if="deletingUser">Deleting…</template><template v-else><u>D</u>elete account</template></button>
    </div>
  </AppModal>

  <AppModal title="New User" :open="showCreateUser" @close="showCreateUser=false; createUserForm={username:'',password:'',role:'member'}">
    <form @submit.prevent="createUser" class="form">
      <div class="field"><label>Username</label><input v-model="createUserForm.username" type="text" required autofocus /></div>
      <div class="field"><label>Password</label><input v-model="createUserForm.password" type="password" required /></div>
      <div class="field"><label>Role</label><MetaSelect v-model="createUserForm.role" :options="ROLE_OPTIONS" /></div>
      <div v-if="createUserError" class="form-error">{{ createUserError }}</div>
      <div class="form-actions">
        <button type="button" class="btn btn-ghost" @click="showCreateUser=false">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="creatingUser">{{ creatingUser ? 'Creating…' : 'Create user' }}</button>
      </div>
    </form>
  </AppModal>

  <AppModal :title="`Edit ${editUserTarget?.username}`" :open="!!editUserTarget" @close="editUserTarget=null">
    <form @submit.prevent="updateUser" class="form">
      <div class="edit-user-grid">
        <div class="field"><label>Username</label><input v-model="editUserForm.username" type="text" required /></div>
        <div class="field"><label>Nickname <span class="label-hint">— up to 3 chars</span></label>
          <input v-model="editUserForm.nickname" type="text" maxlength="3" placeholder="abc" style="width:80px" />
        </div>
        <div class="field"><label>Email</label><input v-model="editUserForm.email" type="email" placeholder="user@example.com" /></div>
        <div class="field"><label>Role</label><MetaSelect v-model="editUserForm.role" :options="ROLE_OPTIONS" /></div>
        <div class="field"><label>New password <span class="label-hint">— leave blank to keep</span></label>
          <input v-model="editUserForm.password" type="password" autocomplete="new-password" placeholder="••••••••" />
        </div>
        <div class="field"><label>Internal rate <span class="label-hint">— €/h</span></label>
          <input v-model.number="editUserForm.internal_rate_hourly" type="number" step="0.01" min="0" placeholder="e.g. 95.00" style="width:120px" />
        </div>
        <div class="field"><label>Locale</label>
          <select v-model="editUserForm.locale" style="width:120px">
            <option value="en">English</option>
            <option value="de">Deutsch</option>
          </select>
        </div>
      </div>

      <div v-if="editUserTarget?.totp_enabled" class="edit-user-totp">
        <div class="edit-user-totp-info">
          <AppIcon name="shield-check" :size="14" class="totp-icon" />
          <span>2FA is <strong>active</strong> for this user.</span>
        </div>
        <template v-if="!resetTotpConfirm">
          <button type="button" class="btn btn-ghost btn-sm danger" @click="resetTotpConfirm=true">Reset 2FA…</button>
        </template>
        <template v-else>
          <span class="totp-confirm-text">This will disable 2FA for {{ editUserTarget?.username }}. Continue?</span>
          <button type="button" class="btn btn-sm btn-danger" :disabled="resetTotpLoading" @click="resetUserTOTP">{{ resetTotpLoading ? 'Resetting…' : 'Yes, disable 2FA' }}</button>
          <button type="button" class="btn btn-ghost btn-sm" @click="resetTotpConfirm=false">Cancel</button>
        </template>
      </div>

      <div v-if="editUserError" class="form-error">{{ editUserError }}</div>
      <div class="form-actions">
        <button type="button" class="btn btn-ghost" @click="editUserTarget=null">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="updatingUser">{{ updatingUser ? 'Saving…' : 'Save changes' }}</button>
      </div>
    </form>
  </AppModal>

  <!-- Unified per-user project access (matrix + role-aware filter + add picker) -->
  <AppModal :title="`Access: ${membershipsTarget?.username}`" :open="!!membershipsTarget" max-width="680px" @close="membershipsTarget=null">
    <div v-if="membershipsLoading" class="memb-loading">Loading…</div>

    <div v-else class="memb-body">
      <p class="empty-hint memb-hint">{{ roleHint }}</p>

      <div class="memb-toolbar">
        <div class="memb-segmented" role="tablist" aria-label="View">
          <button
            type="button" role="tab"
            class="memb-segmented__opt"
            :class="{ 'is-active': filter === 'explicit' }"
            :aria-selected="filter === 'explicit'"
            @click="filter = 'explicit'"
          >Explicit grants</button>
          <button
            type="button" role="tab"
            class="memb-segmented__opt"
            :class="{ 'is-active': filter === 'all' }"
            :aria-selected="filter === 'all'"
            @click="filter = 'all'"
          >All projects</button>
        </div>

        <div class="memb-add" :class="{ 'is-disabled': addableProjects.length === 0 }">
          <select v-model="addProjectId" class="memb-add__field memb-add__pick" :disabled="addableProjects.length === 0">
            <option :value="null" disabled>{{ addableProjects.length === 0 ? 'No projects to add' : 'Pick a project…' }}</option>
            <option v-for="p in addableProjects" :key="p.project_id" :value="p.project_id">
              {{ p.project_key }} — {{ p.project_name }}
            </option>
          </select>
          <select v-if="!isExternal" v-model="addLevel" class="memb-add__field memb-add__lvl">
            <option value="viewer">Viewer</option>
            <option value="editor">Editor</option>
          </select>
          <span v-else class="memb-add__lvl-locked" title="Externals can only be granted Viewer access">Viewer</span>
          <button
            type="button"
            class="memb-add__btn"
            :disabled="!addProjectId"
            @click="addRow"
          >Add</button>
        </div>
      </div>

      <div v-if="memberships.length === 0" class="memb-empty">
        <span class="memb-empty__icon" aria-hidden="true">⌀</span>
        No projects to manage yet.
      </div>
      <div v-else-if="filter === 'explicit' && explicitRows.length === 0" class="memb-empty">
        <span class="memb-empty__icon" aria-hidden="true">∅</span>
        <span>No explicit grants yet.<br />Use the picker above to assign access.</span>
      </div>

      <div v-else class="memb-table-wrap">
        <table class="settings-table memb-table">
          <thead>
            <tr>
              <th>Project</th>
              <th class="th-access">Access</th>
              <th class="th-reset" aria-label="Reset to default"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in explicitRows" :key="`e-${row.project_id}`">
              <td>
                <span class="project-access-key">{{ row.project_key }}</span>
                <span class="project-access-name">{{ row.project_name }}</span>
              </td>
              <td>
                <div class="memb-lvl-group">
                  <button
                    v-for="lvl in visibleLevels" :key="lvl.value"
                    type="button"
                    class="btn btn-sm"
                    :class="row.access_level === lvl.value ? 'btn-primary' : 'btn-ghost'"
                    @click="setMembership(row, lvl.value)"
                  >{{ lvl.label }}</button>
                </div>
              </td>
              <td>
                <button class="btn btn-ghost btn-sm memb-reset" @click="resetMembership(row)" title="Revert to role default">Reset</button>
              </td>
            </tr>

            <tr
              v-if="filter === 'all' && explicitRows.length > 0 && defaultRows.length > 0"
              class="memb-divider-row"
              aria-hidden="true"
            >
              <td colspan="3">
                <div class="memb-divider"><span>Defaults — {{ roleDefaultLabel }}</span></div>
              </td>
            </tr>

            <tr v-for="row in defaultRows" :key="`d-${row.project_id}`">
              <td>
                <span class="project-access-key">{{ row.project_key }}</span>
                <span class="project-access-name">{{ row.project_name }}</span>
              </td>
              <td>
                <div class="memb-lvl-group">
                  <button
                    v-for="lvl in visibleLevels" :key="lvl.value"
                    type="button"
                    class="btn btn-sm"
                    :class="row.access_level === lvl.value ? 'btn-primary' : 'btn-ghost'"
                    @click="setMembership(row, lvl.value)"
                  >{{ lvl.label }}</button>
                </div>
              </td>
              <td>
                <button class="btn btn-ghost btn-sm memb-reset" disabled title="Already at role default">Reset</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="form-actions">
        <button type="button" class="btn btn-ghost" @click="membershipsTarget=null">Close</button>
      </div>
    </div>
  </AppModal>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.user-avatar-row { display: flex; align-items: center; gap: .6rem; }
.you-badge { font-size: 10px; font-weight: 600; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); background: var(--bg); border: 1px solid var(--border); border-radius: 20px; padding: .1rem .45rem; }
.role-label { font-size: 11px; font-weight: 700; letter-spacing: .05em; }
.role-admin    { color: #d97706; }
.role-member   { color: var(--bp-blue); }
.role-external { color: #dc2626; }
.edit-user-grid { display: grid; grid-template-columns: 1fr 1fr; gap: .75rem 1.25rem; }
.edit-user-totp {
  display: flex; align-items: center; gap: .6rem; flex-wrap: wrap;
  padding: .6rem .75rem; background: var(--bg); border: 1px solid var(--border);
  border-radius: var(--radius); margin-top: .25rem; font-size: 13px;
}
.edit-user-totp-info { display: flex; align-items: center; gap: .35rem; flex: 1; }
.totp-icon { color: #16a34a; flex-shrink: 0; }
.totp-confirm-text { font-size: 12px; color: var(--text-muted); }
.ua--inactive { background: var(--text-muted) !important; }
.nick-cell { min-width: 80px; }
.nick-display {
  cursor: pointer; color: var(--text-muted); font-size: 12px; font-weight: 600;
  padding: .15rem .35rem; border-radius: 4px; display: inline-block;
  border: 1px solid transparent;
}
.nick-display:hover { background: var(--bg); border-color: var(--border); color: var(--text); }
.nick-input {
  width: 56px !important; padding: .2rem .35rem !important;
  font-size: 12px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em;
  border: 1px solid var(--bp-blue); border-radius: 4px; outline: none;
  background: #fff;
}
.badge-deleted { background: #f8d7da; color: #721c24; }
.row-muted td { opacity: .65; }
.row-muted:hover td { opacity: .85; }
.badge-active   { background: #d4edda; color: #155724; }
/* ── Per-user access modal: chips, table, toolbar ───────────────────── */
.project-access-key {
  font-size: 11px; font-weight: 700; letter-spacing: .03em;
  padding: .1rem .4rem; border-radius: 3px;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  margin-right: .5rem;
}
.project-access-name { font-weight: 500; font-size: 13px; }

.memb-body { display: flex; flex-direction: column; }
.memb-loading { padding: 1rem; color: var(--text-muted); font-size: 13px; }
.memb-hint    { margin: 0 0 .9rem; padding: 0; }

.memb-toolbar {
  display: flex;
  align-items: stretch;
  gap: .65rem;
  margin-bottom: .9rem;
}

/* Filter — a "view setting" pill, distinct from action buttons */
.memb-segmented {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-card);
  padding: 2px;
}
.memb-segmented__opt {
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: .02em;
  padding: .35rem .8rem;
  border-radius: 999px;
  cursor: pointer;
  white-space: nowrap;
  transition: background .12s ease, color .12s ease;
}
.memb-segmented__opt:hover:not(.is-active) { color: var(--text); }
.memb-segmented__opt.is-active {
  background: var(--bp-blue-pale);
  color: var(--bp-blue-dark);
}

/* Add bar — one composed control with hairline-divided segments */
.memb-add {
  display: flex;
  align-items: stretch;
  flex: 1 1 auto;
  min-width: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
  transition: border-color .12s ease, box-shadow .12s ease;
}
.memb-add:focus-within {
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 3px var(--bp-blue-pale);
}
.memb-add.is-disabled { opacity: .55; }

/* Native selects stripped of their chrome so the bar reads as one shell;
   custom caret SVG keeps the affordance visible. */
.memb-add__field {
  appearance: none;
  -webkit-appearance: none;
  border: none;
  background: transparent;
  color: var(--text);
  font-size: 12px;
  font-family: inherit;
  outline: none;
  cursor: pointer;
  padding: 0 1.6rem 0 .75rem;
  background-image: url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='8' height='5' viewBox='0 0 8 5'><path d='M0 0l4 5 4-5z' fill='%237a8597'/></svg>");
  background-repeat: no-repeat;
  background-position: right .6rem center;
}
.memb-add__field:disabled { cursor: not-allowed; }
.memb-add__pick {
  flex: 1 1 auto;
  min-width: 0;
  border-right: 1px solid var(--border);
  text-overflow: ellipsis;
}
.memb-add__lvl {
  flex: 0 0 92px;
  border-right: 1px solid var(--border);
}
.memb-add__lvl-locked {
  display: inline-flex; align-items: center;
  flex: 0 0 92px;
  padding: 0 .75rem;
  border-right: 1px solid var(--border);
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  letter-spacing: .01em;
}
/* Custom-styled instead of .btn-primary so it sits flush in the bar
   (no nested border, no rounded-corner mismatch). */
.memb-add__btn {
  border: none;
  background: var(--bp-blue);
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: .01em;
  padding: 0 1rem;
  cursor: pointer;
  transition: background .12s ease;
  white-space: nowrap;
}
.memb-add__btn:hover:not(:disabled) { background: var(--bp-blue-dark); }
.memb-add__btn:disabled {
  background: var(--bg);
  color: var(--text-muted);
  cursor: not-allowed;
}

/* Empty states */
.memb-empty {
  display: flex; flex-direction: column;
  align-items: center; justify-content: center;
  gap: .55rem;
  min-height: 120px;
  padding: 1.5rem;
  margin-bottom: .9rem;
  text-align: center;
  font-size: 13px;
  color: var(--text-muted);
  background: var(--bg);
  border: 1px dashed var(--border);
  border-radius: var(--radius);
  line-height: 1.45;
}
.memb-empty__icon {
  font-family: 'DM Mono','Fira Code',monospace;
  font-size: 22px;
  color: var(--border);
  line-height: 1;
}

/* Table */
.memb-table-wrap {
  max-height: 56vh;
  overflow-y: auto;
  margin-bottom: .9rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
}
.memb-table { margin: 0; }
.memb-table thead th { position: sticky; top: 0; z-index: 1; }
.memb-table td { padding: .5rem .6rem; }
.memb-table .th-access { width: 220px; }
.memb-table .th-reset  { width: 76px; }
.memb-lvl-group { display: inline-flex; gap: .25rem; }

/* Reset is neutral, not destructive — keep its hover off the danger palette. */
.memb-reset:hover:not(:disabled) { background: var(--bg); color: var(--text); }
.memb-reset:disabled {
  opacity: .35;
  cursor: not-allowed;
  pointer-events: none;
}

/* Divider row — hairline + small label, Linear-style "below the fold". */
.memb-divider-row td {
  padding: 1rem .6rem .35rem;
  background: transparent;
  border-bottom: none;
}
.memb-divider-row:hover td { background: transparent; }
.memb-divider {
  display: flex; align-items: center; gap: .75rem;
}
.memb-divider::before {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--border);
}
.memb-divider span {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
  color: var(--text-muted);
}
</style>
