<script setup lang="ts">
/**
 * AttachmentSidebar — presentation layer for `useAttachmentUploads`.
 *
 * Renders one chip per AttachmentJob with filename, size, live progress bar,
 * thumbnail (images), and remove / retry actions. Owns a drop zone at the
 * bottom that forwards File drops via the `add-files` event. Pure presentation
 * — all state lives in the `useAttachmentUploads` composable on the parent.
 */
import { computed, ref } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import type { AttachmentJob } from '@/composables/useAttachmentUploads'
import { useAttachmentLightbox } from '@/composables/useAttachmentLightbox'
import type { Attachment } from '@/types'

const props = defineProps<{
  jobs: AttachmentJob[]
  title?: string
  emptyHint?: string
  /** If true, the drop zone is hidden (read-only view). */
  readonly?: boolean
}>()

const emit = defineEmits<{
  (e: 'add-files', files: FileList): void
  (e: 'remove', job: AttachmentJob): void
  (e: 'retry', job: AttachmentJob): void
}>()

const lightbox = useAttachmentLightbox()

// Build the list of image-only attachments for the lightbox, preserving
// the order in which they appear in the sidebar so left/right nav matches
// what the user sees.
const imageAttachmentsInOrder = computed<Attachment[]>(() => {
  const out: Attachment[] = []
  for (const j of props.jobs) {
    if (j.status === 'done' && j.attachmentId != null && j.isImage) {
      out.push({
        id: j.attachmentId,
        issue_id: 0,
        object_key: '',
        filename: j.filename,
        content_type: j.file.type || 'image/*',
        size_bytes: j.size,
        uploaded_by: 0,
        uploader: '',
        created_at: '',
      })
    }
  }
  return out
})

function openInLightbox(job: AttachmentJob, e: Event) {
  e.preventDefault()
  if (!job.isImage || job.status !== 'done' || job.attachmentId == null) return
  const list = imageAttachmentsInOrder.value
  const idx = list.findIndex(a => a.id === job.attachmentId)
  lightbox.openLightbox(list, Math.max(0, idx))
}

const dragOver = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function onDrop(e: DragEvent) {
  dragOver.value = false
  if (e.dataTransfer?.files?.length) emit('add-files', e.dataTransfer.files)
}

function onPick(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files?.length) emit('add-files', input.files)
  input.value = ''
}

function openUrlForJob(job: AttachmentJob): string | null {
  return job.attachmentId != null ? `/api/attachments/${job.attachmentId}` : null
}
</script>

<template>
  <aside class="att-sidebar">
    <header class="att-header">
      <AppIcon name="paperclip" :size="13" />
      <span>{{ title ?? 'Attachments' }}</span>
      <span v-if="jobs.length" class="att-count">{{ jobs.length }}</span>
    </header>

    <div v-if="!jobs.length && emptyHint && readonly" class="att-empty">{{ emptyHint }}</div>

    <ul v-if="jobs.length" class="att-list">
      <li
        v-for="job in jobs"
        :key="job.id"
        :class="['att-item', `att-item--${job.status}`]"
      >
        <button
          v-if="job.status === 'done' && job.isImage && job.attachmentId != null"
          type="button"
          class="att-thumb att-thumb--link"
          :title="`Open ${job.filename}`"
          @click="(e) => openInLightbox(job, e)"
        >
          <img v-if="job.previewUrl" :src="job.previewUrl" alt="" />
          <AppIcon v-else name="image" :size="16" />
        </button>
        <a
          v-else-if="job.status === 'done' && openUrlForJob(job)"
          :href="openUrlForJob(job)!"
          target="_blank"
          class="att-thumb att-thumb--link"
          :title="`Open ${job.filename}`"
        >
          <AppIcon :name="job.isImage ? 'image' : 'file'" :size="16" />
        </a>
        <div v-else class="att-thumb">
          <img v-if="job.isImage && job.previewUrl" :src="job.previewUrl" alt="" />
          <AppIcon v-else :name="job.isImage ? 'image' : 'file'" :size="16" />
        </div>

        <div class="att-body">
          <div class="att-name" :title="job.filename">{{ job.filename }}</div>
          <div class="att-meta">
            <span>{{ formatBytes(job.size) }}</span>
            <span v-if="job.status === 'pending'">· {{ job.progress }}%</span>
            <span v-else-if="job.status === 'failed'" class="att-err">· {{ job.error }}</span>
          </div>
          <div v-if="job.status === 'pending'" class="att-bar">
            <div class="att-bar-fill" :style="{ width: job.progress + '%' }" />
          </div>
        </div>

        <button
          v-if="job.status === 'failed'"
          type="button"
          class="att-btn"
          :title="`Retry upload of ${job.filename}`"
          @click="emit('retry', job)"
        >
          <AppIcon name="refresh-cw" :size="12" />
        </button>
        <button
          v-if="!readonly"
          type="button"
          class="att-btn att-btn--remove"
          :title="`Remove ${job.filename}`"
          @click="emit('remove', job)"
        >
          <AppIcon name="x" :size="12" />
        </button>
      </li>
    </ul>

    <div
      v-if="!readonly"
      :class="['att-drop', { 'att-drop--over': dragOver }]"
      @dragover.prevent="dragOver = true"
      @dragleave.prevent="dragOver = false"
      @drop.prevent="onDrop"
      @click="fileInput?.click()"
    >
      <AppIcon name="upload" :size="14" />
      <span>Drop files or click to browse</span>
      <input ref="fileInput" type="file" multiple hidden @change="onPick" />
    </div>
  </aside>
