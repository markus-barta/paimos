import { afterEach, describe, expect, it } from "vitest";
import { createApp, nextTick } from "vue";
import AppIcon from "@/components/AppIcon.vue";

async function mountIcon(name: string) {
  const el = document.createElement("div");
  document.body.appendChild(el);
  const app = createApp(AppIcon, { name });
  app.mount(el);
  await nextTick();
  return {
    el,
    unmount() {
      app.unmount();
      el.remove();
    },
  };
}

describe("AppIcon", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("renders registered kebab-case lucide icons", async () => {
    const mounted = await mountIcon("git-branch-plus");

    expect(mounted.el.querySelector("svg")?.classList.contains("lucide-git-branch-plus")).toBe(true);

    mounted.unmount();
  });

  it("falls back for unknown icon names", async () => {
    const mounted = await mountIcon("not-registered");

    expect(mounted.el.querySelector("svg")?.classList.contains("lucide-circle-question-mark")).toBe(true);

    mounted.unmount();
  });
});
