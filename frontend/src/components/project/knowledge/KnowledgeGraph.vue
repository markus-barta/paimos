<script setup lang="ts">
// PAI-350 — knowledge graph view. Renders the project's knowledge entries
// (memory / runbook / external_system / related_project / guideline) plus the
// issues linked to them, as an interactive force-directed graph. Data comes
// from GET /api/projects/:id/knowledge/graph (derived from existing relations,
// no schema change). force-graph is imported lazily so it never loads in
// SSR/tests (it touches `window` at module load).
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { api, errMsg } from '@/api/client'
import { getKnowledgeEntry } from '@/services/projectKnowledge'
import { useMarkdown } from '@/composables/useMarkdown'
import type { KnowledgeCategory } from '@/types'

const props = defineProps<{ projectId: number }>()
const router = useRouter()

interface GraphNode {
  id: number
  type: string
  slug: string
  title: string
  reference_count: number
}
interface GraphEdge {
  source: number
  target: number
  type: string
}

const TYPE_COLOR: Record<string, string> = {
  memory: '#3b82f6',
  runbook: '#10b981',
  external_system: '#f59e0b',
  related_project: '#8b5cf6',
  guideline: '#14b8a6',
  ticket: '#6b7280',
  task: '#9ca3af',
  epic: '#6366f1',
  agent: '#ec4899',
}
const colorFor = (t: string) => TYPE_COLOR[t] ?? '#94a3b8'
const typeLabel = (t: string) =>
  t
    .split('_')
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
// A very short on-canvas label for a node (title, else slug), truncated.
const shortLabel = (n: { title?: string; slug?: string }) => {
  const s = (n.title || n.slug || '').trim()
  return s.length > 16 ? s.slice(0, 15) + '…' : s
}

const container = ref<HTMLDivElement | null>(null)
const loading = ref(true)
const loadError = ref('')
const search = ref('')
const selected = ref<GraphNode | null>(null)
const nodeCount = ref(0)
const edgeCount = ref(0)

// PAI-350 — cap/focus paging so large graphs stay renderable.
const total = ref(0)
const truncated = ref(false)
const loadedCount = ref(0)
const focusId = ref<number | null>(null)
const limit = ref(500)

// distinct node types present, for the legend
const legendTypes = ref<string[]>([])

// PAI-350 — the clicked node's body, fetched on demand + rendered as markdown
// (read mode) in the side panel.
const selectedBody = ref('')
const bodyLoading = ref(false)
const bodyError = ref('')
const { html: bodyHtml } = useMarkdown(selectedBody, ref(true))

// eslint-disable-next-line @typescript-eslint/no-explicit-any
let graph: any = null
let raw: { nodes: GraphNode[]; edges: GraphEdge[] } = { nodes: [], edges: [] }
// Manual double-click detection (force-graph has no native double-click hook).
let lastClickId = 0
let lastClickAt = 0

function applyData() {
  const s = search.value.trim().toLowerCase()
  const nodes = s
    ? raw.nodes.filter(
        (n) => n.title.toLowerCase().includes(s) || n.slug.toLowerCase().includes(s) || n.type.includes(s),
      )
    : raw.nodes
  const visible = new Set(nodes.map((n) => n.id))
  const links = raw.edges
    .filter((e) => visible.has(e.source) && visible.has(e.target))
    .map((e) => ({ source: e.source, target: e.target, type: e.type }))
  nodeCount.value = nodes.length
  edgeCount.value = links.length
  graph?.graphData({ nodes: nodes.map((n) => ({ ...n })), links })
}

