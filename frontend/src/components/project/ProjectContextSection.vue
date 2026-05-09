<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { computed, onMounted, ref, watch } from 'vue'
import { errMsg } from '@/api/client'
import type { ProjectRepo } from '@/types'
import {
  addProjectContextRepo,
  loadProjectContext,
  migrateManifestToKnowledge,
  removeProjectContextRepo,
  type ManifestMigrationResult,
} from '@/services/projectContext'
import ProjectManifestTabs from '@/components/project/ProjectManifestTabs.vue'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  projectId: number
  canWrite: boolean
  showHeader?: boolean
}>()

// PAI-178: parents mount this component as a sentinel and listen
// to `populated` to light up a toggle-button badge. True when at
// least one repo is linked OR any of the manifest tabs has content.
const emit = defineEmits<{
  populated: [v: boolean]
  summary: [payload: { repoCount: number; hasManifest: boolean; populated: boolean }]
}>()

const repos = ref<ProjectRepo[]>([])
const loading = ref(true)
const saveError = ref('')
const repoForm = ref({ url: '', default_branch: 'main', label: '' })
const addingRepo = ref(false)

// Driven by ProjectManifestTabs via @populated / @summary.
// hasManifestArea aggregates manifest + guardrails + glossary so the
// parent's existing `hasManifest` semantics still mean "the manifest
// area is populated".
const hasManifestArea = ref(false)

const isPopulated = computed(() => repos.value.length > 0 || hasManifestArea.value)
watch(isPopulated, (v) => emit('populated', v), { immediate: true })
watch(
  [repos, hasManifestArea, isPopulated],
  () => {
    emit('summary', {
      repoCount: repos.value.length,
      hasManifest: hasManifestArea.value,
      populated: isPopulated.value,
    })
  },
  { immediate: true },
)

async function load() {
  loading.value = true
  saveError.value = ''
  try {
    const data = await loadProjectContext(props.projectId)
    repos.value = data.repos
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to load project context.')
  } finally {
    loading.value = false
  }
}

async function addRepo() {
  if (!repoForm.value.url.trim()) return
  addingRepo.value = true
  saveError.value = ''
  try {
    await addProjectContextRepo(props.projectId, repoForm.value)
    repoForm.value = { url: '', default_branch: 'main', label: '' }
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to add repo.')
  } finally {
    addingRepo.value = false
  }
}

async function removeRepo(repo: ProjectRepo) {
  if (!confirm(`Remove repo "${repo.label || repo.url}"?`)) return
  saveError.value = ''
  try {
    await removeProjectContextRepo(props.projectId, repo.id)
    await load()
  } catch (e) {
    saveError.value = errMsg(e, 'Failed to remove repo.')
  }
}

function onManifestPopulated(v: boolean) { hasManifestArea.value = v }

// PAI-357 — admin-only migration of legacy manifest content into the
// PAI-338 knowledge plane. Two-step: dry-run preview, then commit.
// Force overrides idempotency / overwrites existing slugs.
const auth = useAuthStore()
const isAdmin = computed(() => auth.user?.role === 'admin')
const migrationOpen = ref(false)
const migrationLoading = ref(false)
const migrationResult = ref<ManifestMigrationResult | null>(null)
const migrationError = ref('')

async function openMigrationPreview() {
  migrationOpen.value = true
  migrationError.value = ''
  migrationResult.value = null
  migrationLoading.value = true
  try {
    migrationResult.value = await migrateManifestToKnowledge(props.projectId, { dryRun: true })
  } catch (e) {
    migrationError.value = errMsg(e, 'Failed to preview migration.')
  } finally {
    migrationLoading.value = false
  }
}

async function commitMigration(force: boolean) {
  migrationError.value = ''
  migrationLoading.value = true
  try {
    migrationResult.value = await migrateManifestToKnowledge(props.projectId, { force })
  } catch (e) {
    migrationError.value = errMsg(e, 'Migration failed.')
  } finally {
    migrationLoading.value = false
  }
}

function closeMigration() {
  migrationOpen.value = false
  migrationResult.value = null
  migrationError.value = ''
}

onMounted(load)
</script>

