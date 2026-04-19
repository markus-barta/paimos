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

import { type InjectionKey, type Ref, inject, provide, ref } from 'vue'
import type { User, Tag, Project, Sprint } from '@/types'

export interface IssueContext {
  users: Ref<User[]>
  allTags: Ref<Tag[]>
  costUnits: Ref<string[]>
  releases: Ref<string[]>
  projects: Ref<Project[]>
  sprints: Ref<Sprint[]>
}

const ISSUE_CTX_KEY: InjectionKey<IssueContext> = Symbol('issue-context')

export function provideIssueContext(ctx: IssueContext) {
  provide(ISSUE_CTX_KEY, ctx)
}

/**
 * Inject shared lookup data from the nearest provider.
 * When `optional` is true (default false), returns empty-array refs instead of
 * throwing — useful for components that can live outside the provider tree.
 */
export function useIssueContext(optional?: boolean): IssueContext {
  const ctx = inject(ISSUE_CTX_KEY)
  if (!ctx) {
    if (optional) {
      return {
        users: ref([]),
        allTags: ref([]),
        costUnits: ref([]),
        releases: ref([]),
        projects: ref([]),
        sprints: ref([]),
      }
    }
    throw new Error('useIssueContext() called outside provider')
  }
  return ctx
}
