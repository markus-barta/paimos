<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '@/api/client'
import AppIcon from '@/components/AppIcon.vue'
import MetaSelect from '@/components/MetaSelect.vue'
import type { MetaOption } from '@/components/MetaSelect.vue'

interface BpProject { id: number; key: string; name: string }

const router = useRouter()
const projects = ref<BpProject[]>([])
const selectedProject = ref('')

function monthStart(d = new Date()): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`
}

function monthEnd(d = new Date()): string {
  const end = new Date(d.getFullYear(), d.getMonth() + 1, 0)
  return `${end.getFullYear()}-${String(end.getMonth() + 1).padStart(2, '0')}-${String(end.getDate()).padStart(2, '0')}`
}

const fromDate = ref(monthStart())
const toDate = ref(monthEnd())

onMounted(async () => {
  projects.value = await api.get<BpProject[]>('/projects')
})

const projectOptions = computed<MetaOption[]>(() =>
  projects.value.map(p => ({ value: String(p.id), label: `${p.key} - ${p.name}` }))
)

const canOpen = computed(() => !!selectedProject.value && !!fromDate.value && !!toDate.value)

function openIssueList() {
  if (!canOpen.value) return
  router.push({
    path: `/projects/${selectedProject.value}`,
    query: {
      report: 'lieferbericht',
      date_field: 'completed',
      date_from: fromDate.value,
      date_to: toDate.value,
    },
  })
}
</script>

<template>
  <Teleport defer to="#app-header-left">
    <span class="ah-title">Projektbericht</span>
    <span class="ah-subtitle">Uses project issue-list filters</span>
  </Teleport>

  <section class="lb-handoff">
    <div class="lb-main">
      <div class="lb-field">
        <label>Project</label>
        <MetaSelect v-model="selectedProject" :options="projectOptions" placeholder="Select project" />
      </div>
      <div class="lb-field">
        <label>Completed from</label>
        <input v-model="fromDate" type="date" />
      </div>
      <div class="lb-field">
        <label>Completed to</label>
        <input v-model="toDate" type="date" />
      </div>
      <button class="btn btn-primary" :disabled="!canOpen" @click="openIssueList">
        <AppIcon name="arrow-right" :size="14" />
        Open issue list
      </button>
    </div>
    <p class="lb-note">
      The PDF export now lives in the project issue list. This opens the list with a completed-date reporting preset; adjust filters there, then use the project header menu.
    </p>
  </section>
</template>

<style scoped>
.lb-handoff {
  max-width: 760px;
  display: flex;
  flex-direction: column;
  gap: .85rem;
}

.lb-main {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: .85rem;
  padding: 1rem 1.25rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow);
}

.lb-field {
  display: flex;
  flex-direction: column;
  gap: .3rem;
  min-width: 160px;
}

.lb-field:first-child { min-width: 280px; }

.lb-field label {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .06em;
  color: var(--text-muted);
}

.lb-field input {
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: .4rem .55rem;
  font-size: 13px;
  font-family: inherit;
  color: var(--text);
  background: var(--bg);
}

.lb-note {
  margin: 0;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.45;
}
</style>
