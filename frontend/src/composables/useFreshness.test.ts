import { ref, defineComponent } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { mountComponent } from "@/components/ai/testMount";
import { useChangesStore } from "@/stores/changes";
import { useFreshness } from "./useFreshness";

const { getWithMeta } = vi.hoisted(() => ({
  getWithMeta: vi.fn(),
}));

vi.mock("@/api/client", () => ({
  api: {
    getWithMeta,
  },
}));

describe("useFreshness", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setActivePinia(undefined);
    getWithMeta.mockReset();
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      value: "visible",
    });
  });

  it("marks data stale on a changed 200 response and applies it on refresh", async () => {
    const path = ref("/issues");
    const applied: number[] = [];
    let freshness!: ReturnType<typeof useFreshness<{ total: number }>>;

    const TestHarness = defineComponent({
      setup() {
        freshness = useFreshness(path, {
          intervalMs: 1000,
          apply: (payload) => applied.push(payload.total),
          count: (payload) => payload.total,
        });
        void freshness.prime({ total: 2 });
        return () => null;
      },
    });

    getWithMeta.mockResolvedValueOnce({
      data: { total: 5 },
      etag: 'W/"next"',
      lastModified: null,
      status: 200,
    });

    const mounted = await mountComponent(TestHarness);
    vi.advanceTimersByTime(1000);
    await Promise.resolve();

    expect(freshness.stale.value).toBe(true);
    expect(freshness.newCount.value).toBe(3);

    await freshness.refresh();
    expect(applied).toEqual([5]);
    expect(freshness.stale.value).toBe(false);

    await mounted.unmount();
  });

  it("skips polling while the document is hidden", async () => {
    const path = ref("/issues");
    let freshness!: ReturnType<typeof useFreshness<{ total: number }>>;

    const TestHarness = defineComponent({
      setup() {
        freshness = useFreshness(path, { intervalMs: 1000 });
        return () => null;
      },
    });

    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      value: "hidden",
    });

    const mounted = await mountComponent(TestHarness);
    vi.advanceTimersByTime(1000);
    await Promise.resolve();

    expect(getWithMeta).not.toHaveBeenCalled();
    expect(freshness.stale.value).toBe(false);

    await mounted.unmount();
  });

  it("only skips the path prime caused by externally primed data", async () => {
    const path = ref("/issues?limit=100");
    let freshness!: ReturnType<typeof useFreshness<{ total: number }>>;

    const TestHarness = defineComponent({
      setup() {
        freshness = useFreshness(path, { intervalMs: 1000 });
        void freshness.prime({ total: 2 });
        return () => null;
      },
    });

    getWithMeta.mockResolvedValueOnce({
      data: { total: 3 },
      etag: 'W/"next"',
      lastModified: null,
      status: 200,
    });

    const mounted = await mountComponent(TestHarness);
    path.value = "/issues?limit=100&q=search";
    await Promise.resolve();

    expect(getWithMeta).toHaveBeenCalledWith("/issues?limit=100&q=search");

    await mounted.unmount();
  });

  it("does not skip when returning to a path after an unrelated path change", async () => {
    const path = ref("/issues?limit=100");
    let freshness!: ReturnType<typeof useFreshness<{ total: number }>>;

    const TestHarness = defineComponent({
      setup() {
        freshness = useFreshness(path, { intervalMs: 1000 });
        void freshness.prime({ total: 2 });
        return () => null;
      },
    });

    getWithMeta
      .mockResolvedValueOnce({
        data: { total: 3 },
        etag: 'W/"next"',
        lastModified: null,
        status: 200,
      })
      .mockResolvedValueOnce({
        data: { total: 4 },
        etag: 'W/"back"',
        lastModified: null,
        status: 200,
      });

    const mounted = await mountComponent(TestHarness);

    path.value = "/issues?limit=100&q=search";
    await Promise.resolve();
    path.value = "/issues?limit=100";
    await Promise.resolve();

    expect(getWithMeta).toHaveBeenNthCalledWith(1, "/issues?limit=100&q=search");
    expect(getWithMeta).toHaveBeenNthCalledWith(2, "/issues?limit=100");

    await mounted.unmount();
  });

  it("marks matching live changes stale and refreshes latest data", async () => {
    setActivePinia(createPinia());
    const path = ref("/issues?project=7");
    const applied: number[] = [];
    let freshness!: ReturnType<typeof useFreshness<{ total: number }>>;

    const TestHarness = defineComponent({
      setup() {
        freshness = useFreshness(path, {
          intervalMs: 1000,
          apply: (payload) => applied.push(payload.total),
          count: (payload) => payload.total,
          changes: (event) =>
            event.subject_type === "issue" && event.project_id === 7,
        });
        void freshness.prime({ total: 2 });
        return () => null;
      },
    });

    const mounted = await mountComponent(TestHarness);
    const changes = useChangesStore();

    changes.publish({
      id: 1,
      mutation_type: "update",
      subject_type: "issue",
      subject_id: 10,
      project_id: 8,
      user_id: 1,
      created_at: "2026-07-07T10:00:00Z",
    });
    expect(freshness.stale.value).toBe(false);

    changes.publish({
      id: 2,
      mutation_type: "update",
      subject_type: "issue",
      subject_id: 11,
      project_id: 7,
      user_id: 1,
      created_at: "2026-07-07T10:01:00Z",
    });
    expect(freshness.stale.value).toBe(true);
    expect(freshness.newCount.value).toBeNull();

    getWithMeta.mockResolvedValueOnce({
      data: { total: 4 },
      etag: 'W/"live"',
      lastModified: null,
      status: 200,
    });

    await freshness.refresh();

    expect(getWithMeta).toHaveBeenCalledWith("/issues?project=7");
    expect(applied).toEqual([4]);
    expect(freshness.stale.value).toBe(false);

    await mounted.unmount();
  });
});
