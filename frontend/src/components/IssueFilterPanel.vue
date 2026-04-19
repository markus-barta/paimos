<script setup lang="ts">
import AppIcon from '@/components/AppIcon.vue'
import TagChip from '@/components/TagChip.vue'
import type { Issue, Tag, Sprint, User } from '@/types'
import type { Project } from '@/types'
import {
  NEG, isNeg, toggleFilter, toggleFilterCheckbox,
  OTHER_STATUS_SENTINEL,
  TYPE_OPTIONS, STATUS_OPTIONS, PRIORITY_OPTIONS,
} from '@/composables/useIssueFilter'
import type { ComplexTabKey } from '@/composables/useIssueFilter'

const props = defineProps<{
  // Filter arrays (v-model)
  filterType: string[]
  filterStatus: string[]
  filterPriority: string[]
  filterProjects: string[]
  filterAssignee: string[]
  filterTags: string[]
  filterCostUnit: string[]
  filterRelease: string[]
  filterSprints: string[]
  filterEpic: string[]
  showArchivedSprints: boolean
  // Tab state
  complexTab: ComplexTabKey
  complexTabSearch: string
  complexTabs: { key: ComplexTabKey; label: string }[]
  complexBadge: Record<string, number>
  // Data
  activeFilterCount: number
  issues: Issue[]
  projects?: Project[]
  pickerProjects: Project[]
  pickerUsers: User[]
  pickerTags: Tag[]
  pickerCostUnits: string[]
  pickerReleases: string[]
  pickerSprints: Sprint[]
  assigneeIsAny: boolean
}>()

const emit = defineEmits<{
  'update:filterType': [val: string[]]
  'update:filterStatus': [val: string[]]
  'update:filterPriority': [val: string[]]
  'update:filterProjects': [val: string[]]
  'update:filterAssignee': [val: string[]]
  'update:filterTags': [val: string[]]
  'update:filterCostUnit': [val: string[]]
  'update:filterRelease': [val: string[]]
  'update:filterSprints': [val: string[]]
  'update:filterEpic': [val: string[]]
  'update:showArchivedSprints': [val: boolean]
  'update:complexTab': [val: ComplexTabKey]
  'update:complexTabSearch': [val: string]
  clearAll: []
  setAssigneeAny: []
}>()
</script>