<template>
  <section class="context-section">
    <div v-if="showHeader !== false" class="context-header">
      <div>
        <h2 class="context-title">Project Context</h2>
        <p class="context-desc">Repos and manifest power agent-friendly context, anchors, and retrieval.</p>
      </div>
      <button class="btn btn-ghost btn-sm" @click="load" :disabled="loading">Refresh</button>
    </div>

    <div v-if="saveError" class="context-error">{{ saveError }}</div>

    <div class="context-grid">
      <div class="context-card">
        <div class="card-head">
          <div>
            <h3>Repos</h3>
            <p>Used for anchors, deep links, and future multi-repo retrieval.</p>
          </div>
        </div>

        <LoadingText v-if="loading" class="context-empty" label="Loading repos…" />
        <div v-else-if="!repos.length" class="context-empty">No repos linked yet.</div>
        <div v-else class="repo-list">
          <div v-for="repo in repos" :key="repo.id" class="repo-row">
            <div class="repo-main">
              <div class="repo-name">{{ repo.label || repo.url }}</div>
              <a :href="repo.url" target="_blank" rel="noopener" class="repo-url">{{ repo.url }}</a>
              <div class="repo-meta">default branch: <strong>{{ repo.default_branch }}</strong></div>
            </div>
            <button v-if="canWrite" class="btn btn-ghost btn-sm danger" @click="removeRepo(repo)">Remove</button>
          </div>
        </div>

        <div v-if="canWrite" class="repo-form">
          <input v-model="repoForm.label" type="text" placeholder="Label (e.g. backend)" />
          <input v-model="repoForm.url" type="url" placeholder="https://github.com/org/repo" />
          <input v-model="repoForm.default_branch" type="text" placeholder="main" />
          <button class="btn btn-primary btn-sm" @click="addRepo" :disabled="addingRepo">
            {{ addingRepo ? 'Adding…' : 'Add repo' }}
          </button>
        </div>
      </div>

      <div class="context-card">
        <!-- PAI-356/-357: the manifest editor's content (Manifest /
             Guardrails / Glossary / Dev / Ops) is migrating into the
             knowledge plane as memory / runbook / guideline entries.
             The badge signals "this surface is on its way out" without
             yet breaking access to the legacy data — PAI-358 deletes
             the editor after the migration window. -->
        <div v-if="hasManifestArea" class="legacy-banner" role="note">
          <strong>Legacy</strong>
          <span>This area is migrating to the project's Knowledge tab — see PAI-357.</span>
          <button v-if="isAdmin && canWrite" class="legacy-banner__action btn btn-ghost btn-sm" @click="openMigrationPreview">
            Migrate to Knowledge…
          </button>
        </div>

        <!-- PAI-357 admin migration panel. Lazy: only renders when
             the admin clicks the trigger above. dry-run preview lists
             what will be created; Commit + Force commit ship the
             writes through the canonical knowledge.CreateEntryHook. -->
        <div v-if="migrationOpen" class="migration-panel" role="dialog" aria-label="Migrate manifest to knowledge">
          <div class="migration-panel__head">
            <strong>Migrate manifest → Knowledge plane</strong>
            <button class="btn btn-ghost btn-sm" @click="closeMigration">Close</button>
          </div>
          <div v-if="migrationError" class="context-error">{{ migrationError }}</div>
          <LoadingText v-if="migrationLoading" label="Working…" class="context-empty" />
          <template v-else-if="migrationResult">
            <div class="migration-summary">
              <span><strong>{{ migrationResult.created.length }}</strong> {{ migrationResult.dry_run ? 'planned' : 'created' }}</span>
              <span><strong>{{ migrationResult.conflicts.length }}</strong> conflicts</span>
              <span><strong>{{ migrationResult.skipped.length }}</strong> skipped</span>
              <span v-if="migrationResult.migrated_at">
                · stamped <code>{{ migrationResult.migrated_at }}</code>
              </span>
            </div>
            <ul v-if="migrationResult.created.length" class="migration-list">
              <li v-for="(item, i) in migrationResult.created" :key="`c-${i}`">
                <span class="migration-pill">{{ item.kind }}</span>
                <code v-if="item.slug">{{ item.slug }}</code>
                <code v-else-if="item.agent_name">agent: {{ item.agent_name }}</code>
                <span class="migration-title">{{ item.title }}</span>
              </li>
            </ul>
            <ul v-if="migrationResult.conflicts.length" class="migration-list migration-list--warn">
              <li v-for="(item, i) in migrationResult.conflicts" :key="`x-${i}`">
                <span class="migration-pill migration-pill--warn">{{ item.kind }}</span>
                <code v-if="item.slug">{{ item.slug }}</code>
                <span class="migration-title">{{ item.title }}</span>
                <span class="migration-reason">{{ item.reason }}</span>
              </li>
            </ul>
          </template>
          <div class="migration-actions">
            <button
              v-if="migrationResult?.dry_run"
              class="btn btn-primary btn-sm"
              :disabled="migrationLoading"
              @click="commitMigration(false)"
            >Commit migration</button>
            <button
              class="btn btn-ghost btn-sm"
              :disabled="migrationLoading"
              @click="commitMigration(true)"
              :title="migrationResult && !migrationResult.dry_run ? 'Re-run with overwrite' : 'Force-overwrite existing slugs / agent bodies'"
            >Force commit</button>
          </div>
        </div>
        <ProjectManifestTabs
          :project-id="projectId"
          :can-write="canWrite"
          @populated="onManifestPopulated"
        />
      </div>
    </div>
  </section>