</template>

<style scoped>
.att-sidebar {
  display: flex;
  flex-direction: column;
  gap: .55rem;
  padding: .75rem .8rem;
  background: var(--bg);
  border-left: 1px solid var(--border);
  min-width: 240px;
  max-width: 280px;
}

.att-header {
  display: flex;
  align-items: center;
  gap: .4rem;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .06em;
  color: var(--text-muted);
  padding-bottom: .45rem;
  border-bottom: 1px solid var(--border);
}
.att-count {
  margin-left: auto;
  background: var(--bp-blue-pale, #e0eeff);
  color: var(--bp-blue, #2e6da4);
  padding: 0 .4rem;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 700;
}

.att-empty {
  font-size: 12px;
  color: var(--text-muted);
  font-style: italic;
  text-align: center;
  padding: .75rem 0;
}

.att-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: .4rem;
  max-height: 55vh;
  overflow-y: auto;
}
.att-item {
  display: flex;
  align-items: center;
  gap: .55rem;
  padding: .4rem .5rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 6px;
  transition: background .12s, border-color .12s;
  animation: att-slide-in 180ms cubic-bezier(.2,.7,.2,1);
}
@keyframes att-slide-in {
  from { opacity: 0; transform: translateY(-3px); }
  to   { opacity: 1; transform: translateY(0); }
}
.att-item--pending { border-color: color-mix(in srgb, var(--bp-blue, #2e6da4) 30%, var(--border)); }
.att-item--done    { border-color: rgba(30,132,73,.3); }
.att-item--failed {
  background: #fdeeec;
  border-color: rgba(192,57,43,.4);
}

.att-thumb {
  flex-shrink: 0;
  width: 34px;
  height: 34px;
  border-radius: 4px;
  background: var(--bg);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  color: var(--text-muted);
  border: 1px solid var(--border);
}
.att-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
button.att-thumb--link {
  padding: 0;
  background: var(--bg);
  font-family: inherit;
}
.att-thumb--link { cursor: pointer; transition: border-color .12s; }
.att-thumb--link:hover { border-color: var(--bp-blue, #2e6da4); }

.att-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: .15rem;
}
.att-name {
  font-size: 12px;
  font-weight: 600;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.2;
}
.att-meta {
  font-size: 10px;
  color: var(--text-muted);
  display: flex;
  gap: .3rem;
  font-variant-numeric: tabular-nums;
}
.att-err { color: #a02b1c; }

.att-bar {
  height: 3px;
  background: rgba(46,109,164,.15);
  border-radius: 999px;
  overflow: hidden;
  margin-top: .2rem;
  position: relative;
}
.att-bar-fill {
  position: absolute;
  inset: 0;
  background: var(--bp-blue, #2e6da4);
  width: 0;
  transition: width 140ms linear;
}
.att-bar-fill::after {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(90deg, transparent, rgba(255,255,255,.6), transparent);
  animation: att-shimmer 1.1s linear infinite;
}
@keyframes att-shimmer {
  0%   { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}

.att-btn {
  flex-shrink: 0;
  background: none;
  border: none;
  cursor: pointer;
  padding: 3px;
  border-radius: 4px;
  color: var(--text-muted);
  opacity: .55;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: opacity .12s, background .12s, color .12s;
}
.att-btn:hover { opacity: 1; background: rgba(0,0,0,.06); }
.att-btn--remove:hover { color: #a02b1c; }

.att-drop {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: .3rem;
  padding: .85rem .5rem;
  border: 2px dashed var(--border);
  border-radius: 6px;
  font-size: 11px;
  color: var(--text-muted);
  cursor: pointer;
  text-align: center;
  transition: border-color .12s, background .12s, color .12s;
}
.att-drop:hover,
.att-drop--over {
  border-color: var(--bp-blue, #2e6da4);
  background: rgba(46,109,164,.05);
  color: var(--bp-blue, #2e6da4);
}
</style>
