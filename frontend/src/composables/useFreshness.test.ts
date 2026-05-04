import { ref, defineComponent } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { mountComponent } from "@/components/ai/testMount";
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

    freshness.refresh();
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
});