</template>

<style scoped>
.context-section { margin-bottom: 1.5rem; display: flex; flex-direction: column; gap: 1rem; }
.context-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
.context-title { font-size: 18px; font-weight: 800; color: var(--text); margin: 0 0 .15rem; }
.context-desc { margin: 0; color: var(--text-muted); font-size: 13px; }
.context-grid { display: grid; grid-template-columns: 1fr 1.1fr; gap: 1rem; }
.context-card { background: var(--bg-card); border: 1px solid var(--border); border-radius: 12px; box-shadow: var(--shadow); padding: 1rem 1.1rem; display: flex; flex-direction: column; gap: .9rem; }
.card-head h3 { margin: 0 0 .2rem; font-size: 15px; }
.card-head p { margin: 0; color: var(--text-muted); font-size: 12px; }
.context-empty { color: var(--text-muted); font-size: 13px; }
/* PAI-356/-357 — soft-deprecation banner above the manifest editor.
   Visible only when content exists (so empty-state cards aren't
   distracted by it). Yellow/amber to read as "heads up", not red
   "broken". */
.legacy-banner {
  display: flex;
  align-items: center;
  gap: .55rem;
  padding: .5rem .7rem;
  font-size: 12px;
  color: #92400e;
  background: #fffbeb;
  border: 1px solid #fde68a;
  border-radius: 8px;
}
.legacy-banner strong {
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .04em;
  font-size: 11px;
}
.legacy-banner__action {
  margin-left: auto;
}

/* PAI-357 — migration panel. Inline (not modal) so the admin can keep
   the manifest editor visible while reviewing the dry-run. Bordered
   card so it reads as a separate sub-pane within the manifest card. */
.migration-panel {
  display: flex;
  flex-direction: column;
  gap: .55rem;
  padding: .8rem .9rem;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 10px;
  font-size: 13px;
}
.migration-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .5rem;
}
.migration-summary {
  display: flex;
  flex-wrap: wrap;
  gap: .9rem;
  font-size: 12px;
  color: var(--text-muted);
}
.migration-summary code {
  font-size: 11px;
  background: var(--surface-2);
  padding: 0 .3rem;
  border-radius: 4px;
}
.migration-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: .35rem;
}
.migration-list li {
  display: flex;
  align-items: center;
  gap: .5rem;
  font-size: 12px;
}
.migration-list code {
  font-size: 11px;
  background: var(--surface-2);
  padding: 0 .3rem;
  border-radius: 4px;
}
.migration-pill {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .04em;
  padding: 0 .4rem;
  border-radius: 999px;
  background: var(--bp-blue-pale, #dce9f4);
  color: var(--bp-blue-dark, #1f4d75);
}
.migration-pill--warn {
  background: #fde68a;
  color: #92400e;
}
.migration-list--warn {
  border-top: 1px dashed var(--border);
  padding-top: .35rem;
  margin-top: .15rem;
}
.migration-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.migration-reason {
  font-size: 11px;
  color: #92400e;
}
.migration-actions {
  display: flex;
  gap: .4rem;
  justify-content: flex-end;
  margin-top: .15rem;
}
.context-error { color: #b42318; background: #fef3f2; border: 1px solid #fecdca; border-radius: 10px; padding: .7rem .85rem; font-size: 13px; }
.context-ok { color: #166534; background: #ecfdf3; border: 1px solid #abefc6; border-radius: 10px; padding: .7rem .85rem; font-size: 13px; }
.repo-list { display: flex; flex-direction: column; gap: .7rem; }
.repo-row { display: flex; align-items: flex-start; justify-content: space-between; gap: .9rem; padding: .75rem .8rem; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
.repo-main { min-width: 0; }
.repo-name { font-size: 13px; font-weight: 700; color: var(--text); }
.repo-url { display: inline-block; margin-top: .15rem; font-size: 12px; color: var(--text-muted); word-break: break-all; text-decoration: none; }
.repo-url:hover { color: var(--bp-blue-dark); text-decoration: underline; }
.repo-meta { margin-top: .25rem; font-size: 12px; color: var(--text-muted); }
.repo-form { display: grid; grid-template-columns: 1fr 1.4fr .7fr auto; gap: .55rem; }
.repo-form input {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
  color: var(--text);
  font: inherit;
  padding: .55rem .65rem;
}
@media (max-width: 980px) {
  .context-grid { grid-template-columns: 1fr; }
  .repo-form { grid-template-columns: 1fr; }
}
</style>
