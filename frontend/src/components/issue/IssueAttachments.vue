<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { errMsg } from '@/api/client'
import { attachmentsEnabled } from '@/api/instance'
import { MAX_ATTACHMENT_SIZE } from '@/utils/constants'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import { useAttachmentLightbox } from '@/composables/useAttachmentLightbox'
import AppIcon from '@/components/AppIcon.vue'
import type { Attachment } from '@/types'
import { deleteIssueAttachment, loadIssueAttachments, uploadIssueAttachment } from '@/services/issueAttachments'

const props = defineProps<{
  issueId: number
}>()

const authStore = useAuthStore()
const { confirm } = useConfirm()

const attachments    = ref<Attachment[]>([])
const attachLoading  = ref(false)
const uploadProgress = ref<number | null>(null)
const attachError    = ref('')
const dragOver       = ref(false)

async function load() {
  attachLoading.value = true
  attachments.value = await loadIssueAttachments(props.issueId).catch(() => [])
  attachLoading.value = false
}

defineExpose({ load, attachments })

watch(() => props.issueId, () => load())

function isImage(ct: string) { return ct.startsWith('image/') }

const lightbox = useAttachmentLightbox()
const imageAttachments = computed(() => attachments.value.filter(a => isImage(a.content_type)))
function openInLightbox(a: Attachment, e: Event) {
  e.preventDefault()
  const list = imageAttachments.value
  const idx = list.findIndex(x => x.id === a.id)
  lightbox.openLightbox(list, Math.max(0, idx))
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

async function uploadFiles(files: FileList | File[]) {
  if (!attachmentsEnabled.value) {
    attachError.value = 'File storage is not configured on this instance.'
    return
  }
  attachError.value = ''
  for (const file of Array.from(files)) {
    if (file.size > MAX_ATTACHMENT_SIZE) {
      attachError.value = `${file.name}: exceeds 10 MB limit`
      continue
    }
    if (attachments.value.length >= 20) {
      attachError.value = 'Maximum 20 attachments per issue'
      break
    }
    uploadProgress.value = 0
    try {
      const a = await uploadIssueAttachment(props.issueId, file, (pct) => { uploadProgress.value = pct })
      attachments.value = [...attachments.value, a]
    } catch (e: unknown) {
      attachError.value = errMsg(e, 'Upload failed')
    } finally {
      uploadProgress.value = null
    }
  }
}

function onFilePick(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files?.length) uploadFiles(input.files)
  input.value = ''
}

function onDrop(e: DragEvent) {
  e.preventDefault()
  dragOver.value = false
  if (e.dataTransfer?.files?.length) uploadFiles(e.dataTransfer.files)
}

async function deleteAttachment(a: Attachment) {
  if (!await confirm({ message: `Delete "${a.filename}"?`, confirmLabel: 'Delete', danger: true })) return
  try {
    await deleteIssueAttachment(a.id)
    attachments.value = attachments.value.filter(x => x.id !== a.id)
  } catch (e: unknown) {
    attachError.value = errMsg(e, 'Delete failed')
  }
}
</script>

