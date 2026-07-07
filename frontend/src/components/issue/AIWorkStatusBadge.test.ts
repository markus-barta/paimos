import { afterEach, describe, expect, it } from "vitest";
import { createApp, defineComponent, h, nextTick } from "vue";

import AIWorkStatusBadge from "./AIWorkStatusBadge.vue";

async function settle() {
  await Promise.resolve();
  await nextTick();
}

function mountBadge(props: Record<string, unknown> = {}, onOpen?: () => void) {
  const el = document.createElement("div");
  document.body.appendChild(el);
  const Host = defineComponent({
    render() {
      return h(AIWorkStatusBadge, {
        run: {
          id: 10,
          status: "deployed",
          agent_name: "claude",
          device_id: "dev-1",
          action_key: "claude_cli.implement",
          provider_kind: "local_cli",
          provider_id: "claude_cli",
          provider_label: "Claude Code",
          model: "",
          run_mode: "edit",
          profile_id: "",
          effort: "",
          prompt_preset_ref: "",
          context_pack: "",
          version: "0.1.2",
          deploy_target: "local-dev",
          tests_summary: "npm test passed: > noisy package script",
          error: "",
          created_at: "2026-06-30 12:32:28",
          started_at: "2026-06-30 12:32:28",
          finished_at: "2026-06-30 12:32:56",
          ...props,
        },
        onOpen,
      });
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

describe("AIWorkStatusBadge", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("renders a concise status and human tooltip", async () => {
    const { el, unmount } = mountBadge();
    await settle();
    const badge = el.querySelector<HTMLButtonElement>(".ai-work-badge");
    expect(badge?.textContent).toContain("Claude Code deployed");
    const title = badge?.getAttribute("title") ?? "";
    expect(title).toContain("Claude Code deployed");
    expect(title).toContain("claude_cli.implement");
    expect(title).toContain("runner v0.1.2");
    expect(title).toContain("target local-dev");
    expect(title).toContain("tests passed");
    expect(title).not.toContain("noisy package script");
    unmount();
  });

  it("labels drafted runs and includes safe execution metadata", async () => {
    const { el, unmount } = mountBadge({
      status: "drafted",
      action_key: "openrouter_draft.implement",
      provider_kind: "hosted_model",
      provider_id: "openrouter",
      provider_label: "OpenRouter Draft",
      model: "openrouter/test-model",
      run_mode: "draft",
      profile_id: "balanced",
      effort: "standard",
      prompt_preset_ref: "kb:runbook:draft@rev1",
      context_pack: "knowledge",
      deploy_target: "",
      tests_summary: "",
    });
    await settle();
    const badge = el.querySelector<HTMLButtonElement>(".ai-work-badge");
    expect(badge?.textContent).toContain("OpenRouter Draft draft ready");
    const title = badge?.getAttribute("title") ?? "";
    expect(title).toContain("profile balanced");
    expect(title).toContain("effort standard");
    expect(title).toContain("prompt kb:runbook:draft@rev1");
    expect(title).toContain("context knowledge");
    expect(title).not.toContain("API");
    unmount();
  });

  it("opens run history when clicked", async () => {
    let opened = 0;
    const { el, unmount } = mountBadge({}, () => {
      opened += 1;
    });
    await settle();
    el.querySelector<HTMLButtonElement>(".ai-work-badge")!.click();
    await settle();
    expect(opened).toBe(1);
    unmount();
  });
});
