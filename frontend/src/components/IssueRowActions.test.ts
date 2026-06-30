import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, h, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";

import { api } from "@/api/client";
import IssueRowActions from "./IssueRowActions.vue";

vi.mock("@/api/client", () => ({
  api: { post: vi.fn() },
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

function mountRow(props: Record<string, unknown> = {}) {
  const el = document.createElement("div");
  document.body.appendChild(el);
  const pinia = createPinia();
  setActivePinia(pinia);
  const Host = defineComponent({
    render() {
      return h(IssueRowActions, {
        canHaveChildren: false,
        issueId: 42,
        issueType: "ticket",
        issueKey: "PAI-42",
        isAdmin: true,
        ...props,
      });
    },
  });
  const app = createApp(Host);
  app.use(pinia);
  app.mount(el);
  return {
    el,
    unmount() {
      app.unmount();
      el.remove();
    },
  };
}

describe("IssueRowActions — Implement this (PAI-610)", () => {
  beforeEach(() => vi.mocked(api.post).mockReset());
  afterEach(() => {
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("posts to /implement and shows the queued feedback", async () => {
    vi.mocked(api.post).mockResolvedValue({});
    const { el, unmount } = mountRow();
    const btn = el.querySelector<HTMLButtonElement>(".row-act--implement");
    expect(btn).toBeTruthy();
    btn!.click();
    await settle();
    expect(api.post).toHaveBeenCalledWith("/issues/42/implement", {});
    expect(el.textContent).toContain("Queued");
    unmount();
  });

  it("offers a view-the-run follow-through after queueing (PAI-618)", async () => {
    vi.mocked(api.post).mockResolvedValue({});
    let viewed = 0;
    const { el, unmount } = mountRow({ onView: () => (viewed += 1) });
    el.querySelector<HTMLButtonElement>(".row-act--implement")!.click();
    await settle();
    const link = el.querySelector<HTMLButtonElement>(".implement-status--link");
    expect(link).toBeTruthy();
    link!.click();
    await settle();
    expect(viewed).toBe(1);
    unmount();
  });

  it("surfaces an error when the POST fails", async () => {
    vi.mocked(api.post).mockRejectedValue(new Error("nope"));
    const { el, unmount } = mountRow();
    el.querySelector<HTMLButtonElement>(".row-act--implement")!.click();
    await settle();
    expect(el.textContent).toContain("Failed");
    unmount();
  });

  it("ignores a re-click while a request is in flight", async () => {
    vi.mocked(api.post).mockImplementation(() => new Promise(() => {})); // never resolves
    const { el, unmount } = mountRow();
    const btn = el.querySelector<HTMLButtonElement>(".row-act--implement")!;
    btn.click();
    await settle();
    btn.click();
    await settle();
    expect(api.post).toHaveBeenCalledTimes(1);
    unmount();
  });

  it("hides the action for non-implementable issue types", async () => {
    const { el, unmount } = mountRow({ issueType: "cost_unit" });
    expect(el.querySelector(".row-act--implement")).toBeNull();
    unmount();
  });

  it("shows the latest AI work state until a transient implement message takes over", async () => {
    vi.mocked(api.post).mockResolvedValue({});
    let viewed = 0;
    const { el, unmount } = mountRow({
      onView: () => (viewed += 1),
      aiWorkStatus: {
        id: 7,
        status: "deployed",
        agent_name: "claude",
        device_id: "dev-1",
        version: "4.6.4",
        deploy_target: "local-dev",
        tests_summary: "npm test passed",
        error: "",
        created_at: "2026-06-30 09:33:39",
        started_at: "2026-06-30 09:33:40",
        finished_at: "2026-06-30 09:34:35",
      },
    });
    const badge = el.querySelector<HTMLElement>(".ai-work-badge");
    expect(badge?.textContent).toContain("AI deployed");
    expect(badge?.getAttribute("title")).toContain("v4.6.4");
    expect(badge?.getAttribute("title")).toContain("local-dev");
    expect(badge?.getAttribute("title")).toContain("npm test passed");
    badge!.click();
    await settle();
    expect(viewed).toBe(1);

    el.querySelector<HTMLButtonElement>(".row-act--implement")!.click();
    await settle();
    expect(el.querySelector(".ai-work-badge")).toBeNull();
    expect(el.textContent).toContain("Queued");
    unmount();
  });
});
