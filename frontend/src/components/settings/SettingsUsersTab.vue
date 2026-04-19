<script setup lang="ts">
import { ref, computed } from 'vue'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import { useSort } from '@/composables/useSort'
import AppModal from '@/components/AppModal.vue'
import AppIcon from '@/components/AppIcon.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import type { User } from '@/types'

const auth = useAuthStore()
const { confirm } = useConfirm()

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

// ── User project access (external users) ──────────────────────────────────
interface UserProjectAccess { project_id: number; name: string; key: string }
interface SimpleProject { id: number; name: string; key: string }
const userProjectsTarget = ref<User | null>(null)
const userProjects = ref<UserProjectAccess[]>([])
const allProjects = ref<SimpleProject[]>([])
const addProjectId = ref<number | null>(null)
const projectsLoading = ref(false)

async function openUserProjects(u: User) {
  userProjectsTarget.value = u
  projectsLoading.value = true
  try {
    const [up, ap] = await Promise.all([
      api.get<UserProjectAccess[]>(`/users/${u.id}/projects`),
      api.get<SimpleProject[]>('/projects'),
    ])
    userProjects.value = up
    allProjects.value = ap
  } catch { /* ignore */ }
  projectsLoading.value = false
}
async function addUserProject() {
  if (!userProjectsTarget.value || !addProjectId.value) return
  try {
    await api.post(`/users/${userProjectsTarget.value.id}/projects`, { project_id: addProjectId.value })
    const proj = allProjects.value.find(p => p.id === addProjectId.value)
    if (proj) userProjects.value.push({ project_id: proj.id, name: proj.name, key: proj.key })
    addProjectId.value = null
  } catch { /* ignore */ }
}
async function removeUserProject(projectId: number) {
  if (!userProjectsTarget.value) return
  if (!await confirm({ message: 'Remove this user from the project?', confirmLabel: 'Remove' })) return
  try {
    await api.delete(`/users/${userProjectsTarget.value.id}/projects/${projectId}`)
    userProjects.value = userProjects.value.filter(p => p.project_id !== projectId)
  } catch { /* ignore */ }
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
              <button v-if="u.role === 'external'" class="btn btn-ghost btn-sm" @click="openUserProjects(u)" title="Manage project access">Projects</button>
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

  <AppModal :title="`Project Access: ${userProjectsTarget?.username}`" :open="!!userProjectsTarget" @close="userProjectsTarget=null">
    <div v-if="projectsLoading" style="padding:1rem;color:var(--text-muted)">Loading...</div>
    <div v-else class="form">
      <div v-if="userProjects.length > 0" class="project-access-list">
        <div v-for="p in userProjects" :key="p.project_id" class="project-access-row">
          <span class="project-access-key">{{ p.key }}</span>
          <span class="project-access-name">{{ p.name }}</span>
          <button class="btn btn-ghost btn-sm danger" @click="removeUserProject(p.project_id)" title="Remove access">Remove</button>
        </div>
      </div>
      <p v-else class="empty-hint">No projects assigned yet.</p>
      <div class="project-access-add">
        <select v-model="addProjectId" style="flex:1">
          <option :value="null" disabled>Select project...</option>
          <option v-for="p in allProjects.filter(ap => !userProjects.some(up => up.project_id === ap.id))" :key="p.id" :value="p.id">
            {{ p.key }} — {{ p.name }}
          </option>
        </select>
        <button class="btn btn-primary btn-sm" :disabled="!addProjectId" @click="addUserProject">Add</button>
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

/* ── Project access modal ────────────────────────────────────────────── */
.project-access-list { display: flex; flex-direction: column; gap: .25rem; margin-bottom: .75rem; }
.project-access-row {
  display: flex; align-items: center; gap: .75rem;
  padding: .4rem .5rem; border-radius: var(--radius);
  background: var(--bg);
}
.project-access-key {
  font-size: 11px; font-weight: 700; letter-spacing: .03em;
  padding: .1rem .4rem; border-radius: 3px;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
}
.project-access-name { flex: 1; font-size: 13px; font-weight: 500; }
.project-access-add {
  display: flex; gap: .5rem; align-items: center;
}
</style>
