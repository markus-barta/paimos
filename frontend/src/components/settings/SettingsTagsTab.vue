<script setup lang="ts">
import { ref, computed } from 'vue'
import { api, errMsg } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useSort } from '@/composables/useSort'
import { TAG_COLORS } from '@/types'
import type { Tag } from '@/types'
import AppModal from '@/components/AppModal.vue'
import AppIcon from '@/components/AppIcon.vue'
import TagChip from '@/components/TagChip.vue'

const auth = useAuthStore()
const isAdmin = computed(() => auth.user?.role === 'admin')

const tags        = ref<Tag[]>([])
const tagsLoaded  = ref(false)
const showCreateTag  = ref(false)
const createTagForm  = ref({ name: '', color: 'blue', description: '' })
const createTagError = ref('')
const creatingTag    = ref(false)
const editTagTarget  = ref<Tag | null>(null)
const editTagForm    = ref({ name: '', color: 'blue', description: '' })
const editTagError   = ref('')
const updatingTag    = ref(false)
const deleteTagTarget = ref<Tag | null>(null)
const deletingTag     = ref(false)
const deleteTagError  = ref('')

const { sorted: sortedTags, sortIndicator: tagSortInd, thProps: tagThProps } = useSort(tags, {
  name:        { value: t => t.name,        type: 'string' },
  description: { value: t => t.description, type: 'string' },
  created_at:  { value: t => t.created_at,  type: 'date' },
})

async function loadTags() {
  if (tagsLoaded.value) return
  tags.value = await api.get<Tag[]>('/tags')
  tagsLoaded.value = true
}
function openEditTag(t: Tag) {
  editTagTarget.value = t
  editTagForm.value = { name: t.name, color: t.color, description: t.description }
  editTagError.value = ''
}
async function createTag() {
  createTagError.value = ''
  if (!createTagForm.value.name.trim()) { createTagError.value = 'Name required.'; return }
  creatingTag.value = true
  try {
    const t = await api.post<Tag>('/tags', createTagForm.value)
    tags.value.push(t); showCreateTag.value = false
  } catch (e: unknown) { createTagError.value = errMsg(e) }
  finally { creatingTag.value = false }
}
async function updateTag() {
  if (!editTagTarget.value) return
  editTagError.value = ''; updatingTag.value = true
  try {
    const t = await api.put<Tag>(`/tags/${editTagTarget.value.id}`, editTagForm.value)
    const idx = tags.value.findIndex(x => x.id === t.id)
    if (idx >= 0) tags.value[idx] = t
    editTagTarget.value = null
  } catch (e: unknown) { editTagError.value = errMsg(e) }
  finally { updatingTag.value = false }
}
async function deleteTag() {
  if (!deleteTagTarget.value) return
  deletingTag.value = true
  deleteTagError.value = ''
  try {
    await api.delete(`/tags/${deleteTagTarget.value.id}`)
    tags.value = tags.value.filter(t => t.id !== deleteTagTarget.value!.id)
    deleteTagTarget.value = null
  } catch (e: unknown) {
    deleteTagError.value = errMsg(e, 'Delete failed')
  } finally { deletingTag.value = false }
}

// Init
loadTags()
</script>

