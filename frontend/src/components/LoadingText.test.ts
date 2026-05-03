import { afterEach, describe, expect, it } from "vitest";
import { createApp, nextTick } from "vue";
import LoadingText from "@/components/LoadingText.vue";

async function mountLoadingText(props: Record<string, unknown> = {}) {
  const el = document.createElement("div");
  document.body.appendChild(el);
  const app = createApp(LoadingText, props);
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

describe("LoadingText", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("renders a polite busy status with the supplied label", async () => {
    const mounted = await mountLoadingText({ label: "Loading documents..." });
    const status = mounted.el.querySelector('[role="status"]');

    expect(status?.getAttribute("aria-live")).toBe("polite");
    expect(status?.getAttribute("aria-busy")).toBe("true");
    expect(status?.textContent).toContain("Loading documents...");

    mounted.unmount();
  });

  it("can render as an inline element", async () => {
    const mounted = await mountLoadingText({ as: "span", label: "Loading more..." });

    expect(mounted.el.querySelector("span.loading-text")).toBeTruthy();

    mounted.unmount();
  });
});
