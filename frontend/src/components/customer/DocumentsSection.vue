<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.
-->

<!--
 DocumentsSection — scope-agnostic documents UI used by both
 CustomerDetailView and ProjectDetailView. The whole section is a
 drop-target (cleaner than nesting a sub-zone), with the dashed-border
 affordance fading in only when the user actually drags over.

 Document rows show: type icon · filename + label · status pill ·
 validity · uploaded-at · download · delete. PDFs additionally show a
 lazy-loaded inline preview thumbnail.
-->
<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { api, errMsg } from '@/api/client'
import type { Document } from '@/types'

const props = defineProps<{
  scope: 'customer' | 'project'
  scopeId: number
  /** Admin-only writes; set false for member viewers. */
  canWrite: boolean
}>()

// Emit the current document count so a parent (e.g. ProjectDetailView's
// segmented Issues/Docs/Coop control) can show a badge without
// re-fetching the list itself.
const emit = defineEmits<{ count: [n: number] }>()

const docs = ref<Document[]>([])
const loading = ref(true)
const loadError = ref('')
const dragging = ref(false)
const uploading = ref(false)
const uploadError = ref('')
const fileInputRef = ref<HTMLInputElement | null>(null)

const listUrl = computed(() =>
  props.scope === 'customer'
    ? `/customers/${props.scopeId}/documents`
    : `/projects/${props.scopeId}/documents`,
)
const uploadUrl = computed(() =>
  props.scope === 'customer'
    ? `/api/customers/${props.scopeId}/documents`
    : `/api/projects/${props.scopeId}/documents`,
)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    docs.value = await api.get<Document[]>(listUrl.value)
    emit('count', docs.value.length)
  } catch (e: unknown) {
    loadError.value = errMsg(e, 'Failed to load documents.')
    emit('count', 0)
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => [props.scope, props.scopeId], load)
// Mutations (upload / delete) update docs.value directly; broadcast the
// new count so the parent badge stays in sync without a full reload.
watch(() => docs.value.length, (n) => emit('count', n))

async function uploadFiles(files: FileList | File[]) {
  if (!files || !files.length) return
  uploadError.value = ''
  uploading.value = true
  try {
    // Sequential — keeps the UX predictable and avoids hammering MinIO
    // when someone drops 8 PDFs at once. Tiny files; the latency is fine.
    for (const file of Array.from(files)) {
      const fd = new FormData()
      fd.append('file', file)
      const resp = await fetch(uploadUrl.value, {
        method: 'POST',
        credentials: 'same-origin',
        body: fd,
      })
      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}))
        throw new Error(data.error ?? `Upload failed (${resp.status}).`)
      }
    }
    await load()
  } catch (e: unknown) {
    uploadError.value = errMsg(e, 'Upload failed.')
  } finally {
    uploading.value = false
  }
}

function onDragEnter(e: DragEvent) {
  if (!props.canWrite) return
  if (!e.dataTransfer?.types.includes('Files')) return
  e.preventDefault()
  dragging.value = true
}
function onDragOver(e: DragEvent) {
  if (!props.canWrite || !dragging.value) return
  e.preventDefault()
}
function onDragLeave(e: DragEvent) {
  // Only clear when the cursor leaves the section entirely — moving over
  // a child element fires `dragleave` on the parent too without this guard.
  if (!props.canWrite) return
  if (e.currentTarget && (e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) return
  dragging.value = false
}
function onDrop(e: DragEvent) {
  if (!props.canWrite) return
  e.preventDefault()
  dragging.value = false
  if (e.dataTransfer?.files) uploadFiles(e.dataTransfer.files)
}

function onPickFile() { fileInputRef.value?.click() }
function onFileInput(e: Event) {
  const target = e.target as HTMLInputElement
  if (target.files) uploadFiles(target.files)
  target.value = ''
}

async function deleteDoc(d: Document) {
  if (!confirm(`Delete "${d.filename}"?`)) return
  try {
    await api.delete(`/documents/${d.id}`)
    docs.value = docs.value.filter((x) => x.id !== d.id)
  } catch (e: unknown) {
    uploadError.value = errMsg(e, 'Delete failed.')
  }
}

function downloadUrl(d: Document) { return `/api/documents/${d.id}/download` }

function fileIcon(mime: string): string {
  if (mime.startsWith('image/')) return 'image'
  if (mime === 'application/pdf') return 'file-text'
  if (mime.includes('word'))      return 'file-text'
  return 'file'
}

function fmtSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function fmtDate(s: string | null | undefined): string {
  if (!s) return ''
  return new Date(s.replace(' ', 'T') + 'Z').toLocaleDateString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric',
  })
}
</script>

