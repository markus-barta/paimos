import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, h, nextTick } from "vue";

import { api } from "@/api/client";
import AgentRunPanel from "./AgentRunPanel.vue";

vi.mock("@/api/client", () => ({
  api: { get: vi.fn(), post: vi.fn() },
  errMsg: (_e: unknown, fallback: string) => fallback,
}));

vi.mock("@/components/AppIcon.vue", () => ({
  default: { props: ["name"], template: '<span class="icon-stub" />' },
}));

async function settle() {
  for (let i = 0; i < 5; i += 1) {
    await Promise.resolve();
    await nextTick();
  }
}

function mountPanel() {
  const el = document.createElement("div");
  document.body.appendChild(el);
  const Host = defineComponent({
    render() {
      return h(AgentRunPanel, { issueId: 5, issueKey: "PAI-5", projectId: 9 });
    },
  });
  const app = createApp(Host);
  app.mount(el);
  return {
    el,
    unmount() {
      app.unmount();
      el.remove();
    },
  };
}

function run(status: string, extra: Record<string, unknown> = {}) {
  return {
    id: 1,
    status,
    version: "",
    device_id: "laptop",
    deploy_target: "",
    tests_summary: null,
    error: "",
    created_at: "2026-06-29 10:00:00",
    started_at: null,
    finished_at: null,
    ...extra,
  };
}

describe("AgentRunPanel — PAI-610", () => {
  beforeEach(() => {
    vi.mocked(api.get).mockReset();
    vi.mocked(api.post).mockReset();
  });
  afterEach(() => {
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("renders a run's status and starts a run via the issue key", async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === "/issues/5/runs")
        return { runs: [run("deployed", {
          version: "4.6.0",
          deploy_target: "ppm",
          tests_summary: "npm test passed: 2 passed",
          finished_at: "2026-06-29 10:05:00",
        })] };
      if (path === "/projects/9/runners") return { runners: [{ user_id: 1, device_id: "laptop", last_seen: "" }] };
      return {};
    });
    vi.mocked(api.post).mockResolvedValue({});

    const { el, unmount } = mountPanel();
    await settle();

    expect(el.textContent).toContain("Deployed");
    expect(el.textContent).toContain("v4.6.0");
    expect(el.textContent).toContain("npm test passed: 2 passed");
    expect(el.querySelector(".arp-device")).toBeNull(); // 1 runner → no picker

    const btn = el.querySelector<HTMLButtonElement>(".btn-primary");
    expect(btn?.textContent).toContain("Implement this");
    btn!.click();
    await settle();
    expect(api.post).toHaveBeenCalledWith(
      "/issues/PAI-5/implement",
      { device_id: "laptop" },
    );
    unmount();
  });

  it("hints when no runner is online (vs. a runners-endpoint error)", async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === "/issues/5/runs") return { runs: [] };
      if (path === "/projects/9/runners") return { runners: [] };
      return {};
    });
    const { el, unmount } = mountPanel();
    await settle();
    expect(el.textContent).toContain("No runner is online");
    expect(el.textContent).toContain("No runs yet");
    unmount();
  });

  it("surfaces a runners-endpoint error distinctly from 'no runners' (M4)", async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === "/issues/5/runs") return { runs: [] };
      if (path === "/projects/9/runners") throw new Error("boom");
      return {};
    });
    const { el, unmount } = mountPanel();
    await settle();
    expect(el.textContent).toContain("Couldn't check for runners");
    expect(el.textContent).not.toContain("No runner is online");
    unmount();
  });

  it("renders the device picker with >1 runner and posts the selected device (M5)", async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === "/issues/5/runs") return { runs: [] };
      if (path === "/projects/9/runners")
        return { runners: [
          { user_id: 1, device_id: "laptop", last_seen: "" },
          { user_id: 1, device_id: "desktop", last_seen: "" },
        ] };
      return {};
    });
    vi.mocked(api.post).mockResolvedValue({});
    const { el, unmount } = mountPanel();
    await settle();

    const picker = el.querySelector<HTMLSelectElement>(".arp-device");
    expect(picker).toBeTruthy();
    expect(picker!.options.length).toBe(2);
    // Actually change the selection to prove v-model drives the payload (M1).
    picker!.value = "desktop";
    picker!.dispatchEvent(new Event("change"));
    await settle();

    el.querySelector<HTMLButtonElement>(".btn-primary")!.click();
    await settle();
    expect(api.post).toHaveBeenCalledWith(
      "/issues/PAI-5/implement",
      expect.objectContaining({ device_id: "desktop" }),
    );
    unmount();
  });

  it("posts an explicit deploy target only when the user sets one", async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === "/issues/5/runs") return { runs: [] };
      if (path === "/projects/9/runners") return { runners: [{ user_id: 1, device_id: "laptop", last_seen: "" }] };
      return {};
    });
    vi.mocked(api.post).mockResolvedValue({});
    const { el, unmount } = mountPanel();
    await settle();

    const target = el.querySelector<HTMLInputElement>(".arp-deploy-target");
    expect(target).toBeTruthy();
    target!.value = "local-dev";
    target!.dispatchEvent(new Event("input"));
    await settle();

    el.querySelector<HTMLButtonElement>(".btn-primary")!.click();
    await settle();
    expect(api.post).toHaveBeenCalledWith(
      "/issues/PAI-5/implement",
      expect.objectContaining({ device_id: "laptop", deploy_target: "local-dev" }),
    );
    unmount();
  });

  it("renders a timestamp as a valid ISO datetime + a local label (M2/M6)", async () => {
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === "/issues/5/runs") return { runs: [run("deployed")] };
      if (path === "/projects/9/runners") return { runners: [] };
      return {};
    });
    const { el, unmount } = mountPanel();
    await settle();
    const t = el.querySelector("time");
    expect(t).toBeTruthy();
    const dt = t!.getAttribute("datetime")!;
    expect(dt.endsWith("Z")).toBe(true); // UTC, not shifted to local
    expect(Number.isNaN(Date.parse(dt))).toBe(false);
    expect(t!.textContent).not.toContain("Invalid Date");
    expect(t!.textContent!.trim().length).toBeGreaterThan(0);
    unmount();
  });
});

