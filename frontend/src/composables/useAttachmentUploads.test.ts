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

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { nextTick, watch, effectScope } from 'vue'

// IMPORTANT: vi.mock() is hoisted above the composable import, so the composable
// receives the mocked `api` when it's evaluated. Do NOT move this below the import.
vi.mock('@/api/client', () => ({
  api: {
    upload: vi.fn(),
    delete: vi.fn().mockResolvedValue(undefined),
    patch:  vi.fn().mockResolvedValue(undefined),
    get:    vi.fn().mockResolvedValue([]),
    post:   vi.fn().mockResolvedValue(undefined),
    put:    vi.fn().mockResolvedValue(undefined),
  },
  errMsg: (_: unknown, fallback: string) => fallback,
  ApiError: class ApiError extends Error {
    constructor(public status: number, message: string) { super(message) }
  },
}))

import { useAttachmentUploads, type AttachmentJob } from './useAttachmentUploads'
import { api } from '@/api/client'

/**
 * Typed handle to the mocked api.upload so we can stash the onProgress
 * callback the composable hands us and simulate the browser firing it later.
 */
const mockedUpload = api.upload as unknown as ReturnType<typeof vi.fn>

describe('useAttachmentUploads', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('exposes a pending job with progress 0 immediately after addFiles', () => {
    mockedUpload.mockReturnValue(new Promise(() => { /* never resolves */ }))

    const { jobs, addFiles, hasInFlight, inFlightCount } = useAttachmentUploads({
      endpoint: () => '/attachments',
    })
    const file = new File(['x'], 'first.txt', { type: 'text/plain' })
    addFiles([file])

    expect(jobs.value).toHaveLength(1)
    expect(jobs.value[0].status).toBe('pending')
    expect(jobs.value[0].progress).toBe(0)
    expect(jobs.value[0].filename).toBe('first.txt')
    expect(hasInFlight.value).toBe(true)
    expect(inFlightCount.value).toBe(1)
  })

  it('fires reactive watchers when XHR progress changes (the v1.1.16 hang bug)', async () => {
    // This is the real reactivity assertion: install a sync watcher on
    // jobs.value[0].progress BEFORE firing any progress events, then push
    // several progress values and confirm the watcher received every one.
    //
    // If `startUpload` mutates a held raw reference instead of the reactive
    // proxy stored in the array, `job.progress = pct` does NOT fire the
    // proxy's set trap — the watcher never runs, the template never
    // re-renders, and the UI hangs at 0%. A re-read through the proxy would
    // still reflect the new value (proxy get returns target state), which is
    // why a simple `expect(jobs.value[0].progress).toBe(50)` check would
    // pass even when the UI is broken. This test catches the real bug.
    let capturedOnProgress: ((pct: number) => void) | undefined
    mockedUpload.mockImplementation((_endpoint: string, _fd: FormData, onProgress?: (pct: number) => void) => {
      capturedOnProgress = onProgress
      return new Promise(() => { /* never resolves */ })
    })

    const scope = effectScope()
    const observed: number[] = []
    scope.run(() => {
      const { jobs, addFiles } = useAttachmentUploads({ endpoint: () => '/attachments' })
      addFiles([new File(['x'], 'watch.bin')])

      watch(
        () => jobs.value[0]?.progress,
        (v) => { if (typeof v === 'number') observed.push(v) },
        { flush: 'sync' },
      )

      capturedOnProgress!(25)
      capturedOnProgress!(50)
      capturedOnProgress!(75)
      capturedOnProgress!(99)
    })
    await nextTick()
    scope.stop()

    // All four progress mutations must have fired the watcher.
    expect(observed).toEqual([25, 50, 75, 99])
  })

  it('propagates XHR progress events to jobs.value[0].progress (value state)', async () => {
    // Capture the onProgress callback the composable hands to api.upload so the
    // test can simulate the browser firing a progress event at 25% / 50% / 75%.
    // A Vue-3 reactivity bug where startUpload mutates a raw reference instead
    // of the reactive proxy stored in the array would make this test fail: the
    // internal raw object gets the new progress, but jobs.value[0] (which goes
    // through the reactive proxy) stays at 0.
    let capturedOnProgress: ((pct: number) => void) | undefined
    let resolveUpload: ((a: unknown) => void) | undefined
    mockedUpload.mockImplementation((_endpoint: string, _fd: FormData, onProgress?: (pct: number) => void) => {
      capturedOnProgress = onProgress
      return new Promise((resolve) => { resolveUpload = resolve })
    })

    const { jobs, addFiles } = useAttachmentUploads({
      endpoint: () => '/attachments',
    })
    addFiles([new File(['hello'], 'test.bin', { type: 'application/octet-stream' })])
    await nextTick()

    expect(capturedOnProgress).toBeTypeOf('function')

    capturedOnProgress!(25)
    await nextTick()
    expect(jobs.value[0].progress).toBe(25)

    capturedOnProgress!(50)
    await nextTick()
    expect(jobs.value[0].progress).toBe(50)

    capturedOnProgress!(99)
    await nextTick()
    expect(jobs.value[0].progress).toBe(99)

    // Complete the upload.
    resolveUpload!({
      id: 42,
      issue_id: 0,
      object_key: '',
      filename: 'test.bin',
      content_type: 'application/octet-stream',
      size_bytes: 5,
      uploaded_by: 1,
      uploader: '',
      created_at: '',
    })
    await nextTick()
    // Give the then() microtask a beat to run.
    await new Promise((r) => setTimeout(r, 0))
    expect(jobs.value[0].status).toBe('done')
    expect(jobs.value[0].progress).toBe(100)
    expect(jobs.value[0].attachmentId).toBe(42)
  })

  it('marks a job failed and stores the error message when upload rejects', async () => {
    mockedUpload.mockRejectedValue(new Error('boom'))

    const { jobs, addFiles } = useAttachmentUploads({
      endpoint: () => '/attachments',
    })
    addFiles([new File(['x'], 'bad.bin')])
    await new Promise((r) => setTimeout(r, 0))

    expect(jobs.value[0].status).toBe('failed')
    expect(jobs.value[0].error).toBeTruthy()
  })

  it('rejects oversize files without calling api.upload', () => {
    const { jobs, addFiles } = useAttachmentUploads({
      endpoint: () => '/attachments',
      maxFileSize: 10, // bytes
    })
    const big = new File(['1234567890abcdef'], 'big.bin')
    const [job] = addFiles([big])

    expect(job.status).toBe('failed')
    expect(jobs.value[0].status).toBe('failed')
    expect(mockedUpload).not.toHaveBeenCalled()
  })

  it('collects done attachment ids in pendingIds for linkPending()', async () => {
    // First upload resolves with id=1, second with id=2.
    const responses: Array<{ id: number }> = [{ id: 1 }, { id: 2 }]
    mockedUpload.mockImplementation(() => Promise.resolve(responses.shift()))

    const { jobs, addFiles, pendingIds, linkPending } = useAttachmentUploads({
      endpoint: () => '/attachments',
    })
    addFiles([
      new File(['a'], 'a.txt'),
      new File(['b'], 'b.txt'),
    ])
    // Flush both promise chains.
    await new Promise((r) => setTimeout(r, 0))
    await new Promise((r) => setTimeout(r, 0))

    expect(jobs.value.every((j: AttachmentJob) => j.status === 'done')).toBe(true)
    expect(pendingIds.value).toEqual([1, 2])

    await linkPending(99)
    expect(api.patch).toHaveBeenCalledWith('/attachments/link', {
      issue_id: 99,
      attachment_ids: [1, 2],
    })
  })
})
