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
      if (path === "/issues/5/runs") return { runs: [run("deployed", { version: "4.6.0", deploy_target: "ppm" })] };
      if (path === "/projects/9/runners") return { runners: [{ user_id: 1, device_id: "laptop", last_seen: "" }] };
      return {};
    });
    vi.mocked(api.post).mockResolvedValue({});

    const { el, unmount } = mountPanel();
    await settle();

    expect(el.textContent).toContain("Deployed");
    expect(el.textContent).toContain("v4.6.0");
    expect(el.querySelector(".arp-device")).toBeNull(); // 1 runner → no picker

    const btn = el.querySelector<HTMLButtonElement>(".btn-primary");
    expect(btn?.textContent).toContain("Implement this");
    btn!.click();
    await settle();
    expect(api.post).toHaveBeenCalledWith(
      "/issues/PAI-5/implement",
      expect.objectContaining({ device_id: "laptop" }),
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

    el.querySelector<HTMLButtonElement>(".btn-primary")!.click();
    await settle();
    expect(api.post).toHaveBeenCalledWith(
      "/issues/PAI-5/implement",
      expect.objectContaining({ device_id: "laptop" }),
    );
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

  it("polls a non-terminal run every 4s and stops once it goes terminal", async () => {
    const statuses = ["queued", "running", "deployed"];
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

    await vi.advanceTimersByTimeAsync(4000); // tick 2 → deployed (terminal → stop)
    expect(el.textContent).toContain("Deployed");

    const callsAfterTerminal = runsCalls;
    await vi.advanceTimersByTimeAsync(12000); // no further polling once terminal
    expect(runsCalls).toBe(callsAfterTerminal);

    unmount();
  });
});
