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
      if (path === "/issues/5/runs") {
        return {
          runs: [
            {
              id: 1,
              status: "deployed",
              version: "4.6.0",
              device_id: "laptop",
              deploy_target: "ppm",
              tests_summary: null,
              error: "",
              created_at: "2026-06-29T10:00:00Z",
              started_at: "2026-06-29T10:00:01Z",
              finished_at: "2026-06-29T10:01:00Z",
            },
          ],
        };
      }
      if (path === "/projects/9/runners") {
        return { runners: [{ user_id: 1, device_id: "laptop", last_seen: "" }] };
      }
      return {};
    });
    vi.mocked(api.post).mockResolvedValue({});

    const { el, unmount } = mountPanel();
    await settle();

    // The deployed run renders with its status + version.
    expect(el.textContent).toContain("Deployed");
    expect(el.textContent).toContain("v4.6.0");
    // One runner → no device picker.
    expect(el.querySelector(".arp-device")).toBeNull();

    // Clicking "Implement this" posts to the key-based endpoint.
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

  it("hints when no runner is online", async () => {
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
});
