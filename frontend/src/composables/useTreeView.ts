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
 * useTreeView — Tree view state, expand/collapse, group expand.
 */
import { ref, computed, watch } from 'vue'
import type { Ref, ComputedRef } from 'vue'
import type { Issue } from '@/types'

export function useTreeView(
  treeView: Ref<boolean>,
  filterType: Ref<string[]>,
  issues: Ref<Issue[]>,
  issueTree: ComputedRef<(Issue & { children: (Issue & { children: Issue[] })[] })[]>,
  selectedIds: Ref<Set<number>>,
) {
  // Tree view collapse/expand state
  const treeExpanded = ref<Set<number>>(new Set())

  watch(treeView, (v) => {
    if (v && treeExpanded.value.size === 0) expandAllTreeNodes()
  })

  function toggleTreeNode(id: number) {
    const next = new Set(treeExpanded.value)
    if (next.has(id)) next.delete(id); else next.add(id)
    treeExpanded.value = next
  }

  function expandAllTreeNodes() {
    const ids = new Set<number>()
    for (const epic of issueTree.value) {
      ids.add(epic.id)
      for (const ticket of (epic.children ?? [])) {
        ids.add(ticket.id)
      }
    }
    treeExpanded.value = ids
  }

  function collapseAllTreeNodes() { treeExpanded.value = new Set() }

  // Tree selection — checking parent auto-checks children
  function toggleTreeSelect(issue: { id: number; children?: { id: number; children?: { id: number }[] }[] }) {
    const next = new Set(selectedIds.value)
    const shouldSelect = !next.has(issue.id)
    const toggle = (id: number) => { if (shouldSelect) next.add(id); else next.delete(id) }
    toggle(issue.id)
    for (const child of (issue.children ?? [])) {
      toggle(child.id)
      for (const grandchild of ((child as any).children ?? [])) {
        toggle(grandchild.id)
      }
    }
    selectedIds.value = next
  }

  // GROUP_TYPES for which create should be locked when the type filter is set to exactly one of them
  const GROUP_TYPES = new Set(['epic', 'cost_unit', 'release'])

  const derivedCreateType = computed<string | null>(() => {
    if (filterType.value.length !== 1) return null
    const t = filterType.value[0]
    return GROUP_TYPES.has(t) ? t : null
  })

  // Epic/group expand/collapse
  const expandedGroupIds = ref(new Set<number>())

  const isGroupExpandView = computed(() =>
    filterType.value.length === 1 && GROUP_TYPES.has(filterType.value[0])
  )

  function toggleGroupExpand(id: number) {
    const next = new Set(expandedGroupIds.value)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    expandedGroupIds.value = next
  }

  function childrenOf(parentId: number): Issue[] {
    return issues.value.filter(i => i.parent_id === parentId)
  }

  return {
    treeExpanded,
    toggleTreeNode, expandAllTreeNodes, collapseAllTreeNodes,
    toggleTreeSelect,
    derivedCreateType, GROUP_TYPES,
    expandedGroupIds, isGroupExpandView, toggleGroupExpand,
    childrenOf,
  }
}