<template>
  <section
    :class="['docs-section', { 'docs-section--drag': dragging }]"
    @dragenter="onDragEnter"
    @dragover="onDragOver"
    @dragleave="onDragLeave"
    @drop="onDrop"
  >
    <header class="docs-header">
      <div>
        <h3 class="docs-title">Documents</h3>
        <p class="docs-hint" v-if="canWrite">Drag files anywhere in this section, or click upload.</p>
        <p class="docs-hint" v-else>Read-only — admin access required to upload or modify.</p>
      </div>
      <div class="docs-actions" v-if="canWrite">
        <button class="btn btn-ghost btn-sm" :disabled="uploading" @click="onPickFile">
          <AppIcon name="upload" :size="14" />
          {{ uploading ? 'Uploading…' : 'Upload' }}
        </button>
        <input
          ref="fileInputRef"
          type="file"
          multiple
          style="display:none"
          @change="onFileInput"
        />
      </div>
    </header>

    <div v-if="uploadError" class="docs-error">{{ uploadError }}</div>

    <div v-if="loading" class="docs-loading">Loading documents…</div>
    <div v-else-if="loadError" class="docs-error">{{ loadError }}</div>

    <div v-else-if="docs.length === 0" class="docs-empty">
      <AppIcon name="file-stack" :size="22" />
      <div>
        <strong>No documents yet.</strong>
        <span v-if="canWrite"> Drop PDFs, contracts or images here to get started.</span>
      </div>
    </div>

    <ul v-else class="docs-list">
      <li v-for="d in docs" :key="d.id" class="docs-row">
        <a :href="downloadUrl(d)" target="_blank" rel="noopener" class="docs-thumb">
          <iframe
            v-if="d.mime_type === 'application/pdf'"
            :src="`${downloadUrl(d)}#toolbar=0&navpanes=0&view=FitH`"
            loading="lazy"
            class="docs-thumb-pdf"
            title="PDF preview"
          />
          <img
            v-else-if="d.mime_type.startsWith('image/')"
            :src="downloadUrl(d)"
            loading="lazy"
            :alt="d.filename"
            class="docs-thumb-img"
          />
          <AppIcon v-else :name="fileIcon(d.mime_type)" :size="22" />
        </a>

        <div class="docs-meta">
          <a :href="downloadUrl(d)" target="_blank" rel="noopener" class="docs-name">
            {{ d.filename }}
          </a>
          <p v-if="d.label" class="docs-label">{{ d.label }}</p>
          <div class="docs-meta-row">
            <span :class="['docs-status', `docs-status--${d.status}`]">{{ d.status }}</span>
            <span v-if="d.valid_from || d.valid_until" class="docs-validity">
              {{ d.valid_from ? fmtDate(d.valid_from) : '…' }} → {{ d.valid_until ? fmtDate(d.valid_until) : '…' }}
            </span>
            <span class="docs-bytes">{{ fmtSize(d.size_bytes) }}</span>
            <span class="docs-uploaded">uploaded {{ fmtDate(d.uploaded_at) }}</span>
          </div>
        </div>

        <div class="docs-row-actions">
          <a :href="downloadUrl(d)" target="_blank" rel="noopener" class="docs-icon-btn" title="Download">
            <AppIcon name="download" :size="14" />
          </a>
          <button
            v-if="canWrite"
            class="docs-icon-btn docs-icon-btn--danger"
            title="Delete document"
            @click="deleteDoc(d)"
          >
            <AppIcon name="trash-2" :size="14" />
          </button>
        </div>
      </li>
    </ul>

    <div v-if="dragging" class="docs-drop-overlay" aria-hidden="true">
      <AppIcon name="upload-cloud" :size="32" />
      <span>Drop to upload</span>
    </div>
  </section>
