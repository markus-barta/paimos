<script setup lang="ts">
import { ref } from 'vue'
import { api, errMsg } from '@/api/client'
import type { SavedView } from '@/types'
import AppModal from '@/components/AppModal.vue'
import AppIcon from '@/components/AppIcon.vue'

const defaultViews     = ref<SavedView[]>([])
const viewsLoaded      = ref(false)
const viewsDragIdx     = ref<number | null>(null)

async function loadDefaultViews() {
  if (viewsLoaded.value) return
  const all = await api.get<SavedView[]>('/views')
  defaultViews.value = all.filter(v => v.is_admin_default).sort((a, b) => a.sort_order - b.sort_order)
  viewsLoaded.value = true
}

function viewsDragStart(idx: number) { viewsDragIdx.value = idx }
function viewsDragOver(e: DragEvent, idx: number) {
  e.preventDefault()
  if (viewsDragIdx.value === null || viewsDragIdx.value === idx) return
  const items = [...defaultViews.value]
  const [moved] = items.splice(viewsDragIdx.value, 1)
  items.splice(idx, 0, moved)
  defaultViews.value = items
  viewsDragIdx.value = idx
}
async function viewsDragEnd() {
  viewsDragIdx.value = null
  const payload = defaultViews.value.map((v, i) => ({ id: v.id, sort_order: i }))
  await api.patch('/views/order', payload)
}

// Edit default view
const editViewTarget = ref<SavedView | null>(null)
const editViewForm = ref({ title: '', description: '' })
const editViewError = ref('')
const editingView = ref(false)

function openEditDefaultView(v: SavedView) {
  editViewTarget.value = v
  editViewForm.value = { title: v.title, description: v.description }
  editViewError.value = ''
}

async function saveDefaultView() {
  if (!editViewTarget.value) return
  editingView.value = true
  try {
    const updated = await api.put<SavedView>(`/views/${editViewTarget.value.id}`, editViewForm.value)
    const idx = defaultViews.value.findIndex(x => x.id === updated.id)
    if (idx >= 0) defaultViews.value[idx] = updated
    editViewTarget.value = null
  } catch (e: unknown) { editViewError.value = errMsg(e) }
  finally { editingView.value = false }
}

// Delete default view
const deleteViewTarget = ref<SavedView | null>(null)
const deletingView = ref(false)

async function confirmDeleteView() {
  if (!deleteViewTarget.value) return
  deletingView.value = true
  try {
    await api.delete(`/views/${deleteViewTarget.value.id}`)
    defaultViews.value = defaultViews.value.filter(x => x.id !== deleteViewTarget.value!.id)
    deleteViewTarget.value = null
  } catch { /* ignore */ }
  finally { deletingView.value = false }
}

async function toggleViewHidden(v: SavedView) {
  const next = !v.hidden
  await api.put(`/views/${v.id}`, { hidden: next })
  v.hidden = next
}

// Init
loadDefaultViews()
</script>

<template>
  <div class="section">
    <div class="section-header">
      <h2 class="section-title">Default Views</h2>
      <p class="section-desc">Drag to reorder. Toggle visibility with the eye icon. Order and visibility apply to all users.</p>
    </div>
    <div class="card" style="padding:0;overflow:hidden">
      <table class="settings-table">
        <thead><tr><th style="width:30px"></th><th>Title</th><th>Description</th><th style="width:140px">Actions</th></tr></thead>
        <tbody>
          <tr
            v-for="(v, idx) in defaultViews" :key="v.id"
            :class="{ 'row-muted': v.hidden, 'row-dragging': viewsDragIdx === idx }"
            draggable="true"
            @dragstart="viewsDragStart(idx)"
            @dragover="viewsDragOver($event, idx)"
            @dragend="viewsDragEnd"
            style="cursor:grab"
          >
            <td class="drag-handle"><AppIcon name="grip-vertical" :size="14" /></td>
            <td class="fw500">{{ v.title }}</td>
            <td class="muted">{{ v.description || '—' }}</td>
            <td class="actions-cell" style="display:flex;gap:.25rem">
              <button class="btn btn-ghost btn-sm" :title="v.hidden ? 'Show' : 'Hide'" @click="toggleViewHidden(v)">
                <AppIcon :name="v.hidden ? 'eye-off' : 'eye'" :size="15" />
              </button>
              <button class="btn btn-ghost btn-sm" title="Edit" @click="openEditDefaultView(v)">
                <AppIcon name="pencil" :size="14" />
              </button>
              <button class="btn btn-ghost btn-sm danger" title="Delete" @click="deleteViewTarget=v">
                <AppIcon name="trash-2" :size="14" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <!-- ── Modals ──────────────────────────────────────────────────────────── -->
  <AppModal title="Edit View" :open="!!editViewTarget" @close="editViewTarget=null">
    <form @submit.prevent="saveDefaultView" class="form">
      <div class="field"><label>Title</label><input v-model="editViewForm.title" type="text" required /></div>
      <div class="field"><label>Description</label><textarea v-model="editViewForm.description" rows="2" /></div>
      <div v-if="editViewError" class="form-error">{{ editViewError }}</div>
      <div class="form-actions">
        <button type="button" class="btn btn-ghost" @click="editViewTarget=null">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="editingView">{{ editingView ? 'Saving…' : 'Save' }}</button>
      </div>
    </form>
  </AppModal>

  <AppModal title="Delete View" :open="!!deleteViewTarget" @close="deleteViewTarget=null" confirm-key="d" @confirm="confirmDeleteView">
    <p style="font-size:14px;color:var(--text);margin-bottom:1.25rem">
      Delete <strong>{{ deleteViewTarget?.title }}</strong>? This default view will be removed for all users.
    </p>
    <div style="display:flex;justify-content:flex-end;gap:.5rem">
      <button class="btn btn-ghost" @click="deleteViewTarget=null"><u>C</u>ancel</button>
      <button class="btn btn-danger" :disabled="deletingView" @click="confirmDeleteView"><template v-if="deletingView">Deleting…</template><template v-else><u>D</u>elete</template></button>
    </div>
  </AppModal>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.drag-handle { color: var(--text-muted); cursor: grab; }
.row-dragging { opacity: .5; background: var(--bp-blue-pale); }
.row-muted td { opacity: .65; }
.row-muted:hover td { opacity: .85; }
</style>
