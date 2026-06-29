<script setup lang="ts">
/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-610 (epic PAI-605): the "Implement this" button + live run-status card.
// Clicking the button creates a queued agent run (PAI-606); the developer's
// local runner (PAI-608) picks it up over SSE and reports progress back, which
// this panel surfaces by polling while a run is in flight.
import { ref, computed, onMounted, onUnmounted } from "vue";
import AppIcon from "@/components/AppIcon.vue";
import { api, errMsg } from "@/api/client";

const props = defineProps<{
  issueId: number;
  issueKey: string;
  projectId: number;
}>();

interface AgentRun {
  id: number;
  status: string;
  version: string;
  device_id: string;
  deploy_target: string;
  tests_summary: string | null;
  error: string;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
}

interface ProjectRunner {
  user_id: number;
  device_id: string;
  last_seen: string;
}

const runs = ref<AgentRun[]>([]);
const runners = ref<ProjectRunner[]>([]);
const selectedDevice = ref("");
const loading = ref(true);
const busy = ref(false);
const error = ref("");
const runnersError = ref(""); // distinct from "no runners online" (M4)

const TERMINAL = new Set(["deployed", "failed", "cancelled"]);
const hasActiveRun = computed(() => runs.value.some((r) => !TERMINAL.has(r.status)));

let pollTimer: ReturnType<typeof setInterval> | null = null;
// Monotonic tokens so an out-of-order response can't overwrite newer state (M2).
let runsSeq = 0;
let runnersSeq = 0;
// Guards against a fetch that resolves AFTER unmount re-arming the poll timer (H1).
let alive = true;

const STATUS_LABEL: Record<string, string> = {
  queued: "Queued",
  running: "Running",
  tests_passed: "Tests passed",
  tests_failed: "Tests failed",
  deployed: "Deployed",
  failed: "Failed",
  cancelled: "Cancelled",
};

function statusLabel(s: string): string {
  return STATUS_LABEL[s] ?? s;
}

// The API emits SQLite's "YYYY-MM-DD HH:MM:SS" (UTC). Parse it to a real Date
// for a localized display string and a valid ISO `datetime` attribute (M6).
function toDate(ts: string | null): Date | null {
  if (!ts) return null;
  let s = ts.trim().replace(" ", "T");
  // Treat a zone-less timestamp as UTC (the API emits UTC). A present zone is a
  // trailing Z or ±HH:MM — only append Z when neither is there (L1).
  if (!/[Zz]$|[+-]\d{2}:?\d{2}$/.test(s)) s += "Z";
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : d;
}
function isoAttr(ts: string): string {
  return toDate(ts)?.toISOString() ?? ts;
}
function localTime(ts: string): string {
  return toDate(ts)?.toLocaleString() ?? ts;
}

async function fetchRuns() {
  const seq = ++runsSeq;
  try {
    const data = await api.get<{ runs: AgentRun[] }>(`/issues/${props.issueId}/runs`);
    if (!alive || seq !== runsSeq) return; // unmounted, or a newer fetch landed
    runs.value = data.runs ?? [];
    error.value = "";
  } catch (e: unknown) {
    if (!alive || seq !== runsSeq) return;
    error.value = errMsg(e, "Could not load runs.");
  } finally {
    if (alive) loading.value = false;
  }
  syncPolling();
}

async function fetchRunners() {
  const seq = ++runnersSeq;
  try {
    const data = await api.get<{ runners: ProjectRunner[] }>(
      `/projects/${props.projectId}/runners`,
    );
    if (!alive || seq !== runnersSeq) return;
    runners.value = data.runners ?? [];
    runnersError.value = "";
    if (!selectedDevice.value && runners.value.length) {
      selectedDevice.value = runners.value[0].device_id;
    }
  } catch (e: unknown) {
    if (!alive || seq !== runnersSeq) return;
    runners.value = [];
    runnersError.value = errMsg(e, "Could not load runners."); // M4: don't masquerade as "none online"
  }
}

async function implement() {
  busy.value = true;
  error.value = "";
  try {
    await api.post(`/issues/${props.issueKey}/implement`, {
      device_id: selectedDevice.value,
    });
    await Promise.all([fetchRuns(), fetchRunners()]); // M5: refresh the picker too
  } catch (e: unknown) {
    error.value = errMsg(e, "Could not start the run.");
  } finally {
    busy.value = false;
  }
}

