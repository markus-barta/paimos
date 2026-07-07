import { onBeforeUnmount, onMounted, ref, watch, type Ref } from "vue";
import { getActivePinia } from "pinia";

import { api } from "@/api/client";
import { useChangesStore, type MutationChangeEvent } from "@/stores/changes";

interface FreshnessOptions<T> {
  intervalMs?: number;
  enabled?: Ref<boolean>;
  apply?: (payload: T) => void;
  count?: (payload: T) => number | null;
  changes?: (event: MutationChangeEvent) => boolean;
}

export function useFreshness<T>(
  path: Ref<string>,
  opts: FreshnessOptions<T> = {},
) {
  const stale = ref(false);
  const newCount = ref<number | null>(null);
  const etag = ref<string | null>(null);

  let currentData: T | null = null;
  let pendingData: T | null = null;
  let pendingEtag: string | null = null;
  let timer: number | null = null;
  let skipNextPathPrimeFor: string | null = null;
  let unsubscribeChanges: (() => void) | null = null;

  function setCurrent(data: T) {
    currentData = data;
    pendingData = null;
    pendingEtag = null;
    stale.value = false;
    newCount.value = null;
  }

  async function prime(data?: T) {
    if (data !== undefined) {
      setCurrent(data);
      skipNextPathPrimeFor = path.value;
      return;
    }
    const response = await api.getWithMeta<T>(path.value);
    if (response.status === 200) {
      etag.value = response.etag;
      setCurrent(response.data);
    }
  }

  async function fetchAndApply() {
    const response = await api.getWithMeta<T>(path.value);
    if (response.status !== 200) return;
    opts.apply?.(response.data);
    etag.value = response.etag;
    setCurrent(response.data);
  }

  async function tick() {
    if (document.visibilityState !== "visible") return;
    if (opts.enabled && !opts.enabled.value) return;
    const headers = etag.value ? { "If-None-Match": etag.value } : undefined;
    const response = await api.getWithMeta<T>(path.value, { headers });
    if (response.status === 304) return;
    if (response.status !== 200) return;

    pendingData = response.data;
    pendingEtag = response.etag;
    stale.value = true;
    if (opts.count && pendingData) {
      const nextCount = opts.count(pendingData);
      const currentCount = currentData && opts.count(currentData);
      newCount.value =
        nextCount != null && currentCount != null
          ? Math.max(0, nextCount - currentCount)
          : nextCount;
    } else {
      newCount.value = null;
    }
  }

  async function refresh() {
    if (!pendingData) {
      if (stale.value) await fetchAndApply();
      return;
    }
    opts.apply?.(pendingData);
    currentData = pendingData;
    pendingData = null;
    etag.value = pendingEtag;
    pendingEtag = null;
    stale.value = false;
    newCount.value = null;
  }

  function startPolling() {
    stopPolling();
    timer = window.setInterval(() => {
      void tick();
    }, opts.intervalMs ?? 300_000);
  }

  function stopPolling() {
    if (timer !== null) {
      window.clearInterval(timer);
      timer = null;
    }
  }

  onMounted(startPolling);
  onMounted(() => {
    if (!opts.changes) return;
    if (!getActivePinia()) return;
    const changes = useChangesStore();
    unsubscribeChanges = changes.subscribe(
      opts.changes,
      () => {
        if (opts.enabled && !opts.enabled.value) return;
        pendingData = null;
        pendingEtag = null;
        stale.value = true;
        newCount.value = null;
      },
    );
  });
  onBeforeUnmount(() => {
    stopPolling();
    unsubscribeChanges?.();
    unsubscribeChanges = null;
  });

  watch(path, (nextPath) => {
    if (skipNextPathPrimeFor !== null) {
      const shouldSkip = skipNextPathPrimeFor === nextPath;
      skipNextPathPrimeFor = null;
      if (shouldSkip) return;
    }
    void prime();
  });

  return {
    stale,
    newCount,
    etag,
    prime,
    refresh,
    tick,
  };
}