<template>
  <div class="filter-panel">
    <div class="fp-header">
      <span class="fp-title">Filters</span>
      <button v-if="activeFilterCount > 0" class="fp-clear" @click="emit('clearAll')">Clear all</button>
    </div>

    <div class="fp-body">
      <!-- Left: Tier 1 simple checkboxes -->
      <div class="fp-left">
        <div class="fp-grid">
          <!-- Type -->
          <div class="fp-group">
            <div class="fp-group-label">Type</div>
            <div v-for="opt in TYPE_OPTIONS" :key="opt.value"
              :class="['fp-option', filterType.includes(NEG+opt.value) ? 'fp-option--neg' : '']"
              @click="emit('update:filterType', toggleFilter(filterType, opt.value))">
              <input type="checkbox"
                :checked="filterType.includes(opt.value) || filterType.includes(NEG+opt.value)"
                :class="{ 'fp-cb--neg': filterType.includes(NEG+opt.value) }"
                @click.stop @change="emit('update:filterType', toggleFilterCheckbox(filterType, opt.value))" />
              <span class="fp-icon" v-if="opt.icon" v-html="opt.icon"></span>
              <span :class="{ 'fp-label--neg': filterType.includes(NEG+opt.value) }">{{ opt.label }}</span>
              <span v-if="filterType.includes(NEG+opt.value)" class="fp-neg-badge">NOT</span>
            </div>
          </div>

          <!-- Status -->
          <div class="fp-group">
            <div class="fp-group-label">Status</div>
            <div v-for="opt in STATUS_OPTIONS" :key="opt.value"
              :class="['fp-option', filterStatus.includes(NEG+opt.value) ? 'fp-option--neg' : '']"
              @click="emit('update:filterStatus', toggleFilter(filterStatus, opt.value))">
              <input type="checkbox"
                :checked="filterStatus.includes(opt.value) || filterStatus.includes(NEG+opt.value)"
                :class="{ 'fp-cb--neg': filterStatus.includes(NEG+opt.value) }"
                @click.stop @change="emit('update:filterStatus', toggleFilterCheckbox(filterStatus, opt.value))" />
              <span v-if="opt.dotColor" class="fp-dot"
                :class="{ 'fp-dot--outline': opt.dotOutline }"
                :style="opt.dotOutline ? { borderColor: opt.dotColor } : { background: opt.dotColor }">
              </span>
              <span :class="{ 'fp-label--neg': filterStatus.includes(NEG+opt.value) }">{{ opt.label }}</span>
              <span v-if="filterStatus.includes(NEG+opt.value)" class="fp-neg-badge">NOT</span>
            </div>
            <!-- Other / unknown status -->
            <div :class="['fp-option', filterStatus.includes(NEG+OTHER_STATUS_SENTINEL) ? 'fp-option--neg' : '']"
              @click="emit('update:filterStatus', toggleFilter(filterStatus, OTHER_STATUS_SENTINEL))">
              <input type="checkbox"
                :checked="filterStatus.includes(OTHER_STATUS_SENTINEL) || filterStatus.includes(NEG+OTHER_STATUS_SENTINEL)"
                :class="{ 'fp-cb--neg': filterStatus.includes(NEG+OTHER_STATUS_SENTINEL) }"
                @click.stop @change="emit('update:filterStatus', toggleFilterCheckbox(filterStatus, OTHER_STATUS_SENTINEL))" />
              <span class="fp-dot" style="background:#d1d5db"></span>
              <span :class="{ 'fp-label--neg': filterStatus.includes(NEG+OTHER_STATUS_SENTINEL) }">Other / unknown</span>
              <span v-if="filterStatus.includes(NEG+OTHER_STATUS_SENTINEL)" class="fp-neg-badge">NOT</span>
            </div>
          </div>

          <!-- Priority -->
          <div class="fp-group">
            <div class="fp-group-label">Priority</div>
            <div v-for="opt in PRIORITY_OPTIONS" :key="opt.value"
              :class="['fp-option', filterPriority.includes(NEG+opt.value) ? 'fp-option--neg' : '']"
              @click="emit('update:filterPriority', toggleFilter(filterPriority, opt.value))">
              <input type="checkbox"
                :checked="filterPriority.includes(opt.value) || filterPriority.includes(NEG+opt.value)"
                :class="{ 'fp-cb--neg': filterPriority.includes(NEG+opt.value) }"
                @click.stop @change="emit('update:filterPriority', toggleFilterCheckbox(filterPriority, opt.value))" />
              <AppIcon v-if="opt.iconName" :name="opt.iconName" :size="12" :stroke-width="2.5" :style="{ color: opt.arrowColor }" />
              <span :class="{ 'fp-label--neg': filterPriority.includes(NEG+opt.value) }">{{ opt.label }}</span>
              <span v-if="filterPriority.includes(NEG+opt.value)" class="fp-neg-badge">NOT</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Vertical divider -->
      <div v-if="complexTabs.length" class="fp-vdivider"></div>

      <!-- Right: Tier 2 tab-based searchable pickers -->
      <div v-if="complexTabs.length" class="fp-right">
        <div class="fp-tabs" role="tablist">
          <button
            v-for="tab in complexTabs"
            :key="tab.key"
            role="tab"
            :class="['fp-tab', { active: complexTab === tab.key }]"
            @click="emit('update:complexTab', tab.key); emit('update:complexTabSearch', '')"
          >
            {{ tab.label }}
            <span v-if="complexBadge[tab.key] > 0" class="fp-tab-badge">{{ complexBadge[tab.key] }}</span>
          </button>
        </div>

        <div class="fp-picker">
          <input
            :value="complexTabSearch"
            @input="emit('update:complexTabSearch', ($event.target as HTMLInputElement).value)"
            class="fp-search"
            type="search"
            placeholder="Search..."
            autocomplete="off"
          />
          <div class="fp-picker-list">
            <!-- Project tab -->
            <template v-if="complexTab === 'project'">
              <label v-for="p in pickerProjects" :key="p.id" class="fp-option">
                <input type="checkbox"
                  :checked="filterProjects.includes(String(p.id))"
                  @change="emit('update:filterProjects', toggleFilterCheckbox(filterProjects, String(p.id)))" />
                <span class="fp-proj-key">{{ p.key }}</span>
                <span class="fp-proj-name">{{ p.name }}</span>
              </label>
            </template>

            <!-- Assignee tab -->
            <template v-else-if="complexTab === 'assignee'">
              <label class="fp-option fp-option--pinned">
                <input type="radio"
                  :checked="assigneeIsAny"
                  @change="emit('setAssigneeAny')" />
                <span>Any</span>
              </label>
              <label v-if="!complexTabSearch || 'unassigned'.includes(complexTabSearch.toLowerCase())" class="fp-option fp-option--pinned">
                <input type="checkbox"
                  :checked="filterAssignee.includes('unassigned')"
                  @change="emit('update:filterAssignee', toggleFilterCheckbox(filterAssignee, 'unassigned'))" />
                <span>Unassigned</span>
              </label>
              <label v-for="u in pickerUsers" :key="u.id" class="fp-option">
                <input type="checkbox"
                  :checked="filterAssignee.includes(String(u.id))"
                  @change="emit('update:filterAssignee', toggleFilterCheckbox(filterAssignee, String(u.id)))" />
                <span>{{ u.username }}</span>
              </label>
            </template>

            <!-- Tags tab -->
            <template v-else-if="complexTab === 'tags'">
              <label v-for="t in pickerTags" :key="t.id" class="fp-option">
                <input type="checkbox"
                  :checked="filterTags.includes(String(t.id))"
                  @change="emit('update:filterTags', toggleFilterCheckbox(filterTags, String(t.id)))" />
                <TagChip :tag="t" />
              </label>
            </template>

            <!-- Cost Unit tab -->
            <template v-else-if="complexTab === 'costunit'">
              <label v-for="cu in pickerCostUnits" :key="cu" class="fp-option">
                <input type="checkbox"
                  :checked="filterCostUnit.includes(cu)"
                  @change="emit('update:filterCostUnit', toggleFilterCheckbox(filterCostUnit, cu))" />
                <span>{{ cu }}</span>
              </label>
            </template>

            <!-- Release tab -->
            <template v-else-if="complexTab === 'release'">
              <label v-for="r in pickerReleases" :key="r" class="fp-option">
                <input type="checkbox"
                  :checked="filterRelease.includes(r)"
                  @change="emit('update:filterRelease', toggleFilterCheckbox(filterRelease, r))" />
                <span>{{ r }}</span>
              </label>
            </template>

            <!-- Sprint tab -->
            <template v-else-if="complexTab === 'sprint'">
              <label v-for="s in pickerSprints" :key="s.id" class="fp-option">
                <input type="checkbox"
                  :checked="filterSprints.includes(String(s.id))"
                  @change="emit('update:filterSprints', toggleFilterCheckbox(filterSprints, String(s.id)))" />
                <span :class="{ 'fp-sprint-archived': s.archived }">{{ s.title }}</span>
                <span v-if="s.start_date" class="fp-sprint-dates">{{ s.start_date.slice(0,10) }}</span>
              </label>
              <label class="fp-option fp-option--toggle" style="margin-top:.5rem; border-top: 1px solid var(--border); padding-top:.5rem">
                <input type="checkbox" :checked="showArchivedSprints" @change="emit('update:showArchivedSprints', !showArchivedSprints)" />
                <span class="fp-muted">Show archived</span>
              </label>
            </template>

            <!-- Epic tab -->
            <template v-else-if="complexTab === 'epic'">
              <label v-for="epic in issues.filter(i => i.type === 'epic')" :key="epic.id" class="fp-option">
                <input type="checkbox"
                  :checked="filterEpic.includes(String(epic.id))"
                  @change="emit('update:filterEpic', toggleFilterCheckbox(filterEpic, String(epic.id)))" />
                <span>{{ epic.issue_key }} {{ epic.title }}</span>
              </label>
            </template>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.filter-panel {
  margin-top: .75rem; margin-bottom: 1.25rem;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow);
  padding: 1rem 1.25rem;
}
.fp-header {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: .85rem;
}
.fp-title { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .07em; color: var(--text-muted); }
.fp-clear { background: none; border: none; font-size: 12px; color: var(--bp-blue); cursor: pointer; padding: 0; font-family: inherit; }
.fp-clear:hover { text-decoration: underline; }