<template>
  <div class="section">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">Tags</h2>
        <p class="section-desc">Global tags applied to projects and issues.</p>
      </div>
      <button v-if="isAdmin" class="btn btn-primary btn-sm" @click="showCreateTag=true; createTagForm={name:'',color:'blue',description:''}; createTagError=''">+ New tag</button>
    </div>
    <div v-if="tags.length === 0" class="empty-hint">No tags yet.</div>
    <div v-else class="card" style="padding:0;overflow:hidden">
      <table class="settings-table">
        <thead>
          <tr>
            <th v-bind="tagThProps('name')">Tag <span class="sort-ind"><AppIcon :name="tagSortInd('name')" :size="11" /></span></th>
            <th v-bind="tagThProps('description')">Description <span class="sort-ind"><AppIcon :name="tagSortInd('description')" :size="11" /></span></th>
            <th v-bind="tagThProps('created_at')">Created <span class="sort-ind"><AppIcon :name="tagSortInd('created_at')" :size="11" /></span></th>
            <th v-if="isAdmin"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in sortedTags" :key="t.id">
            <td><TagChip :tag="t" /></td>
            <td class="muted">{{ t.description || '—' }}</td>
            <td class="muted">{{ t.created_at.slice(0,10) }}</td>
            <td v-if="isAdmin" class="actions-cell">
              <button class="btn btn-ghost btn-sm" @click="openEditTag(t)">Edit</button>
              <button class="btn btn-ghost btn-sm danger" @click="deleteTagTarget=t">Delete</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <!-- ── Modals ──────────────────────────────────────────────────────────── -->
  <AppModal title="New Tag" :open="showCreateTag" @close="showCreateTag=false">
    <form @submit.prevent="createTag" class="form">
      <div class="field"><label>Name</label><input v-model="createTagForm.name" type="text" placeholder="e.g. bug, feature" required autofocus /></div>
      <div class="field"><label>Color</label>
        <div class="color-palette">
          <button v-for="c in TAG_COLORS" :key="c" type="button" :class="['color-swatch',`swatch-${c}`,{selected:createTagForm.color===c}]" :title="c" @click="createTagForm.color=c"></button>
        </div>
        <div class="color-preview"><TagChip :tag="{id:0,name:createTagForm.name||'preview',color:createTagForm.color,description:'',created_at:''}" /></div>
      </div>
      <div class="field"><label>Description <span class="label-hint">— optional</span></label><input v-model="createTagForm.description" type="text" /></div>
      <div v-if="createTagError" class="form-error">{{ createTagError }}</div>
      <div class="form-actions">
        <button type="button" class="btn btn-ghost" @click="showCreateTag=false">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="creatingTag">{{ creatingTag ? 'Creating…' : 'Create tag' }}</button>
      </div>
    </form>
  </AppModal>

  <AppModal :title="`Edit tag: ${editTagTarget?.name}`" :open="!!editTagTarget" @close="editTagTarget=null">
    <form @submit.prevent="updateTag" class="form">
      <div class="field"><label>Name</label><input v-model="editTagForm.name" type="text" required autofocus /></div>
      <div class="field"><label>Color</label>
        <div class="color-palette">
          <button v-for="c in TAG_COLORS" :key="c" type="button" :class="['color-swatch',`swatch-${c}`,{selected:editTagForm.color===c}]" :title="c" @click="editTagForm.color=c"></button>
        </div>
        <div class="color-preview"><TagChip :tag="{id:0,name:editTagForm.name||'preview',color:editTagForm.color,description:'',created_at:''}" /></div>
      </div>
      <div class="field"><label>Description <span class="label-hint">— optional</span></label><input v-model="editTagForm.description" type="text" /></div>
      <div v-if="editTagError" class="form-error">{{ editTagError }}</div>
      <div class="form-actions">
        <button type="button" class="btn btn-ghost" @click="editTagTarget=null">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="updatingTag">{{ updatingTag ? 'Saving…' : 'Save changes' }}</button>
      </div>
    </form>
  </AppModal>

  <AppModal title="Delete Tag" :open="!!deleteTagTarget" @close="deleteTagTarget=null; deleteTagError=''" confirm-key="d" @confirm="deleteTag">
    <p class="delete-warning">Delete tag <TagChip :tag="deleteTagTarget!" />? Removed from all projects and issues. Cannot be undone.</p>
    <p v-if="deleteTagError" class="form-error">{{ deleteTagError }}</p>
    <div class="form-actions" style="margin-top:1.25rem">
      <button class="btn btn-ghost" @click="deleteTagTarget=null; deleteTagError=''"><u>C</u>ancel</button>
      <button class="btn btn-danger" @click="deleteTag" :disabled="deletingTag"><template v-if="deletingTag">Deleting…</template><template v-else><u>D</u>elete tag</template></button>
    </div>
  </AppModal>
</template>

<style src="./settings-shared.css"></style>
<style scoped>
.color-palette { display: flex; flex-wrap: wrap; gap: .4rem; }
.color-swatch { width: 24px; height: 24px; border-radius: 50%; border: 2px solid transparent; cursor: pointer; transition: transform .1s, border-color .1s; outline: none; }
.color-swatch:hover { transform: scale(1.15); }
.color-swatch.selected { border-color: var(--text); transform: scale(1.1); }
.swatch-gray{background:#9ca3af}.swatch-slate{background:#64748b}.swatch-blue{background:#3b82f6}
.swatch-indigo{background:#6366f1}.swatch-purple{background:#a855f7}.swatch-pink{background:#ec4899}
.swatch-red{background:#ef4444}.swatch-orange{background:#f97316}.swatch-yellow{background:#eab308}
.swatch-green{background:#22c55e}.swatch-teal{background:#14b8a6}.swatch-cyan{background:#06b6d4}
.color-preview { margin-top: .25rem; }
.delete-warning { font-size: 14px; color: var(--text); line-height: 1.6; display: flex; align-items: center; flex-wrap: wrap; gap: .4rem; }
</style>
