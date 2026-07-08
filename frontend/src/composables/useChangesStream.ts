import { onBeforeUnmount, onMounted, watch, type Ref } from "vue";

import { liveUpdatesEnabled, loadInstance } from "@/api/instance";
import { useChangesStore, type MutationChangeEvent } from "@/stores/changes";

const LAST_SEQ_KEY = "paimos.changes.lastSeq";

let source: EventSource | null = null;

function readLastSeq(): number {
  const raw = localStorage.getItem(LAST_SEQ_KEY);
  const n = raw ? Number(raw) : 0;
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
}

function writeLastSeq(id: number) {
  if (Number.isFinite(id) && id > 0) {
    localStorage.setItem(LAST_SEQ_KEY, String(Math.floor(id)));
  }
}

export function useChangesStream(enabled?: Ref<boolean>) {
  const changes = useChangesStore();
  let stopWatch: (() => void) | null = null;

  function connect() {
    if (source) return;
    const since = readLastSeq();
    source = new EventSource(`/api/changes?since=${encodeURIComponent(String(since))}`);
    let opened = false;
    source.onopen = () => {
      opened = true;
    };
    source.addEventListener("mutation", (event) => {
      const msg = event as MessageEvent<string>;
      let payload: MutationChangeEvent;
      try {
        payload = JSON.parse(msg.data) as MutationChangeEvent;
      } catch {
        return;
      }
      const seq = Number(msg.lastEventId || payload.id);
      if (Number.isFinite(seq) && seq > 0) writeLastSeq(seq);
      changes.publish(payload);
    });
    source.onerror = () => {
      // If the stream never opened, the feature flag is likely off or the
      // connection cap rejected us. After a successful open, keep the native
      // EventSource reconnect path; the backend honors Last-Event-ID.
      if (!opened) {
        source?.close();
        source = null;
      }
    };
  }

  function disconnect() {
    source?.close();
    source = null;
  }

  async function sync() {
    if (enabled && !enabled.value) {
      disconnect();
      return;
    }
    await loadInstance();
    if (!liveUpdatesEnabled.value) {
      disconnect();
      return;
    }
    connect();
  }

  onMounted(() => {
    stopWatch = watch(
      [() => enabled?.value ?? true, liveUpdatesEnabled],
      () => { void sync(); },
      { immediate: true },
    );
  });
  onBeforeUnmount(() => {
    stopWatch?.();
    disconnect();
  });
}
