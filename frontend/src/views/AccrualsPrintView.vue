<script setup lang="ts">
// ACME-1 — Project Accruals editorial print view.
// Standalone, decoupled from app shell. Designed for ⌘P → Save as PDF.
// Aesthetic: warm broadsheet, ledger margins, hairline rules, tabular figures.
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api, errMsg } from '@/api/client'
import {
  ACCRUALS_DEFAULT_STATUSES as ACCRUALS_DEFAULTS,
  ACCRUALS_EXTRA_STATUSES   as ACCRUALS_EXTRAS,
} from '@/constants/status'

const route = useRoute()

interface AccrualsApiRow { project_id: number; project_key: string; project_name: string; totals: Record<string, number> }
interface AccrualsApiResp { from: string; to: string; statuses: string[]; rows: AccrualsApiRow[] }

const from = String(route.query.from ?? '')
const to   = String(route.query.to ?? '')
const extras = String(route.query.extras ?? '')
  .split(',')
  .map(s => s.trim())
  .filter(s => (ACCRUALS_EXTRAS as readonly string[]).includes(s))

const columns = computed<string[]>(() => [...ACCRUALS_DEFAULTS, ...ACCRUALS_EXTRAS.filter(s => extras.includes(s))])

const rows = ref<AccrualsApiRow[]>([])
const loading = ref(true)
const error = ref('')

const totals = computed(() => {
  const t: Record<string, number> = {}
  let grand = 0
  for (const c of columns.value) t[c] = 0
  for (const r of rows.value) {
    for (const c of columns.value) {
      const v = r.totals[c] ?? 0
      t[c] += v
      if (c !== 'cancelled') grand += v
    }
  }
  return { byStatus: t, grand }
})

function rowTotal(r: AccrualsApiRow): number {
  let s = 0
  for (const c of columns.value) {
    if (c === 'cancelled') continue
    s += r.totals[c] ?? 0
  }
  return s
}