// Poll only while a run is in flight AND the tab is visible — a backgrounded
// tab with a stuck queued run must not heartbeat forever (M1). Each tick also
// refreshes runners so one that connects after load appears in the picker (M5).
function pollTick() {
  void fetchRuns();
  void fetchRunners();
}
function syncPolling() {
  if (!alive) {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
    return;
  }
  const shouldPoll = hasActiveRun.value && !document.hidden;
  if (shouldPoll && !pollTimer) {
    pollTimer = setInterval(pollTick, 4000);
  } else if (!shouldPoll && pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

function onVisibility() {
  if (!document.hidden && hasActiveRun.value) void fetchRuns();
  syncPolling();
}

onMounted(() => {
  void fetchRuns();
  void fetchRunners();
  document.addEventListener("visibilitychange", onVisibility);
});

onUnmounted(() => {
  alive = false;
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
  document.removeEventListener("visibilitychange", onVisibility);
});
</script>

<template>
  <section class="agent-run-panel">
    <div class="arp-head">
      <h3 class="arp-title">
        <AppIcon name="zap" :size="14" />
        Implement
      </h3>
      <div class="arp-actions">
        <select
          v-if="runners.length > 1"
          v-model="selectedDevice"
          class="arp-device"
          aria-label="Target runner"
        >
          <option v-for="r in runners" :key="r.device_id" :value="r.device_id">
            {{ r.device_id }}
          </option>
        </select>
        <button
          class="btn btn-primary btn-sm"
          type="button"
          :disabled="busy"
          @click="implement"
        >
          {{ busy ? "Starting…" : "Implement this" }}
        </button>
      </div>
    </div>

    <p v-if="runnersError" class="arp-error" role="alert">
      Couldn't check for runners: {{ runnersError }}
    </p>
    <p v-else-if="!runners.length" class="arp-hint">
      No runner is online for this project. The run will queue until a
      <code>paimos run-agent watch</code> picks it up.
    </p>

    <p v-if="error" class="arp-error" role="alert">{{ error }}</p>

    <p v-if="!loading && !runs.length" class="arp-empty">
      No runs yet. Click <strong>Implement this</strong> to hand
      {{ issueKey }} to your local agent.
    </p>

    <ul v-if="runs.length" class="arp-runs" aria-live="polite" aria-label="Agent runs">
      <li v-for="run in runs" :key="run.id" class="arp-run">
        <span class="arp-pill" :class="`arp-pill--${run.status}`">
          {{ statusLabel(run.status) }}
        </span>
        <span class="arp-run-meta">
          <span v-if="run.version" class="arp-ver">v{{ run.version }}</span>
          <span v-if="run.device_id" class="arp-dev">{{ run.device_id }}</span>
          <span v-if="run.deploy_target" class="arp-target">→ {{ run.deploy_target }}</span>
          <time :datetime="isoAttr(run.created_at)">{{ localTime(run.created_at) }}</time>
        </span>
        <span v-if="run.error" class="arp-run-err">{{ run.error }}</span>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.agent-run-panel {
  margin-top: 1.25rem;
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-card);
}
.arp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.arp-title {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
}
.arp-actions {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
}
.arp-device {
  font: inherit;
  font-size: 12px;
  padding: 0.25rem 0.4rem;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg);
  color: var(--text);
}
.arp-hint,
.arp-empty {
  margin: 0.6rem 0 0;
  font-size: 12px;
  color: var(--text-muted);
}
.arp-error {
  margin: 0.6rem 0 0;
  font-size: 12px;
  color: #c0392b;
}
.arp-hint code {
  font-size: 11px;
  background: color-mix(in srgb, var(--text-muted) 12%, transparent);
  padding: 0.05rem 0.3rem;
  border-radius: 4px;
}
.arp-runs {
  list-style: none;
  margin: 0.75rem 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.arp-run {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  flex-wrap: wrap;
  font-size: 12px;
}
.arp-pill {
  display: inline-block;
  padding: 0.1rem 0.5rem;
  border-radius: 999px;
  font-weight: 600;
  font-size: 11px;
  white-space: nowrap;
  background: color-mix(in srgb, var(--text-muted) 18%, transparent);
  color: var(--text);
}
.arp-pill--running {
  background: color-mix(in srgb, var(--bp-blue) 20%, transparent);
  color: var(--bp-blue);
}
.arp-pill--tests_passed {
  background: color-mix(in srgb, #1aa179 24%, transparent);
  color: #0f7355;
}
.arp-pill--deployed {
  background: color-mix(in srgb, #2ecc71 24%, transparent);
  color: #1e8449;
}
.arp-pill--tests_failed,
.arp-pill--failed {
  background: color-mix(in srgb, #e74c3c 22%, transparent);
  color: #c0392b;
}
.arp-run-meta {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--text-muted);
}
.arp-ver {
  font-weight: 600;
  color: var(--text);
}
.arp-run-err {
  color: #c0392b;
  flex-basis: 100%;
}
</style>