.fp-body { display: flex; gap: 0; align-items: flex-start; }
.fp-left { flex-shrink: 0; }
.fp-vdivider { width: 1px; background: var(--border); align-self: stretch; margin: 0 1.25rem; flex-shrink: 0; }
.fp-right { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: .6rem; }

.fp-grid { display: grid; grid-template-columns: repeat(3, minmax(110px, auto)); gap: .75rem 1.5rem; }
.fp-group { display: flex; flex-direction: column; gap: .3rem; }
.fp-group-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); margin-bottom: .2rem; }
.fp-option {
  display: flex; align-items: center; gap: .45rem;
  font-size: 13px; color: var(--text); cursor: pointer;
  padding: .15rem 0; user-select: none;
}
.fp-option input[type="checkbox"],
.fp-option input[type="radio"] { width: 14px; height: 14px; flex-shrink: 0; accent-color: var(--bp-blue); cursor: pointer; margin: 0; }
.fp-option--pinned { font-weight: 600; }
.fp-option--neg { opacity: .9; }
.fp-cb--neg { accent-color: #ef4444; }
.fp-label--neg { text-decoration: line-through; opacity: .7; }
.fp-neg-badge {
  font-size: 9px; font-weight: 800; letter-spacing: .05em; text-transform: uppercase;
  color: #991b1b; background: #fee2e2; border-radius: 3px;
  padding: 0 .3rem; margin-left: auto; flex-shrink: 0;
}
.fp-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; display: inline-block; }
.fp-dot--outline { background: transparent !important; border: 2px solid; }
.fp-icon { display: inline-flex; align-items: center; flex-shrink: 0; }
.fp-icon :deep(svg) { display: block; }
.fp-proj-key { font-size: 11px; font-weight: 700; font-family: monospace; color: var(--bp-blue); }
.fp-proj-name { color: var(--text-muted); font-size: 11px; margin-left: .15rem; }

