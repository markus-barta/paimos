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
    vi.mocked(api.post).mockResolvedValue({ id: 12 });
    const { el, unmount } = mountRow();
    const btn = el.querySelector<HTMLButtonElement>(".row-act--implement");
    expect(btn).toBeTruthy();
    expect(btn?.textContent).toContain("Run");
    btn!.click();
    await settle();
    expect(api.post).toHaveBeenCalledWith("/issues/42/implement", {});
    expect(el.textContent).toContain("Run #12 queued");
    unmount();
  });

  it("offers a view-the-run follow-through after queueing (PAI-618)", async () => {
    vi.mocked(api.post).mockResolvedValue({ id: 13 });
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

  it("renders explicit provider row actions when multiple actions are available", async () => {
    vi.mocked(api.post).mockResolvedValue({ id: 14 });
    const { el, unmount } = mountRow({
      agentActions: [
        { action_key: "claude_cli.implement", provider_kind: "local_cli", provider_id: "claude_cli", label: "Claude Code", run_modes: ["edit"], can_test: true, can_deploy: false },
        { action_key: "codex_cli.implement", provider_kind: "local_cli", provider_id: "codex_cli", label: "Codex CLI", run_modes: ["edit"], can_test: true, can_deploy: false },
      ],
    });
    const buttons = [...el.querySelectorAll<HTMLButtonElement>(".row-run-action--start")];
    expect(buttons.map((b) => b.textContent?.trim())).toEqual(["Claude", "Codex"]);
    buttons[1].click();
    await settle();
    expect(api.post).toHaveBeenCalledWith("/issues/42/implement", {
      action_key: "codex_cli.implement",
    });
    expect(el.textContent).toContain("Run #14 queued with Codex");
    unmount();
  });

  it("keeps the one-action shorthand while still recording the action when supplied", async () => {
    vi.mocked(api.post).mockResolvedValue({ id: 15 });
    const { el, unmount } = mountRow({
      agentActions: [
        { action_key: "codex_cli.implement", provider_kind: "local_cli", provider_id: "codex_cli", label: "Codex CLI", run_modes: ["edit"], can_test: true, can_deploy: false },
      ],
    });
    const btn = el.querySelector<HTMLButtonElement>(".row-act--implement");
    expect(btn?.textContent).toContain("Run");
    expect(btn?.textContent).not.toContain("Codex");
    btn!.click();
    await settle();
    expect(api.post).toHaveBeenCalledWith("/issues/42/implement", {
      action_key: "codex_cli.implement",
    });
    unmount();
  });

  it("includes the selected project agent when one is provided", async () => {
    vi.mocked(api.post).mockResolvedValue({ id: 16 });
    const { el, unmount } = mountRow({ agentName: "codex" });
    el.querySelector<HTMLButtonElement>(".row-act--implement")!.click();
    await settle();
    expect(api.post).toHaveBeenCalledWith("/issues/42/implement", {
      agent_name: "codex",
    });
    expect(el.textContent).toContain("Run #16 queued as codex");
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

  it("turns the row AI action into Open run when a run already exists", async () => {
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
    const openRun = el.querySelector<HTMLButtonElement>(".row-run-action--open");
    expect(openRun?.textContent).toContain("Open run");
    expect(el.querySelector(".row-act--implement")).toBeNull();
    openRun!.click();
    await settle();
    expect(viewed).toBe(1);
    expect(api.post).not.toHaveBeenCalled();
    unmount();
  });
});
