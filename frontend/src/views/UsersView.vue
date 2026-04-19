<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'

import AppModal from '@/components/AppModal.vue'
import AppFooter from '@/components/AppFooter.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useSort } from '@/composables/useSort'
import AppIcon from '@/components/AppIcon.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import type { User } from '@/types'

const ROLE_OPTIONS: MetaOption[] = [
  { value: 'member', label: 'Member' },
  { value: 'admin',  label: 'Admin'  },
]

const authStore = useAuthStore()
const users     = ref<User[]>([])
const loading   = ref(true)

// Create
const showCreate = ref(false)
const createForm = ref({ username: '', password: '', role: 'member' })
const createError = ref('')
const creating = ref(false)

// Edit
const editTarget = ref<User | null>(null)
const editForm   = ref({ username: '', password: '', role: 'member' })
const editError  = ref('')
const updating   = ref(false)

const isSelf = (u: User) => u.id === authStore.user?.id

const { sorted: sortedUsers, sortIndicator, thProps } = useSort(users, {
  username:   { value: u => u.username,   type: 'string' },
  role:       { value: u => u.role,       type: { order: ['admin','member'] } },
  created_at: { value: u => u.created_at, type: 'date' },
})

onMounted(async () => {
  users.value = await api.get<User[]>('/users')
  loading.value = false
})

function openEdit(u: User) {
  editTarget.value = u
  editForm.value = { username: u.username, password: '', role: u.role }
  editError.value = ''
}

async function createUser() {
  createError.value = ''
  if (!createForm.value.username || !createForm.value.password) {
    createError.value = 'Username and password required.'
    return
  }
  creating.value = true
  try {
    const u = await api.post<User>('/users', createForm.value)
    users.value.push(u)
    showCreate.value = false
    createForm.value = { username: '', password: '', role: 'member' }
  } catch (e: unknown) {
    createError.value = errMsg(e)
  } finally {
    creating.value = false
  }
}

async function updateUser() {
  if (!editTarget.value) return
  editError.value = ''
  updating.value = true
  try {
    const payload: Record<string, string> = {}
    if (editForm.value.username !== editTarget.value.username) payload.username = editForm.value.username
    if (editForm.value.role     !== editTarget.value.role)     payload.role     = editForm.value.role
    if (editForm.value.password)                               payload.password = editForm.value.password

    const u = await api.put<User>(`/users/${editTarget.value.id}`, payload)
    const idx = users.value.findIndex(x => x.id === u.id)
    if (idx >= 0) users.value[idx] = u
    editTarget.value = null
  } catch (e: unknown) {
    editError.value = errMsg(e)
  } finally {
    updating.value = false
  }
}
</script>