function fmt(n: number): string {
  if (!n) return '—'
  return n.toLocaleString('de-DE', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
}

const STATUS_DE: Record<string, string> = {
  'new':         'Neu',
  'backlog':     'Backlog',
  'in-progress': 'In Arbeit',
  'qa':          'QA',
  'done':        'Erledigt',
  'delivered':   'Geliefert',
  'accepted':    'Akzeptiert',
  'invoiced':    'Verrechnet',
  'cancelled':   'Storniert',
}
function statusLabel(s: string): string { return STATUS_DE[s] ?? s }

function periodLabel(): string {
  if (!from || !to) return ''
  const opts: Intl.DateTimeFormatOptions = { day: 'numeric', month: 'long', year: 'numeric' }
  const f = new Date(from).toLocaleDateString('de-DE', opts)
  const t = new Date(to).toLocaleDateString('de-DE', opts)
  return `${f} — ${t}`
}

const generatedAt = new Date().toLocaleString('de-DE', {
  day: 'numeric', month: 'long', year: 'numeric',
  hour: '2-digit', minute: '2-digit',
})

onMounted(async () => {
  try {
    const r = await api.get<AccrualsApiResp>(`/reports/accruals?from=${from}&to=${to}`)
    rows.value = r.rows
  } catch (e: unknown) {
    error.value = errMsg(e, 'Bericht konnte nicht geladen werden.')
  } finally {
    loading.value = false
  }
})

function doPrint() {
  window.print()
}
function closeWindow() {
  window.close()
}
</script>

<template>
  <div class="broadsheet">
    <!-- Top action bar — hidden when printing -->
    <div class="actions no-print">
      <button class="action-btn" @click="doPrint">⌘P · Drucken oder als PDF speichern</button>
      <button class="action-btn action-btn--ghost" @click="closeWindow">Schließen</button>
    </div>

    <article class="paper">
      <!-- Masthead -->
      <header class="masthead">
        <div class="masthead-left">
          <div class="masthead-eyebrow">PAIMOS · Projektmanagement</div>
          <h1 class="masthead-title">Vorratsbuch</h1>
          <div class="masthead-subtitle">Stundensummen je Status und Projekt</div>
        </div>
        <div class="masthead-right">
          <div class="masthead-meta">
            <span class="meta-label">Zeitraum</span>
            <span class="meta-value">{{ periodLabel() }}</span>
          </div>
          <div class="masthead-meta">
            <span class="meta-label">Erstellt am</span>
            <span class="meta-value">{{ generatedAt }}</span>
          </div>
          <div class="masthead-meta">
            <span class="meta-label">Projekte</span>
            <span class="meta-value">{{ String(rows.length).padStart(3,'0') }}</span>
          </div>
        </div>
      </header>

      <div class="rule rule--double"></div>

      <!-- Body -->
      <section v-if="loading" class="state">Wird geladen …</section>
      <section v-else-if="error" class="state state--err">{{ error }}</section>

      <section v-else>
        <table class="ledger-table">
          <thead>
            <tr>
              <th class="col-key">Kürzel</th>
              <th class="col-name">Projekt</th>
              <th v-for="c in columns" :key="c" class="col-num">{{ statusLabel(c) }}</th>
              <th class="col-num col-total">Summe</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in rows" :key="r.project_id">
              <td class="col-key">{{ r.project_key }}</td>
              <td class="col-name">{{ r.project_name }}</td>
              <td v-for="c in columns" :key="c" class="col-num" :class="`num--${c}`">
                {{ fmt(r.totals[c] ?? 0) }}
              </td>
              <td class="col-num col-total">{{ fmt(rowTotal(r)) }}</td>
            </tr>
          </tbody>
          <tfoot>
            <tr>
              <td colspan="2" class="totals-label">GESAMT</td>
              <td v-for="c in columns" :key="c" class="col-num">{{ fmt(totals.byStatus[c]) }}</td>
              <td class="col-num col-total">{{ fmt(totals.grand) }}</td>
            </tr>
          </tfoot>
        </table>

        <p class="footnote">
          * Stunden werden dem Datum zugeordnet, an dem das Issue zuletzt seinen aktuellen Status erreicht hat
          (gespeichert in <code>issue_history</code>). Stornierte Stunden erscheinen in der Spalte, sind jedoch
          weder in der Zeilen- noch in der Gesamtsumme enthalten.
        </p>
      </section>

      <footer class="colophon">
        <span>PAIMOS · {{ new Date().getFullYear() }}</span>
        <span class="colophon-dot">·</span>
        <span>Vertraulich — interne Vorrätsberechnung</span>
      </footer>
    </article>
  </div>
</template>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,400;12..96,500;12..96,600;12..96,700&family=JetBrains+Mono:wght@400;500;600&display=swap');

/* Local cool palette + accent variable from the theming engine */
.broadsheet {
  --acc:        var(--accruals-accent, #006497);
  --acc-soft:   var(--accruals-accent-soft, rgba(0,100,151,.08));
  --acc-dark:   var(--accruals-accent-dark, #00466b);
  --paper:      #fafbfc;
  --paper-edge: #e9ebf0;
  --ink:        #0f1419;
  --ink-2:      #2a3441;
  --mute:       #6b7480;
  --mute-2:     #8a909a;
  --line:       #dde1e7;
  --line-2:     #c9cfd7;

  min-height: 100vh;
  background: var(--paper-edge);
  padding: 2rem 1.5rem 3.5rem;
  font-family: 'Bricolage Grotesque', system-ui, sans-serif;
  font-feature-settings: 'ss01' on;
  color: var(--ink);
}

.actions {
  max-width: 880px; margin: 0 auto 1rem;
  display: flex; gap: .45rem; justify-content: flex-end;
}
.action-btn {
  font-family: 'Bricolage Grotesque', sans-serif;
  font-size: 11px; font-weight: 600; letter-spacing: -.003em;
  background: var(--acc); color: #fff;
  padding: .45rem .85rem; border: 1px solid var(--acc); border-radius: 3px;
  cursor: pointer; transition: background .15s;
}
.action-btn:hover { background: var(--acc-dark); border-color: var(--acc-dark); }
.action-btn--ghost { background: transparent; color: var(--acc); }
.action-btn--ghost:hover { background: var(--acc-soft); color: var(--acc-dark); }

.paper {
  max-width: 880px; margin: 0 auto;
  background: var(--paper);
  padding: 2.5rem 2.75rem 2rem;
  box-shadow:
    0 1px 0 rgba(15,20,25,.03),
    0 2px 6px rgba(15,20,25,.04),
    0 24px 60px rgba(15,20,25,.10);
  position: relative;
  border-top: 3px solid var(--acc);
}
.paper > * { position: relative; }

/* Masthead — restrained, single-line title */
.masthead {
  display: flex; align-items: flex-end; justify-content: space-between;
  gap: 2rem; margin-bottom: 1.1rem;
}
.masthead-eyebrow {
  font-size: 9px; font-weight: 700; letter-spacing: .16em;
  text-transform: uppercase; color: var(--acc);
  margin-bottom: .3rem;
}
.masthead-title {
  font-family: 'Bricolage Grotesque', sans-serif;
  font-size: 26px; font-weight: 700; line-height: 1;
  color: var(--ink); margin: 0 0 .25rem;
  letter-spacing: -.018em;
}
.masthead-subtitle {
  font-size: 11.5px; color: var(--mute); font-weight: 400;
  letter-spacing: -.003em;
}

.masthead-right { text-align: right; min-width: 200px; }
.masthead-meta {
  display: flex; align-items: baseline; justify-content: flex-end; gap: .55rem;
  padding: .25rem 0;
}
.masthead-meta + .masthead-meta { border-top: 1px dotted var(--line); }
.meta-label {
  font-size: 8.5px; font-weight: 700; letter-spacing: .14em;
  text-transform: uppercase; color: var(--mute);
}
.meta-value {
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 10px; font-weight: 500; color: var(--ink);
  font-variant-numeric: tabular-nums;
}

.rule { height: 1px; background: var(--ink); margin: .85rem 0; }
.rule--double {
  height: 3px;
  border-top: 1px solid var(--ink);
  border-bottom: 1px solid var(--ink);
  background: transparent;
  margin: .85rem 0 1.35rem;
}

.state { padding: 3rem 0; text-align: center; color: var(--mute); font-size: 12px; }
.state--err { color: #a04030; }

/* Ledger table */
.ledger-table {
  width: 100%;
  border-collapse: collapse;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}
.ledger-table thead th {
  font-family: 'Bricolage Grotesque', sans-serif;
  font-size: 8.5px; font-weight: 700; letter-spacing: .12em;
  text-transform: uppercase; color: var(--mute);
  text-align: right; padding: .5rem .55rem;
  border-bottom: 1px solid var(--ink);
}
.ledger-table thead .col-key,
.ledger-table thead .col-name { text-align: left; }
.ledger-table tbody td {
  padding: .45rem .55rem;
  border-bottom: 1px dotted var(--line);
  color: var(--ink);
}
.ledger-table tbody tr:hover td { background: var(--acc-soft); }
.col-key {
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 9.5px; font-weight: 600; letter-spacing: .02em;
  color: var(--mute); width: 1%; white-space: nowrap;
}
.col-name {
  font-family: 'Bricolage Grotesque', sans-serif;
  font-size: 11.5px; font-weight: 500; color: var(--ink);
  text-align: left; letter-spacing: -.005em;
}
.col-num {
  text-align: right; font-feature-settings: 'tnum' on;
  width: 1%; white-space: nowrap;
}
.col-total {
  font-weight: 700; padding-left: 1rem;
  border-left: 1px solid var(--line);
  color: var(--acc);
}
.num--accepted  { color: var(--acc); opacity: .75; }
.num--invoiced  { color: var(--acc); font-weight: 600; }
.num--cancelled { color: var(--mute-2); opacity: .65; font-style: italic; }

.ledger-table tfoot td {
  border-top: 1px solid var(--ink);
  border-bottom: 3px double var(--ink);
  padding: .65rem .55rem; font-weight: 700; color: var(--ink);
  background: var(--acc-soft);
}
.totals-label {
  font-family: 'Bricolage Grotesque', sans-serif;
  font-size: 9px; letter-spacing: .14em;
  text-align: right; color: var(--ink);
}

.footnote {
  font-size: 10px; color: var(--mute);
  margin: 1.25rem 0 0; line-height: 1.55;
  max-width: 65ch; font-weight: 400;
}
.footnote code {
  font-family: 'JetBrains Mono', monospace;
  font-size: 9.5px;
  background: var(--acc-soft); padding: .05rem .3rem; border-radius: 2px;
  color: var(--acc-dark);
}

.colophon {
  margin-top: 2rem; padding-top: .85rem;
  border-top: 1px solid var(--line);
  font-size: 8.5px; letter-spacing: .12em; text-transform: uppercase;
  color: var(--mute-2);
  display: flex; justify-content: center; gap: .5rem;
}
.colophon-dot { color: var(--line-2); }

/* Print */
@media print {
  .broadsheet { background: #fff; padding: 0; min-height: auto; }
  .no-print { display: none !important; }
  .paper {
    box-shadow: none; max-width: none; padding: 1.4cm 1.4cm 1cm;
    background: #fff;
    border-top: 2.5pt solid var(--acc);
  }
  .ledger-table tbody tr:hover td { background: transparent; }
  .ledger-table tbody td { color: #000; }
  .col-name { color: #000; }
  @page { margin: 1cm 1cm 1.4cm; size: A4 landscape; }
}
</style>