watch(search, applyData)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const params = new URLSearchParams({ limit: String(limit.value) })
    if (focusId.value != null) {
      params.set('focus', String(focusId.value))
      params.set('hops', '2')
    }
    const res = await api.get<{
      nodes: GraphNode[]
      edges: GraphEdge[]
      total: number
      truncated: boolean
    }>(`/projects/${props.projectId}/knowledge/graph?${params.toString()}`)
    raw = { nodes: res.nodes, edges: res.edges }
    total.value = res.total
    truncated.value = res.truncated
    loadedCount.value = res.nodes.length
    legendTypes.value = [...new Set(raw.nodes.map((n) => n.type))].sort()
    applyData()
  } catch (e) {
    loadError.value = errMsg(e, 'Failed to load the knowledge graph.')
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  const { default: ForceGraph } = await import('force-graph')
  if (!container.value) return
  // Chain on the `any`-typed `graph` so our domain-typed accessor callbacks
  // don't collide with force-graph's NodeObject generics under strict types.
  graph = new ForceGraph(container.value)
  graph
    .nodeId('id')
    .nodeLabel((n: GraphNode) => `${typeLabel(n.type)} · ${n.title}`)
    .nodeColor((n: GraphNode) => colorFor(n.type))
    .nodeRelSize(5)
    .nodeVal((n: GraphNode) => 1 + Math.min(n.reference_count, 12))
    // Persistent short label drawn below each node ('after' = on top of the
    // default circle, sitting clear of the connecting lines). A faint white
    // halo keeps it legible without obstructing edges.
    .nodeCanvasObjectMode(() => 'after')
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    .nodeCanvasObject((n: any, ctx: any, globalScale: number) => {
      const label = shortLabel(n)
      if (!label || globalScale < 0.25) return
      const fontSize = 11 / globalScale
      ctx.font = `${fontSize}px Sans-Serif`
      ctx.textAlign = 'center'
      ctx.textBaseline = 'top'
      const r = Math.sqrt(1 + Math.min(n.reference_count ?? 0, 12)) * 5
      const y = n.y + r + 1.5 / globalScale
      const w = ctx.measureText(label).width
      ctx.fillStyle = 'rgba(255,255,255,0.82)'
      ctx.fillRect(n.x - w / 2 - 1.5, y - 0.5, w + 3, fontSize + 1.5)
      ctx.fillStyle = '#334155'
      ctx.fillText(label, n.x, y)
    })
    .linkColor(() => 'rgba(148,163,184,0.5)')
    .linkLabel((e: GraphEdge) => typeLabel(e.type))
    .linkDirectionalArrowLength(3)
    .linkDirectionalArrowRelPos(1)
    .onNodeClick((n: GraphNode) => {
      // Single-click selects (side panel); a second click on the same node
      // within 300ms navigates to it.
      const now = Date.now()
      if (n.id === lastClickId && now - lastClickAt < 300) {
        lastClickId = 0
        selected.value = n
        openSelected()
        return
      }
      lastClickId = n.id
      lastClickAt = now
      selected.value = n
    })
    .onBackgroundClick(() => {
      selected.value = null
    })
    // Re-frame when the layout settles (after load / filter changes). The
    // engine only re-runs on data changes, so this never fights user pan/zoom.
    .onEngineStop(() => graph?.zoomToFit(400, 50))
  await load()
})

onBeforeUnmount(() => {
  // force-graph exposes _destructor for teardown
  graph?._destructor?.()
  graph = null
})

const KNOWLEDGE_TYPES = ['memory', 'runbook', 'external_system', 'related_project', 'guideline']
const selectedIsKnowledge = computed(
  () => !!selected.value && KNOWLEDGE_TYPES.includes(selected.value.type),
)

function openSelected() {
  const n = selected.value
  if (!n) return
  if (n.type === 'agent') {
    router.push({ query: { tab: 'agents' } })
  } else if (selectedIsKnowledge.value) {
    router.push({ query: { tab: 'knowledge', memory: n.slug } })
  } else {
    router.push(`/projects/${props.projectId}/issues/${n.id}`)
  }
}

// PAI-350 — fetch the clicked node's body for the read-mode markdown panel.
// Knowledge entries → their body; agents → their markdown body; issues → the
// description.
async function loadNodeBody(n: GraphNode): Promise<string> {
  if (KNOWLEDGE_TYPES.includes(n.type)) {
    const e = await getKnowledgeEntry(props.projectId, n.type as KnowledgeCategory, n.slug)
    return e.body || ''
  }
  if (n.type === 'agent') {
    const a = await api.get<{ body?: string; description?: string }>(
      `/projects/${props.projectId}/agents/${encodeURIComponent(n.slug)}`,
    )
    return a.body || a.description || ''
  }
  const iss = await api.get<{ description?: string }>(`/issues/${n.id}`)
  return iss.description || ''
}

watch(selected, async (n) => {
  selectedBody.value = ''
  bodyError.value = ''
  if (!n) return
  bodyLoading.value = true
  try {
    selectedBody.value = await loadNodeBody(n)
  } catch (e) {
    bodyError.value = errMsg(e, 'Could not load details.')
  } finally {
    bodyLoading.value = false
  }
})

// PAI-350 — paging controls. Focus reloads to the node's 2-hop neighborhood;
// Show all raises the cap to the full count; Whole graph clears the focus.
function focusOn() {
  if (!selected.value) return
  focusId.value = selected.value.id
  limit.value = 500
  load()
}
function clearFocus() {
  focusId.value = null
  limit.value = 500
  load()
}
function showAll() {
  focusId.value = null
  limit.value = Math.max(total.value + 1, 500)
  load()
}
</script>