.fp-tabs { display: flex; gap: .25rem; flex-wrap: wrap; }
.fp-tab {
  display: inline-flex; align-items: center; gap: .3rem;
  background: none; border: 1px solid var(--border); border-radius: 6px;
  padding: .25rem .65rem; font-size: 12px; font-weight: 600; cursor: pointer;
  color: var(--text-muted); font-family: inherit; transition: all .1s;
}
.fp-tab:hover { border-color: var(--bp-blue); color: var(--bp-blue-dark); }
.fp-tab.active { background: var(--bp-blue-pale); color: var(--bp-blue-dark); border-color: var(--bp-blue); }
.fp-tab-badge {
  display: inline-flex; align-items: center; justify-content: center;
  background: var(--bp-blue); color: #fff; border-radius: 20px;
  font-size: 10px; font-weight: 700; min-width: 16px; height: 16px; padding: 0 3px;
}

.fp-picker { display: flex; flex-direction: column; gap: .45rem; }
.fp-search {
  width: 100%; border: 1px solid var(--border); border-radius: 6px;
  padding: .3rem .65rem; font-size: 13px; font-family: inherit;
  background: var(--bg); color: var(--text); outline: none;
}
.fp-search:focus { border-color: var(--bp-blue); }
.fp-picker-list {
  display: flex; flex-direction: column; gap: .05rem;
  max-height: 200px; overflow-y: auto; padding-right: .25rem;
}
.fp-sprint-archived { opacity: .55; text-decoration: line-through; }
.fp-sprint-dates { font-size: 11px; color: var(--text-muted); margin-left: auto; }
.fp-muted { color: var(--text-muted); font-size: 12px; }
.fp-option--toggle { opacity: .85; }
</style>