<template>
  <div class="attachments-section">
    <h3 class="section-title">
      Attachments
      <span class="attach-count" v-if="attachments.length">{{ attachments.length }}</span>
    </h3>

    <div v-if="attachError" class="form-error" style="margin-bottom:.5rem">{{ attachError }}</div>

    <div
      class="attach-drop-zone"
      :class="{ 'attach-drop-zone--over': dragOver && attachmentsEnabled, 'attach-drop-zone--disabled': !attachmentsEnabled }"
      @dragover.prevent="attachmentsEnabled ? (dragOver = true) : null"
      @dragleave="dragOver = false"
      @drop="onDrop"
    >
      <div v-if="attachments.length" class="attach-grid">
        <div v-for="a in attachments" :key="a.id" class="attach-item">
          <button
            v-if="isImage(a.content_type)"
            type="button"
            class="attach-thumb-link"
            :title="`Open ${a.filename}`"
            @click="(e) => openInLightbox(a, e)"
          >
            <img :src="`/api/attachments/${a.id}`" :alt="a.filename" class="attach-thumb" loading="lazy" />
          </button>
          <div v-else class="attach-file">
            <AppIcon name="file" :size="20" />
            <a :href="`/api/attachments/${a.id}`" target="_blank" class="attach-file-name">{{ a.filename }}</a>
          </div>
          <div class="attach-meta">
            <span class="attach-size">{{ formatSize(a.size_bytes) }}</span>
            <span class="attach-uploader">{{ a.uploader }}</span>
            <button v-if="attachmentsEnabled && (authStore.user?.role === 'admin' || a.uploaded_by === authStore.user?.id)" class="attach-delete" @click="deleteAttachment(a)" title="Delete">
              <AppIcon name="x" :size="12" />
            </button>
          </div>
        </div>
      </div>

      <div v-if="uploadProgress !== null" class="attach-progress">
        <div class="attach-progress-bar" :style="{ width: uploadProgress + '%' }"></div>
      </div>

      <!-- Upload UI — only shown when storage is configured. -->
      <label v-if="attachmentsEnabled" class="attach-upload-label">
        <input type="file" multiple class="attach-upload-input" @change="onFilePick" />
        <span v-if="!attachments.length" class="attach-empty">No attachments — drag files here or click to upload</span>
        <span v-else class="attach-add">+ Add files</span>
      </label>
      <div v-else class="attach-disabled-notice">
        File storage is not configured on this instance.
      </div>
    </div>
  </div>
</template>

<style scoped>
.attachments-section { margin-top: 1.5rem; }
.section-title {
  font-size: 13px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; color: var(--text-muted);
  display: flex; align-items: center; gap: .5rem;
}
.attach-count {
  font-size: 11px; font-weight: 700; color: var(--text-muted);
  background: var(--bg); border-radius: 99px;
  padding: .05rem .4rem; margin-left: .35rem;
}
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); }
.attach-drop-zone {
  border: 2px dashed var(--border); border-radius: var(--radius);
  padding: .75rem; transition: border-color .15s, background .15s;
}
.attach-drop-zone--over {
  border-color: var(--bp-blue); background: var(--bp-blue-pale);
}
.attach-drop-zone--disabled {
  border-style: solid; background: var(--bg);
  opacity: .75;
}
.attach-disabled-notice {
  font-size: 12px; font-style: italic; color: var(--text-muted);
  text-align: center; padding: .25rem 0;
}
.attach-grid {
  display: flex; flex-wrap: wrap; gap: .6rem; margin-bottom: .5rem;
}
.attach-item {
  display: flex; flex-direction: column; gap: .25rem;
  padding: .4rem; border-radius: var(--radius);
  border: 1px solid var(--border); background: var(--bg-card);
  max-width: 160px;
}
.attach-thumb-link {
  display: block;
  padding: 0;
  background: none;
  border: none;
  cursor: pointer;
  font-family: inherit;
}
.attach-thumb {
  width: 150px; max-height: 120px; object-fit: cover;
  border-radius: 4px; display: block;
}
.attach-file {
  display: flex; align-items: center; gap: .35rem;
  padding: .25rem 0;
}
.attach-file-name {
  font-size: 12px; color: var(--bp-blue); text-decoration: none;
  word-break: break-all;
}
.attach-file-name:hover { text-decoration: underline; }
.attach-meta {
  display: flex; align-items: center; gap: .4rem; font-size: 11px; color: var(--text-muted);
}
.attach-delete {
  margin-left: auto; background: none; border: none; cursor: pointer;
  color: var(--text-muted); padding: .1rem; border-radius: 3px; line-height: 0;
}
.attach-delete:hover { color: #c0392b; background: #fde8e8; }
.attach-progress {
  height: 3px; background: var(--border); border-radius: 2px;
  overflow: hidden; margin: .5rem 0;
}
.attach-progress-bar {
  height: 100%; background: var(--bp-blue); transition: width .15s;
}
.attach-upload-label {
  display: block; text-align: center; cursor: pointer;
  padding: .4rem 0;
}
.attach-upload-input { display: none; }
.attach-empty { font-size: 12px; color: var(--text-muted); font-style: italic; }
.attach-add { font-size: 12px; color: var(--bp-blue); font-weight: 600; }
.attach-add:hover { text-decoration: underline; }
</style>