<template>
  <div class="kg">
    <div class="kg-toolbar">
      <input v-model="search" class="kg-search" type="search" placeholder="Filter nodes by title, slug or type…" />
      <span class="kg-count">{{ nodeCount }} nodes · {{ edgeCount }} edges</span>
      <span v-if="truncated" class="kg-trunc">
        {{ loadedCount }} of {{ total }} shown
        <button class="kg-link" @click="showAll">Show all</button>
        <button v-if="focusId !== null" class="kg-link" @click="clearFocus">Whole graph</button>
      </span>
      <span class="kg-legend">
        <span v-for="t in legendTypes" :key="t" class="kg-legend-item">
          <span class="kg-dot" :style="{ background: colorFor(t) }"></span>{{ typeLabel(t) }}
        </span>
      </span>
    </div>

    <div class="kg-body">
      <div ref="container" class="kg-canvas"></div>

      <aside v-if="selected" class="kg-panel">
        <div class="kg-panel-head">
          <span class="kg-dot" :style="{ background: colorFor(selected.type) }"></span>
          <span class="kg-panel-type">{{ typeLabel(selected.type) }}</span>
          <button class="kg-panel-close" aria-label="Close" @click="selected = null">×</button>
        </div>
        <h4 class="kg-panel-title">{{ selected.title }}</h4>
        <code v-if="selected.slug" class="kg-panel-slug">{{ selected.slug }}</code>
        <p v-if="selected.reference_count" class="kg-panel-meta">
          referenced {{ selected.reference_count }}×
        </p>
        <div class="kg-panel-body">
          <p v-if="bodyLoading" class="kg-panel-muted">Loading…</p>
          <p v-else-if="bodyError" class="kg-panel-muted kg-error">{{ bodyError }}</p>
          <!-- eslint-disable-next-line vue/no-v-html -->
          <div v-else-if="bodyHtml" class="kg-md" v-html="bodyHtml"></div>
          <p v-else class="kg-panel-muted">No content.</p>
        </div>
        <div class="kg-panel-actions">
          <button class="kg-open" @click="focusOn">Focus</button>
          <button class="kg-open" @click="openSelected">Open entry →</button>
        </div>
      </aside>
    </div>

    <p v-if="loading" class="kg-status">Loading graph…</p>
    <p v-else-if="loadError" class="kg-status kg-error">{{ loadError }}</p>
    <p v-else-if="!raw.nodes.length" class="kg-status">
      No knowledge entries yet. Add memory, runbooks, or external systems to see the graph.
    </p>
  </div>
</template>

<style scoped>
.kg { display: flex; flex-direction: column; height: 100%; min-height: 480px; }
.kg-toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 8px 0 12px; }
.kg-search { flex: 0 1 320px; padding: 6px 10px; border: 1px solid var(--border, #d1d5db); border-radius: 6px; font-size: 13px; }
.kg-count { font-size: 12px; color: var(--text-muted, #6b7280); }
.kg-trunc { font-size: 12px; color: var(--text-muted, #6b7280); display: inline-flex; align-items: center; gap: 6px; }
.kg-link { border: none; background: none; color: var(--accent, #3b82f6); font-size: 12px; cursor: pointer; padding: 0; text-decoration: underline; }
.kg-panel-actions { display: flex; gap: 6px; }
.kg-legend { display: flex; gap: 12px; flex-wrap: wrap; margin-left: auto; }
.kg-legend-item { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; color: var(--text-muted, #6b7280); }
.kg-dot { width: 9px; height: 9px; border-radius: 50%; display: inline-block; }
.kg-body { position: relative; flex: 1; min-height: 420px; border: 1px solid var(--border, #e5e7eb); border-radius: 8px; overflow: hidden; }
.kg-canvas { position: absolute; inset: 0; }
.kg-panel { position: absolute; top: 12px; right: 12px; width: 340px; max-height: calc(100% - 24px); display: flex; flex-direction: column; background: var(--surface, #fff); border: 1px solid var(--border, #e5e7eb); border-radius: 8px; padding: 14px; box-shadow: 0 4px 16px rgba(0,0,0,0.08); }
.kg-panel-head { display: flex; align-items: center; gap: 6px; }
.kg-panel-type { font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted, #6b7280); }
.kg-panel-close { margin-left: auto; border: none; background: none; font-size: 17px; line-height: 1; cursor: pointer; color: var(--text-muted, #6b7280); padding: 0 2px; }
.kg-panel-title { margin: 8px 0 4px; font-size: 14px; }
.kg-panel-slug { font-size: 11px; color: var(--text-muted, #6b7280); }
.kg-panel-meta { font-size: 12px; color: var(--text-muted, #6b7280); margin: 6px 0; }
.kg-panel-body { margin: 8px 0; overflow-y: auto; flex: 1; min-height: 0; }
.kg-panel-muted { font-size: 12px; color: var(--text-muted, #6b7280); margin: 4px 0; }
.kg-md { font-size: 12.5px; line-height: 1.5; color: var(--text, #1f2937); word-break: break-word; }
.kg-md :deep(h1), .kg-md :deep(h2), .kg-md :deep(h3) { font-size: 13px; margin: 8px 0 4px; }
.kg-md :deep(p) { margin: 4px 0; }
.kg-md :deep(ul), .kg-md :deep(ol) { margin: 4px 0; padding-left: 18px; }
.kg-md :deep(code) { font-size: 11px; background: var(--surface-2, #f3f4f6); padding: 0 3px; border-radius: 3px; }
.kg-md :deep(pre) { background: var(--surface-2, #f3f4f6); padding: 6px; border-radius: 4px; overflow-x: auto; }
.kg-md :deep(a) { color: var(--accent, #3b82f6); }
.kg-open { margin-top: 8px; padding: 6px 10px; font-size: 12px; border: 1px solid var(--border, #d1d5db); border-radius: 6px; background: var(--surface, #fff); cursor: pointer; }
.kg-open:hover { background: var(--hover, #f3f4f6); }
.kg-status { padding: 12px 0; font-size: 13px; color: var(--text-muted, #6b7280); }
.kg-error { color: #dc2626; }
</style>