</template>

<style scoped>
.docs-section {
  position: relative;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 1.25rem 1.4rem;
  display: flex; flex-direction: column; gap: 1rem;
  transition: border-color .15s, box-shadow .15s;
}
.docs-section--drag {
  border-color: var(--bp-blue);
  box-shadow: 0 0 0 4px rgba(46,109,164,.10);
}
.docs-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 1rem; }
.docs-title { font-size: 14px; font-weight: 700; color: var(--text); margin: 0 0 .15rem; letter-spacing: -.01em; }
.docs-hint { font-size: 12px; color: var(--text-muted); margin: 0; }
.docs-actions { display: flex; gap: .5rem; }

.docs-error {
  background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca;
  padding: .5rem .75rem; border-radius: var(--radius); font-size: 13px;
}
.docs-loading, .docs-empty {
  display: flex; align-items: center; gap: .75rem;
  padding: 1.25rem; color: var(--text-muted); font-size: 13px;
  border: 1px dashed var(--border); border-radius: 8px;
}
.docs-empty strong { color: var(--text); font-weight: 600; display: block; }

.docs-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: .5rem; }
.docs-row {
  display: grid;
  grid-template-columns: 92px 1fr auto;
  gap: 1rem;
  align-items: center;
  padding: .65rem;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: #fafbfc;
  transition: border-color .15s, background .15s;
}
.docs-row:hover { border-color: var(--bp-blue-light); background: #fff; }

.docs-thumb {
  width: 92px; height: 64px;
  display: flex; align-items: center; justify-content: center;
  background: #fff; border: 1px solid var(--border); border-radius: 6px;
  overflow: hidden;
  color: var(--text-muted);
  text-decoration: none;
}
.docs-thumb-pdf { width: 100%; height: 100%; border: none; pointer-events: none; }
.docs-thumb-img { max-width: 100%; max-height: 100%; object-fit: cover; }

.docs-meta { min-width: 0; display: flex; flex-direction: column; gap: .15rem; }
.docs-name {
  font-size: 13px; font-weight: 600; color: var(--text); text-decoration: none;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.docs-name:hover { color: var(--bp-blue-dark); text-decoration: underline; }
.docs-label { font-size: 12px; color: var(--text-muted); margin: 0; }
.docs-meta-row {
  display: flex; gap: .65rem; flex-wrap: wrap; align-items: center;
  margin-top: .25rem; font-size: 11px; color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.docs-status {
  display: inline-block;
  padding: .1rem .5rem;
  border-radius: 999px;
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em;
}
.docs-status--active  { background: #dcfce7; color: #166534; }
.docs-status--expired { background: #fee2e2; color: #991b1b; }
.docs-status--draft   { background: #e2e8f0; color: #475569; }

.docs-validity { font-family: 'DM Mono', monospace; font-size: 11px; }

.docs-row-actions { display: flex; gap: .25rem; align-items: center; }
.docs-icon-btn {
  width: 28px; height: 28px;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 6px; border: none; background: transparent;
  color: var(--text-muted); cursor: pointer; text-decoration: none;
  transition: background .15s, color .15s;
}
.docs-icon-btn:hover { background: var(--bp-blue-pale); color: var(--bp-blue-dark); }
.docs-icon-btn--danger:hover { background: #fef2f2; color: #b91c1c; }

.docs-drop-overlay {
  position: absolute; inset: 0;
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: .5rem;
  background: rgba(46,109,164,.06);
  border-radius: 10px;
  color: var(--bp-blue-dark);
  font-weight: 600;
  pointer-events: none;
  backdrop-filter: blur(2px);
}
</style>
