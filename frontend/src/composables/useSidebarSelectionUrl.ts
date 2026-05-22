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
 * useSidebarSelectionUrl — PAI-479.
 *
 * Two-way sync between a sidebar's `selectedIssueId` and `?selected=<ISSUE_KEY>`
 * in the route query. The point is link-sharing: a user with an issue open in
 * the sidebar can copy the URL bar and the recipient lands in the same list
 * with the same issue pre-opened.
 *
 * Write side (state → URL): each selection change replaces the query (no
 * history-entry churn while scanning a list). Other query keys are preserved.
 *
 * Read side (URL → state): on mount, the consumer's `resolveKeyToId` is called
 * with the `?selected` value. If it resolves to a numeric id, the sidebar is
 * opened to that issue — regardless of whether the row would be visible under
 * the current filter/tab (the recipient sees what the sender intended). If the
 * resolver returns null (issue not accessible, not found), the sidebar stays
 * closed; no spinner, no surprise navigation.
 *
 * The composable owns no state of its own — it just plumbs the existing
 * `selectedIssueId` ref into the route. Consumers keep full control of when
 * the resolvers are ready (e.g. wait until the issue list has loaded before
 * unblocking the mount-time resolve).
 */

import { onMounted, watch, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

const QUERY_KEY = 'selected'

export interface SidebarSelectionUrlOptions {
  /** Source of truth for the currently-open sidebar issue. */
  selectedIssueId: Ref<number | null>
  /**
   * Map a numeric issue id to its issue_key for the URL.
   * Return null if the key isn't known locally — the URL will not be touched
   * for that selection (the sidebar still opens; only the URL sync is skipped).
   */
  resolveIdToKey: (id: number) => string | null
  /**
   * Map an `?selected=<KEY>` URL value to a numeric issue id on mount.
   * Return null to indicate the issue isn't accessible / doesn't exist — the
   * sidebar stays closed. May be async (e.g. an API fallback for keys not in
   * the local list).
   */
  resolveKeyToId: (key: string) => Promise<number | null> | number | null
  /**
   * Optional gate for the mount-time resolve. When provided, the read-side
   * resolve is deferred until this returns true (typical use: wait for the
   * issue list to finish loading so resolveKeyToId can hit the local cache
   * first).
   */
  ready?: () => boolean
}

export function useSidebarSelectionUrl(opts: SidebarSelectionUrlOptions) {
  const route = useRoute()
  const router = useRouter()

  // Suppress write-side echo while we apply the URL → state on mount.
  let applyingFromUrl = false

  watch(opts.selectedIssueId, (id) => {
    if (applyingFromUrl) return
    const key = id != null ? opts.resolveIdToKey(id) : null
    const nextQuery = { ...route.query }
    if (key) {
      if (nextQuery[QUERY_KEY] === key) return
      nextQuery[QUERY_KEY] = key
    } else {
      if (nextQuery[QUERY_KEY] === undefined) return
      delete nextQuery[QUERY_KEY]
    }
    void router.replace({ query: nextQuery })
  })

  onMounted(async () => {
    const raw = route.query[QUERY_KEY]
    const key = typeof raw === 'string' ? raw : Array.isArray(raw) ? raw[0] : null
    if (!key) return

    if (opts.ready) {
      // Poll a small budget for readiness. Real consumers (IssueList,
      // PortalProjectView) already have a loaded `issues` array within a few
      // microtasks of mount; this just avoids racing the first paint.
      let tries = 0
      while (!opts.ready() && tries < 50) {
        await new Promise(r => setTimeout(r, 20))
        tries++
      }
    }

    const id = await opts.resolveKeyToId(key)
    if (id != null) {
      applyingFromUrl = true
      opts.selectedIssueId.value = id
      // Release the suppression after the watcher has had a tick to run.
      await new Promise(r => setTimeout(r, 0))
      applyingFromUrl = false
    }
  })
}