describe("AgentRunPanel — polling lifecycle (H2)", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(api.get).mockReset();
    vi.mocked(api.post).mockReset();
  });
  afterEach(() => {
    vi.useRealTimers();
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("polls an in-flight run every 4s and stops once it reaches a result state", async () => {
    const statuses = ["queued", "running", "tests_passed"];
    let runsCalls = 0;
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === "/issues/5/runs") {
        const s = statuses[Math.min(runsCalls, statuses.length - 1)];
        runsCalls += 1;
        return { runs: [run(s)] };
      }
      if (path === "/projects/9/runners") return { runners: [{ user_id: 1, device_id: "laptop", last_seen: "" }] };
      return {};
    });

    const { el, unmount } = mountPanel();
    await vi.advanceTimersByTimeAsync(0); // flush onMounted fetches
    expect(el.textContent).toContain("Queued");

    await vi.advanceTimersByTimeAsync(4000); // tick 1 → running
    expect(el.textContent).toContain("Running");

    await vi.advanceTimersByTimeAsync(4000); // tick 2 → tests_passed (finished → stop)
    expect(el.textContent).toContain("Tests passed");

    const callsAfterTerminal = runsCalls;
    await vi.advanceTimersByTimeAsync(12000); // no further polling once finished
    expect(runsCalls).toBe(callsAfterTerminal);

    unmount();
  });
});

describe("AgentRunPanel — visibility + leak (H1/H2)", () => {
  let hidden = false;
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(api.get).mockReset();
    vi.mocked(api.post).mockReset();
    hidden = false;
    Object.defineProperty(document, "hidden", { configurable: true, get: () => hidden });
  });
  afterEach(() => {
    vi.useRealTimers();
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("pauses polling while the tab is hidden and catches up on re-show (H2)", async () => {
    let runsCalls = 0;
    vi.mocked(api.get).mockImplementation(async (path: string) => {
      if (path === "/issues/5/runs") {
        runsCalls += 1;
        return { runs: [run("queued")] };
      }
      if (path === "/projects/9/runners") return { runners: [] };
      return {};
    });
    const { unmount } = mountPanel();
    await vi.advanceTimersByTimeAsync(0);
    const afterMount = runsCalls;
    await vi.advanceTimersByTimeAsync(4000);
    expect(runsCalls).toBeGreaterThan(afterMount); // polling while visible

    hidden = true;
    document.dispatchEvent(new Event("visibilitychange"));
    const afterHide = runsCalls;
    await vi.advanceTimersByTimeAsync(12000);
    expect(runsCalls).toBe(afterHide); // paused while hidden

    hidden = false;
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(0);
    expect(runsCalls).toBeGreaterThan(afterHide); // caught up on re-show
    unmount();
  });

  it("leaves no polling timer after unmounting mid-fetch (H1)", async () => {
    let landRun: () => void = () => {};
    let runsCalls = 0;
    vi.mocked(api.get).mockImplementation((path: string) => {
      if (path === "/issues/5/runs") {
        runsCalls += 1;
        return new Promise((r) => {
          landRun = () => r({ runs: [run("queued")] });
        });
      }
      return Promise.resolve({ runners: [] });
    });
    const { unmount } = mountPanel(); // onMounted → fetchRuns is pending
    await Promise.resolve();
    unmount(); // unmount before the fetch resolves
    landRun(); // the fetch now lands on a dead component
    await vi.advanceTimersByTimeAsync(0);
    const settled = runsCalls;
    await vi.advanceTimersByTimeAsync(20000); // an orphan interval would poll here
    expect(runsCalls).toBe(settled); // no leak
  });
});
