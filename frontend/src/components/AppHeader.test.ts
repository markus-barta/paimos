import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import AppHeader from "@/components/AppHeader.vue";
import { useIssueRefreshPromptStore } from "@/stores/issueRefreshPrompt";

vi.mock("vue-router", () => ({
  useRoute: () => ({ path: "/issues" }),
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("@/components/AppIcon.vue", () => ({
  default: {
    props: ["name"],
    template: '<span class="icon-stub" :data-icon="name"></span>',
  },
}));

vi.mock("@/components/SearchPalette.vue", () => ({
  default: {
    props: ["visible", "anchor"],
    methods: {
      handleKeydown: vi.fn(),
    },
    template: '<div v-if="visible" class="search-palette-stub"></div>',
  },
}));

vi.mock("@/stores/undo", () => ({
  useUndoStore: () => ({
    undoRows: [],
    redoRows: [],
    historyRows: [],
    panelOpen: false,
    refresh: vi.fn(),
    openPanel: vi.fn(),
    closePanel: vi.fn(),
  }),
}));

async function mountHeader() {
  const el = document.createElement("div");
  document.body.appendChild(el);

  const pinia = createPinia();
  setActivePinia(pinia);
  const store = useIssueRefreshPromptStore(pinia);

  const app = createApp(AppHeader);
  app.use(pinia);
  app.mount(el);
  await nextTick();

  return {
    el,
    store,
    async unmount() {
      app.unmount();
      el.remove();
      await nextTick();
    },
  };
}

function refreshShortcut() {
  const event = new KeyboardEvent("keydown", {
    key: "r",
    metaKey: true,
    bubbles: true,
    cancelable: true,
  });
  window.dispatchEvent(event);
  return event;
}

async function flushDomUpdate() {
  await nextTick();
  await new Promise((resolve) => window.setTimeout(resolve, 0));
  await nextTick();
}

describe("AppHeader issue refresh prompt", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    localStorage.clear();
    vi.clearAllMocks();
  });

  it("keeps search visible and leaves browser refresh alone by default", async () => {
    const mounted = await mountHeader();
    const refresh = vi.fn();

    expect(mounted.el.querySelector('input[type="search"]')).toBeTruthy();
    expect(mounted.el.querySelector(".ah-refresh-prompt")).toBeFalsy();

    const event = refreshShortcut();

    expect(event.defaultPrevented).toBe(false);
    expect(refresh).not.toHaveBeenCalled();

    await mounted.unmount();
  });

  it("replaces search with the prompt and handles the refresh shortcut when stale", async () => {
    const mounted = await mountHeader();
    const refresh = vi.fn();

    mounted.store.show(3, refresh);
    await flushDomUpdate();

    expect(mounted.el.querySelector('input[type="search"]')).toBeFalsy();
    expect(mounted.el.querySelector(".ah-refresh-prompt")?.textContent).toContain(
      "3 issues updated",
    );

    const event = refreshShortcut();

    expect(event.defaultPrevented).toBe(true);
    expect(refresh).toHaveBeenCalledTimes(1);

    await mounted.unmount();
  });
});
