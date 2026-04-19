/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

/**
 * useAttachmentUploads — shared upload pipeline for issue create/edit surfaces.
 *
 * One composable owns a `jobs` list; callers pick the endpoint (pending vs.
 * issue-scoped) and consume the list via the `AttachmentSidebar` component.
 *
 * Used by: CreateIssueModal, IssueSidePanel (quick edit), future consumers.
 */
import { ref, computed } from 'vue'
import type { Ref } from 'vue'
import type { Attachment } from '@/types'
import { api, errMsg } from '@/api/client'

export type UploadStatus = 'pending' | 'done' | 'failed'

export interface AttachmentJob {
  /** Client-side unique id. Stable across retries. */
  id: string
  file: File
  filename: string
  size: number
  isImage: boolean
  progress: number
  status: UploadStatus
  /** Server-side attachment id once the upload resolves. */
  attachmentId: number | null
  error?: string
  /** blob: URL for image thumbnails while uploading. Revoked on removeJob. */
  previewUrl: string | null
}

export interface UseAttachmentUploadsOptions {
  /** Called per upload — returns e.g. '/attachments' or '/issues/42/attachments'. */
  endpoint: () => string
  /** Max per-file size in bytes. Default 10 MB (matches backend cap). */
  maxFileSize?: number
  /** Max concurrent attachments. Default 20 (matches backend cap). */
  maxCount?: number
}

const DEFAULT_MAX_SIZE = 10 * 1024 * 1024
const DEFAULT_MAX_COUNT = 20

export interface UseAttachmentUploadsReturn {
  jobs: Ref<AttachmentJob[]>
  pendingIds: Ref<number[]>
  hasInFlight: Ref<boolean>
  inFlightCount: Ref<number>
  addFiles: (files: FileList | File[]) => AttachmentJob[]
  removeJob: (job: AttachmentJob) => Promise<void>
  retryJob: (job: AttachmentJob) => void
  linkPending: (issueId: number) => Promise<void>
  seedExisting: (attachments: Attachment[]) => void
  reset: () => void
}

export function useAttachmentUploads(opts: UseAttachmentUploadsOptions): UseAttachmentUploadsReturn {
  const maxSize  = opts.maxFileSize ?? DEFAULT_MAX_SIZE
  const maxCount = opts.maxCount    ?? DEFAULT_MAX_COUNT
  const jobs = ref<AttachmentJob[]>([])
  let seq = 0

  const pendingIds = computed<number[]>(() => {
    const ids: number[] = []
    for (const j of jobs.value) {
      if (j.status === 'done' && j.attachmentId != null) ids.push(j.attachmentId)
    }
    return ids
  })
  const hasInFlight   = computed(() => jobs.value.some(j => j.status === 'pending'))
  const inFlightCount = computed(() => jobs.value.filter(j => j.status === 'pending').length)

  function addFiles(files: FileList | File[]): AttachmentJob[] {
    const list = Array.from(files)
    const added: AttachmentJob[] = []
    for (const file of list) {
      if (jobs.value.length >= maxCount) {
        jobs.value.push(makeJob(file, 'failed', `max ${maxCount} attachments reached`))
        added.push(jobs.value[jobs.value.length - 1])
        continue
      }
      if (file.size > maxSize) {
        jobs.value.push(makeJob(file, 'failed',
          `file too large (max ${Math.round(maxSize / 1024 / 1024)} MB)`))
        added.push(jobs.value[jobs.value.length - 1])
        continue
      }
      jobs.value.push(makeJob(file, 'pending'))
      // Fetch the live reactive proxy from the array — mutating the raw
      // reference returned by makeJob() would bypass the proxy's set trap
      // so watchers and templates wouldn't see progress updates.
      const live = jobs.value[jobs.value.length - 1]
      added.push(live)
      startUpload(live)
    }
    return added
  }

  function makeJob(file: File, status: UploadStatus, error?: string): AttachmentJob {
    const isImage = file.type.startsWith('image/')
    return {
      id: `job-${++seq}`,
      file,
      filename: file.name,
      size: file.size,
      isImage,
      progress: 0,
      status,
      attachmentId: null,
      error,
      previewUrl: isImage ? URL.createObjectURL(file) : null,
    }
  }

  function startUpload(job: AttachmentJob) {
    job.status = 'pending'
    job.progress = 0
    job.error = undefined

    const fd = new FormData()
    fd.append('file', job.file)

    api.upload<Attachment>(opts.endpoint(), fd, (pct) => { job.progress = pct })
      .then((a) => {
        job.status = 'done'
        job.progress = 100
        job.attachmentId = a.id
      })
      .catch((err: unknown) => {
        job.status = 'failed'
        job.error = errMsg(err, 'upload failed')
      })
  }

  async function removeJob(job: AttachmentJob): Promise<void> {
    if (job.status === 'done' && job.attachmentId != null) {
      try {
        await api.delete(`/attachments/${job.attachmentId}`)
      } catch {
        // Swallow — still remove client-side so the UI doesn't get stuck.
      }
    }
    if (job.previewUrl) URL.revokeObjectURL(job.previewUrl)
    jobs.value = jobs.value.filter(j => j.id !== job.id)
  }

  function retryJob(job: AttachmentJob): void {
    // Re-fetch the live reactive proxy in case the caller passed a stale
    // raw reference (same reason as addFiles above).
    const live = jobs.value.find(j => j.id === job.id)
    if (live) startUpload(live)
  }

  async function linkPending(issueId: number): Promise<void> {
    const ids = pendingIds.value
    if (!ids.length) return
    await api.patch('/attachments/link', {
      issue_id: issueId,
      attachment_ids: ids,
    })
  }

  /** Seed the jobs list with already-uploaded attachments (done state, no upload). */
  function seedExisting(attachments: Attachment[]): void {
    // Revoke any existing blob URLs from a previous seed/reset cycle.
    for (const j of jobs.value) {
      if (j.previewUrl && j.previewUrl.startsWith('blob:')) URL.revokeObjectURL(j.previewUrl)
    }
    jobs.value = attachments.map(a => ({
      id: `existing-${a.id}`,
      // No File reference for existing attachments — retry would not work,
      // but they're already uploaded so retry is never relevant.
      file: new File([], a.filename, { type: a.content_type }),
      filename: a.filename,
      size: a.size_bytes,
      isImage: a.content_type.startsWith('image/'),
      progress: 100,
      status: 'done',
      attachmentId: a.id,
      // Server URL — no blob revoke needed.
      previewUrl: a.content_type.startsWith('image/') ? `/api/attachments/${a.id}` : null,
    }))
  }

  function reset(): void {
    for (const j of jobs.value) {
      // Only revoke blob URLs; existing-attachment previewUrls are server paths.
      if (j.previewUrl && j.previewUrl.startsWith('blob:')) URL.revokeObjectURL(j.previewUrl)
    }
    jobs.value = []
  }

  return {
    jobs,
    pendingIds,
    hasInFlight,
    inFlightCount,
    addFiles,
    removeJob,
    retryJob,
    linkPending,
    seedExisting,
    reset,
  }
}