<template>
    <Teleport defer to="#app-header-left">
      <span class="ah-title">Users</span>
      <span class="ah-subtitle">{{ users.length }} user{{ users.length !== 1 ? 's' : '' }}</span>
    </Teleport>
    <Teleport defer to="#app-header-right">
      <button class="btn btn-primary btn-sm" @click="showCreate=true">+ New user</button>
    </Teleport>

    <div v-if="loading" class="loading">Loading…</div>

    <div v-else class="user-table-wrap">
      <table class="user-table">
        <thead>
          <tr>
            <th v-bind="thProps('username')">Username <span class="sort-ind"><AppIcon :name="sortIndicator('username')" :size="11" /></span></th>
            <th v-bind="thProps('role')">Role <span class="sort-ind"><AppIcon :name="sortIndicator('role')" :size="11" /></span></th>
            <th v-bind="thProps('created_at')">Created <span class="sort-ind"><AppIcon :name="sortIndicator('created_at')" :size="11" /></span></th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in sortedUsers" :key="u.id">
            <td class="username-cell">
              <div class="user-avatar-row">
                <UserAvatar :user="u" size="sm" />
                {{ u.nickname || u.username }}
                <span v-if="isSelf(u)" class="you-badge">you</span>
              </div>
            </td>
            <td><span class="badge" :class="u.role === 'admin' ? 'badge-active' : 'badge-archived'">{{ u.role }}</span></td>
            <td class="date-cell">{{ u.created_at.slice(0, 10) }}</td>
            <td class="actions-cell">
              <button class="btn btn-ghost btn-sm" @click="openEdit(u)">Edit</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <AppFooter />

    <!-- Create modal -->
    <AppModal title="New User" :open="showCreate" @close="showCreate=false; createForm={username:'',password:'',role:'member'}">
      <form @submit.prevent="createUser" class="form">
        <div class="field">
          <label>Username</label>
          <input v-model="createForm.username" type="text" placeholder="username" required autofocus />
        </div>
        <div class="field">
          <label>Password</label>
          <input v-model="createForm.password" type="password" placeholder="••••••••" required />
        </div>
        <div class="field">
          <label>Role</label>
          <MetaSelect v-model="createForm.role" :options="ROLE_OPTIONS" />
        </div>
        <div v-if="createError" class="form-error">{{ createError }}</div>
        <div class="form-actions">
          <button type="button" class="btn btn-ghost" @click="showCreate=false">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="creating">
            {{ creating ? 'Creating…' : 'Create user' }}
          </button>
        </div>
      </form>
    </AppModal>

    <!-- Edit modal -->
    <AppModal :title="`Edit ${editTarget?.username}`" :open="!!editTarget" @close="editTarget=null">
      <form @submit.prevent="updateUser" class="form">
        <div class="field">
          <label>Username</label>
          <input v-model="editForm.username" type="text" required />
        </div>
        <div class="field">
          <label>Role</label>
          <MetaSelect v-model="editForm.role" :options="ROLE_OPTIONS" />
        </div>
        <div class="field">
          <label>New password <span class="label-hint">— leave blank to keep current</span></label>
          <input v-model="editForm.password" type="password" placeholder="••••••••" autocomplete="new-password" />
        </div>
        <div v-if="editError" class="form-error">{{ editError }}</div>
        <div class="form-actions">
          <button type="button" class="btn btn-ghost" @click="editTarget=null">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="updating">
            {{ updating ? 'Saving…' : 'Save changes' }}
          </button>
        </div>
      </form>
    </AppModal>
</template>

<style scoped>
.loading { color: var(--text-muted); padding: 2rem 0; }

.user-table-wrap {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow);
  overflow: hidden;
}
.user-table { width: 100%; border-collapse: collapse; }
.user-table thead th {
  padding: .65rem 1rem;
  text-align: left;
  font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em;
  color: var(--text-muted); background: var(--bg); border-bottom: 1px solid var(--border);
}
.sortable-th { cursor: pointer; user-select: none; white-space: nowrap; }
.sortable-th:hover { color: var(--text); background: var(--border) !important; }
.sortable-th.sort-active { color: var(--bp-blue-dark) !important; }
.sort-ind { display: inline-block; margin-left: .25rem; font-size: 10px; opacity: .55; vertical-align: middle; }
.sortable-th.sort-active .sort-ind { opacity: 1; }
.user-table tbody tr { border-bottom: 1px solid var(--border); transition: background .1s; }
.user-table tbody tr:last-child { border-bottom: none; }
.user-table tbody tr:hover { background: #f0f2f4; }
.user-table td { padding: .75rem 1rem; font-size: 13px; vertical-align: middle; }
.date-cell { color: var(--text-muted); }
.actions-cell { text-align: right; }

.user-avatar-row { display: flex; align-items: center; gap: .6rem; font-weight: 500; }
.you-badge {
  font-size: 10px; font-weight: 600; text-transform: uppercase; letter-spacing: .06em;
  color: var(--text-muted); background: var(--bg);
  border: 1px solid var(--border); border-radius: 20px;
  padding: .1rem .45rem;
}

.btn-sm { padding: .3rem .65rem; font-size: 12px; }

.form { display: flex; flex-direction: column; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: .35rem; }
.field label { font-size: 12px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: .05em; }
.label-hint { font-weight: 400; text-transform: none; letter-spacing: 0; font-size: 11px; }
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); }
.form-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }
</style>
