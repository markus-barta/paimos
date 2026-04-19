<script setup lang="ts">
/**
 * ImportCollisionModal — shared collision/confirmation dialog for CSV and
 * (future) Jira imports.
 *
 * Emits:
 *   confirm(strategy, projectName?) — user confirmed, proceed with import
 *   cancel                          — user cancelled
 */

export interface PreflightResult {
  project_key: string
  project_exists: boolean
  total: number
  collision_keys: string[]
  collision_count: number
  new_count: number
}

export type CollisionStrategy = 'skip' | 'overwrite' | 'insert'

const props = defineProps<{
  open: boolean
  preflight: PreflightResult | null
}>()

const emit = defineEmits<{
  confirm: [strategy: CollisionStrategy, projectName: string]
  cancel: []
}>()

import { ref, watch, onUnmounted } from 'vue'
import AppModal from '@/components/AppModal.vue'

const strategy    = ref<CollisionStrategy>('skip')
const projectName = ref('')

// Pre-fill project name with key when preflight arrives
watch(() => props.preflight, pf => {
  if (pf && !pf.project_exists) {
    projectName.value = pf.project_key
  }
  // Default to 'skip' when collisions exist, 'insert' when none
  if (pf && pf.collision_count === 0) {
    strategy.value = 'insert'
  } else {
    strategy.value = 'skip'
  }
}, { immediate: true })

function doConfirm() {
  emit('confirm', strategy.value, projectName.value)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') { emit('cancel'); e.preventDefault() }
  if (e.key === 'Enter') { doConfirm(); e.preventDefault() }
}
watch(() => props.open, (v) => {
  if (v) window.addEventListener('keydown', onKey)
  else   window.removeEventListener('keydown', onKey)
})
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <AppModal
    :title="preflight?.project_exists ? `Import into ${preflight.project_key}` : 'Import new project'"
    :open="open"
    @close="emit('cancel')"
    max-width="580px"
  >
    <div v-if="preflight" class="icm-body">

      <!-- New project: show editable name field -->
      <template v-if="!preflight.project_exists">
        <p class="icm-summary">
          <strong>{{ preflight.total }}</strong> issue{{ preflight.total !== 1 ? 's' : '' }} found in CSV.
          This project does not exist yet and will be created.
        </p>
        <div class="icm-field">
          <label class="icm-label">Project name</label>
          <input v-model="projectName" type="text" class="icm-input" :placeholder="preflight.project_key" />
          <p class="icm-hint">Key will be <code>{{ preflight.project_key }}</code> — you can rename the project later.</p>
        </div>
      </template>

      <!-- Existing project: show collision options -->
      <template v-else>
        <p class="icm-summary">
          <strong>{{ preflight.total }}</strong> issue{{ preflight.total !== 1 ? 's' : '' }} found in CSV
          <template v-if="preflight.collision_count > 0">
            · <span class="icm-collision-count">{{ preflight.collision_count }} already exist</span>
          </template>
        </p>

        <div v-if="preflight.collision_count > 0" class="icm-strategy">
          <p class="icm-label">What should happen with existing issues?</p>

          <label class="icm-option" :class="{ selected: strategy === 'skip' }">
            <input type="radio" v-model="strategy" value="skip" />
            <div class="icm-option-body">
              <span class="icm-option-title">Skip existing</span>
              <span class="icm-option-desc">
                Import {{ preflight.new_count }} new issue{{ preflight.new_count !== 1 ? 's' : '' }},
                skip {{ preflight.collision_count }} existing
              </span>
            </div>
          </label>

          <label class="icm-option" :class="{ selected: strategy === 'overwrite' }">
            <input type="radio" v-model="strategy" value="overwrite" />
            <div class="icm-option-body">
              <span class="icm-option-title">Overwrite existing</span>
              <span class="icm-option-desc">
                Update {{ preflight.collision_count }} existing,
                import {{ preflight.new_count }} new
              </span>
            </div>
          </label>

          <label class="icm-option" :class="{ selected: strategy === 'insert' }">
            <input type="radio" v-model="strategy" value="insert" />
            <div class="icm-option-body">
              <span class="icm-option-title">Insert anyway</span>
              <span class="icm-option-desc">
                Create new issues for all rows — may create duplicates
              </span>
            </div>
          </label>

          <!-- Collision key list (collapsed if many) -->
          <details v-if="preflight.collision_keys?.length" class="icm-collisions">
            <summary>{{ preflight.collision_count }} conflicting key{{ preflight.collision_count !== 1 ? 's' : '' }}</summary>
            <div class="icm-keys">
              <code v-for="k in preflight.collision_keys" :key="k">{{ k }}</code>
            </div>
          </details>
        </div>

        <p v-else class="icm-no-collision">
          No conflicts — all {{ preflight.total }} issues will be imported.
        </p>
      </template>

      <div class="icm-actions">
        <button class="btn btn-ghost" @click="emit('cancel')"><u>C</u>ancel</button>
        <button class="btn btn-primary" @click="doConfirm">
          <template v-if="preflight.project_exists"><u>I</u>mport →</template>
          <template v-else><u>C</u>reate &amp; Import →</template>
        </button>
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.icm-body { display: flex; flex-direction: column; gap: 1.1rem; }

.icm-summary { font-size: 14px; color: var(--text); line-height: 1.5; }
.icm-collision-count { color: #e65100; font-weight: 600; }
.icm-no-collision { font-size: 13px; color: #155724; background: #d4edda; padding: .5rem .75rem; border-radius: var(--radius); }

/* New project name field */
.icm-field { display: flex; flex-direction: column; gap: .35rem; }
.icm-label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--text-muted); }
.icm-input { font-size: 14px; }
.icm-hint  { font-size: 11px; color: var(--text-muted); margin-top: .1rem; }
.icm-hint code { font-family: monospace; font-weight: 700; color: var(--text); }

/* Strategy radio group */
.icm-strategy { display: flex; flex-direction: column; gap: .5rem; }
.icm-option {
  display: grid;
  grid-template-columns: 1rem 1fr;
  align-items: start;
  gap: .65rem;
  padding: .75rem 1rem;
  border: 1px solid var(--border); border-radius: var(--radius);
  cursor: pointer; transition: border-color .12s, background .12s;
}
.icm-option:hover { border-color: var(--bp-blue); background: var(--bg); }
.icm-option.selected { border-color: var(--bp-blue); background: var(--bp-blue-pale); }
.icm-option input[type="radio"] { margin-top: 2px; accent-color: var(--bp-blue); }
.icm-option-body { display: flex; flex-direction: column; gap: .2rem; min-width: 0; }
.icm-option-title { font-size: 13px; font-weight: 600; color: var(--text); }
.icm-option-desc  { font-size: 12px; color: var(--text-muted); line-height: 1.45; }

/* Collision key list */
.icm-collisions { font-size: 12px; color: var(--text-muted); margin-top: .25rem; }
.icm-collisions summary { cursor: pointer; user-select: none; }
.icm-keys { display: flex; flex-wrap: wrap; gap: .3rem; margin-top: .5rem; }
.icm-keys code { font-family: monospace; font-size: 11px; background: var(--bg); border: 1px solid var(--border); padding: .1rem .4rem; border-radius: 4px; }

.icm-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: .25rem; }
</style>
