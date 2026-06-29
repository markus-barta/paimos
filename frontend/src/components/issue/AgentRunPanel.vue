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

const TERMINAL = new Set(["deployed", "failed", "cancelled"]);
const hasActiveRun = computed(() => runs.value.some((r) => !TERMINAL.has(r.status)));

let pollTimer: ReturnType<typeof setInterval> | null = null;

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

async function fetchRuns() {
  try {
    const data = await api.get<{ runs: AgentRun[] }>(`/issues/${props.issueId}/runs`);
    runs.value = data.runs ?? [];
    error.value = "";
  } catch (e: unknown) {
    error.value = errMsg(e, "Could not load runs.");
  } finally {
    loading.value = false;
  }
  syncPolling();
}

async function fetchRunners() {
  try {
    const data = await api.get<{ runners: ProjectRunner[] }>(
      `/projects/${props.projectId}/runners`,
    );
    runners.value = data.runners ?? [];
    if (!selectedDevice.value && runners.value.length) {
      selectedDevice.value = runners.value[0].device_id;
    }
  } catch {
    runners.value = [];
  }
}

async function implement() {
  busy.value = true;
  error.value = "";
  try {
    await api.post(`/issues/${props.issueKey}/implement`, {
      device_id: selectedDevice.value,
    });
    await fetchRuns();
  } catch (e: unknown) {
    error.value = errMsg(e, "Could not start the run.");
  } finally {
    busy.value = false;
  }
}

// Poll only while a run is in flight; stop once everything is terminal so an
// idle ticket page isn't hitting the API on a timer.
function syncPolling() {
  if (hasActiveRun.value && !pollTimer) {
    pollTimer = setInterval(fetchRuns, 4000);
  } else if (!hasActiveRun.value && pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

onMounted(() => {
  void fetchRuns();
  void fetchRunners();
});

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer);
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

    <p v-if="!runners.length" class="arp-hint">
      No runner is online for this project. The run will queue until a
      <code>paimos run-agent watch</code> picks it up.
    </p>

    <p v-if="error" class="arp-error">{{ error }}</p>

    <p v-if="!loading && !runs.length" class="arp-empty">
      No runs yet. Click <strong>Implement this</strong> to hand
      {{ issueKey }} to your local agent.
    </p>

    <ul v-if="runs.length" class="arp-runs">
      <li v-for="run in runs" :key="run.id" class="arp-run">
        <span class="arp-pill" :class="`arp-pill--${run.status}`">
          {{ statusLabel(run.status) }}
        </span>
        <span class="arp-run-meta">
          <span v-if="run.version" class="arp-ver">v{{ run.version }}</span>
          <span v-if="run.device_id" class="arp-dev">{{ run.device_id }}</span>
          <span v-if="run.deploy_target" class="arp-target">→ {{ run.deploy_target }}</span>
          <time :datetime="run.created_at">{{ run.created_at }}</time>
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
