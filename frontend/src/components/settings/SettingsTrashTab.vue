<script setup lang="ts">
import { ref } from 'vue'
import { api } from '@/api/client'
import { useConfirm } from '@/composables/useConfirm'
import type { User } from '@/types'
import UserAvatar from '@/components/UserAvatar.vue'

type TrashedIssue = {
  id: number
  issue_key: string
  project_id: number | null
  type: string
  title: string
  deleted_at: string
  deleted_by_name?: string
}

const deletedUsers    = ref<User[]>([])
const deletedProjects = ref<{ id: number; name: string; status: string; created_at: string }[]>([])
const deletedIssues   = ref<TrashedIssue[]>([])
const trashLoading    = ref(false)
const { confirm }     = useConfirm()

async function loadTrash() {
  trashLoading.value = true
  try {
    const [du, dp, di] = await Promise.all([
      api.get<User[]>('/users?status=deleted'),
      api.get<{ id: number; name: string; status: string; created_at: string }[]>('/projects?status=deleted'),
      api.get<TrashedIssue[]>('/issues/trash'),
    ])
    deletedUsers.value    = du
    deletedProjects.value = dp
    deletedIssues.value   = di ?? []
  } catch { /* swallow */ }
  finally { trashLoading.value = false }
}

async function restoreUser(u: User) {
  await api.put<User>(`/users/${u.id}`, { status: 'inactive' })
  deletedUsers.value = deletedUsers.value.filter(x => x.id !== u.id)
}

async function restoreProject(p: { id: number; name: string }) {
  await api.put(`/projects/${p.id}`, { status: 'active' })
  deletedProjects.value = deletedProjects.value.filter(x => x.id !== p.id)
}

async function restoreIssue(iss: TrashedIssue) {
  await api.post(`/issues/${iss.id}/restore`, {})
  deletedIssues.value = deletedIssues.value.filter(x => x.id !== iss.id)
}

async function purgeIssue(iss: TrashedIssue) {
  const ok = await confirm({
    message: `Permanently delete ${iss.issue_key} "${iss.title}"? This wipes its comments, history, tags, time entries, attachments, and any dependency / impact links. Cannot be undone.`,
    confirmLabel: 'Delete forever',
    danger: true,
  })
  if (!ok) return
  await api.delete(`/issues/${iss.id}/purge`)
  deletedIssues.value = deletedIssues.value.filter(x => x.id !== iss.id)
}

// Init
loadTrash()
</script>

<template>
  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Trash</h2>
      <p class="section-desc">Soft-deleted users, projects and issues. Restore to bring them back, or delete forever to wipe them permanently.</p>
    </div>

    <div v-if="trashLoading" class="empty-hint">Loading…</div>
    <template v-else>

      <!-- Deleted users -->
      <div class="trash-sub-section">
        <h3 class="trash-sub-title">Deleted Users</h3>
        <div v-if="deletedUsers.length === 0" class="empty-hint">No deleted users.</div>
        <div v-else class="card" style="padding:0;overflow:hidden">
          <table class="settings-table">
            <thead>
              <tr><th>Username</th><th>Role</th><th>Deleted</th><th></th></tr>
            </thead>
            <tbody>
              <tr v-for="u in deletedUsers" :key="u.id">
                <td>
                  <div class="user-avatar-row">
                    <UserAvatar :user="u" size="sm" />
                    <span class="fw500">{{ u.username }}</span>
                  </div>
                </td>
                <td><span class="badge badge-archived">{{ u.role }}</span></td>
                <td class="muted">{{ u.created_at.slice(0,10) }}</td>
                <td class="actions-cell">
                  <button class="btn btn-ghost btn-sm" @click="restoreUser(u)" title="Restore to disabled">Restore</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Deleted projects -->
      <div class="trash-sub-section">
        <h3 class="trash-sub-title">Deleted Projects</h3>
        <div v-if="deletedProjects.length === 0" class="empty-hint">No deleted projects.</div>
        <div v-else class="card" style="padding:0;overflow:hidden">
          <table class="settings-table">
            <thead>
              <tr><th>Name</th><th>Created</th><th></th></tr>
            </thead>
            <tbody>
              <tr v-for="p in deletedProjects" :key="p.id">
                <td class="fw500">{{ p.name }}</td>
                <td class="muted">{{ p.created_at.slice(0,10) }}</td>
                <td class="actions-cell">
                  <button class="btn btn-ghost btn-sm" @click="restoreProject(p)" title="Restore project">Restore</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Deleted issues -->
      <div class="trash-sub-section">
        <h3 class="trash-sub-title">Deleted Issues</h3>
        <div v-if="deletedIssues.length === 0" class="empty-hint">No deleted issues.</div>
        <div v-else class="card" style="padding:0;overflow:hidden">
          <table class="settings-table">
            <thead>
              <tr>
                <th>Key</th>
                <th>Type</th>
                <th>Title</th>
                <th>Deleted</th>
                <th>By</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="iss in deletedIssues" :key="iss.id">
                <td class="fw500">{{ iss.issue_key || `#${iss.id}` }}</td>
                <td><span class="badge badge-archived">{{ iss.type }}</span></td>
                <td>{{ iss.title }}</td>
                <td class="muted">{{ iss.deleted_at.slice(0,10) }}</td>
                <td class="muted">{{ iss.deleted_by_name || '—' }}</td>
                <td class="actions-cell">
                  <button class="btn btn-ghost btn-sm" @click="restoreIssue(iss)" title="Restore this issue (children stay in trash)">Restore</button>
                  <button class="btn btn-ghost btn-sm danger" @click="purgeIssue(iss)" title="Permanently delete — cannot be undone">Delete forever</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

    </template>
  </div>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.trash-sub-section { margin-bottom: 1.5rem; }
.trash-sub-title { font-size: 13px; font-weight: 700; color: var(--text-muted); text-transform: uppercase; letter-spacing: .06em; margin-bottom: .6rem; }
.user-avatar-row { display: flex; align-items: center; gap: .6rem; }
.btn-sm.danger { color: #c0392b; }
.btn-sm.danger:hover { background: #fef2f2; color: #991b1b; }
</style>
