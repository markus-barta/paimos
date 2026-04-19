<script setup lang="ts">
defineProps<{
  aggregation: {
    member_count: number
    estimate_hours: number | null; estimate_lp: number | null; estimate_eur: number | null
    ar_hours: number | null; ar_lp: number | null; ar_eur: number | null
    actual_hours: number | null; actual_internal_cost: number | null; margin_eur: number | null
  }
  timeLabel: () => string
  formatHours: (h: number) => string
  toggleTimeUnit: () => void
}>()
</script>

<template>
  <div class="billing-summary">
    <h3 class="billing-summary-title">Billing Summary</h3>
    <div class="billing-grid">
      <div class="billing-col">
        <span class="billing-col-label">Estimate</span>
        <div class="billing-row" v-if="aggregation.estimate_hours != null">
          <span class="billing-label meta-label--toggle" @click="toggleTimeUnit" title="Toggle h / PT">Hours <span class="unit-toggle">{{ timeLabel() }}</span></span>
          <span class="billing-value">{{ formatHours(aggregation.estimate_hours) }}</span>
        </div>
        <div class="billing-row" v-if="aggregation.estimate_lp != null">
          <span class="billing-label">LP</span>
          <span class="billing-value">{{ aggregation.estimate_lp }}</span>
        </div>
        <div class="billing-row" v-if="aggregation.estimate_eur != null">
          <span class="billing-label">EUR</span>
          <span class="billing-value billing-value--eur">{{ aggregation.estimate_eur.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}</span>
        </div>
      </div>
      <div class="billing-col">
        <span class="billing-col-label">AR</span>
        <div class="billing-row" v-if="aggregation.ar_hours != null">
          <span class="billing-label meta-label--toggle" @click="toggleTimeUnit" title="Toggle h / PT">Hours <span class="unit-toggle">{{ timeLabel() }}</span></span>
          <span class="billing-value">{{ formatHours(aggregation.ar_hours) }}</span>
        </div>
        <div class="billing-row" v-if="aggregation.ar_lp != null">
          <span class="billing-label">LP</span>
          <span class="billing-value">{{ aggregation.ar_lp }}</span>
        </div>
        <div class="billing-row" v-if="aggregation.ar_eur != null">
          <span class="billing-label">EUR</span>
          <span class="billing-value billing-value--eur">{{ aggregation.ar_eur.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}</span>
        </div>
      </div>
      <div class="billing-col">
        <span class="billing-col-label">Actuals</span>
        <div class="billing-row" v-if="aggregation.actual_hours != null">
          <span class="billing-label meta-label--toggle" @click="toggleTimeUnit" title="Toggle h / PT">Hours <span class="unit-toggle">{{ timeLabel() }}</span></span>
          <span class="billing-value">{{ formatHours(aggregation.actual_hours) }}</span>
        </div>
        <div class="billing-row" v-if="aggregation.actual_internal_cost != null">
          <span class="billing-label">Internal</span>
          <span class="billing-value">{{ aggregation.actual_internal_cost.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}</span>
        </div>
        <div class="billing-row" v-if="aggregation.margin_eur != null">
          <span class="billing-label">Margin</span>
          <span :class="['billing-value', 'billing-value--margin', aggregation.margin_eur >= 0 ? 'margin-pos' : 'margin-neg']">{{ aggregation.margin_eur.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}</span>
        </div>
      </div>
    </div>
    <p class="billing-members">{{ aggregation.member_count }} member issue{{ aggregation.member_count !== 1 ? 's' : '' }}</p>
  </div>
</template>

<style scoped>
.billing-summary {
  margin: 1rem 0; padding: 1rem 1.25rem;
  background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius);
}
.billing-summary-title {
  font-size: 13px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: .04em; margin: 0 0 .75rem;
}
.billing-grid {
  display: grid; grid-template-columns: repeat(3, 1fr); gap: 1.5rem;
}
.billing-col-label {
  display: block; font-size: 12px; font-weight: 600; color: var(--text);
  margin-bottom: .5rem; border-bottom: 1px solid var(--border); padding-bottom: .25rem;
}
.billing-row {
  display: flex; justify-content: space-between; align-items: baseline;
  font-size: 13px; padding: .15rem 0;
}
.billing-label { color: var(--text-muted); }
.billing-value { font-weight: 500; font-variant-numeric: tabular-nums; }
.billing-value--eur { color: var(--bp-blue-dark); font-weight: 600; }
.billing-value--margin { font-weight: 600; }
.margin-pos { color: #166534; }
.margin-neg { color: #991b1b; }
.billing-members {
  margin: .5rem 0 0; font-size: 11px; color: var(--text-muted); font-style: italic;
}
.meta-label--toggle { cursor: pointer; }
.meta-label--toggle:hover .unit-toggle { color: var(--bp-blue); }
.unit-toggle { color: var(--bp-blue); text-decoration: underline; text-decoration-style: dotted; }
</style>
