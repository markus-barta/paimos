import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import AppHeader from "@/components/AppHeader.vue";
import { useIssueRefreshPromptStore } from "@/stores/issueRefreshPrompt";

const { routerPush, mockAuthStore, mockSearchStore } = vi.hoisted(() => ({
  routerPush: vi.fn(),
  mockAuthStore: { user: null as Record<string, unknown> | null },
  mockSearchStore: (() => {
    const store = {
      query: "",
      setQuery: vi.fn(),
      clear: vi.fn(),
    };
    store.setQuery = vi.fn((q: string) => {
      store.query = q;
    });
    store.clear = vi.fn(() => {
      store.query = "";
    });
    return store;
  })(),
}));

vi.mock("vue-router", () => ({
  useRoute: () => ({ path: "/issues" }),
  useRouter: () => ({ push: routerPush }),
}));

vi.mock("@/stores/auth", () => ({
  useAuthStore: () => mockAuthStore,
}));

vi.mock("@/stores/search", () => ({
  useSearchStore: () => mockSearchStore,
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

function fakeUser(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    username: "mba",
    role: "admin",
    created_at: "2026-01-01T00:00:00Z",
    nickname: "",
    first_name: "",
    last_name: "",
    email: "",
    avatar_path: "",
    markdown_default: true,
    monospace_fields: false,
    recent_projects_limit: 3,
    internal_rate_hourly: null,
    show_alt_unit_table: false,
    show_alt_unit_detail: false,
    locale: "en",
    recent_timers_limit: 5,
    timezone: "auto",
    preview_hover_delay: 1000,
    issue_auto_refresh_enabled: true,
    issue_auto_refresh_interval_seconds: 60,
    last_login_at: null,
    accruals_stats_enabled: false,
    accruals_extra_statuses: "",
    ...overrides,
  };
}

async function mountHeader(userOverrides: Record<string, unknown> = {}) {
  const el = document.createElement("div");
  document.body.appendChild(el);

  const pinia = createPinia();
  setActivePinia(pinia);
  const store = useIssueRefreshPromptStore(pinia);
  mockAuthStore.user = fakeUser(userOverrides);

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

async function flushCenterSwapTransition() {
  await flushDomUpdate();
  await new Promise((resolve) => window.setTimeout(resolve, 180));
  await flushDomUpdate();
}

async function flushCenterSwapTransitionWithFakeTimers() {
  await nextTick();
  vi.advanceTimersByTime(180);
  await nextTick();
}

describe("AppHeader issue refresh prompt", () => {
  afterEach(() => {
    vi.useRealTimers();
    mockAuthStore.user = null;
    mockSearchStore.query = "";
    document.body.innerHTML = "";
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
    await flushCenterSwapTransition();

    expect(mounted.el.querySelector('input[type="search"]')).toBeFalsy();
    expect(mounted.el.querySelector(".ah-refresh-prompt")?.textContent).toContain(
      "3 issues updated",
    );

    const event = refreshShortcut();

    expect(event.defaultPrevented).toBe(true);
    expect(refresh).toHaveBeenCalledTimes(1);

    await mounted.unmount();
  });

  it("shows a muted countdown that opens the account preference", async () => {
    const mounted = await mountHeader();

    mounted.store.show(null, vi.fn());
    await flushCenterSwapTransition();

    const countdown = mounted.el.querySelector<HTMLButtonElement>(
      ".ah-refresh-countdown",
    );
    expect(countdown?.textContent).toContain("(refreshing in 60s)");
    expect(
      countdown?.querySelector(".ah-refresh-countdown-icon")?.getAttribute("data-icon"),
    ).toBe("settings");

    countdown?.click();

    expect(routerPush).toHaveBeenCalledWith({
      path: "/settings",
      query: { tab: "account", focus: "issue-auto-refresh" },
    });

    await mounted.unmount();
  });

  it("counts down in ten second steps and triggers the refresh", async () => {
    vi.useFakeTimers();
    const mounted = await mountHeader();
    const refresh = vi.fn();

    mounted.store.show(null, refresh);
    await flushCenterSwapTransitionWithFakeTimers();

    expect(
      mounted.el.querySelector(".ah-refresh-countdown")?.textContent,
    ).toContain("(refreshing in 60s)");

    vi.advanceTimersByTime(10_000);
    await nextTick();

    expect(
      mounted.el.querySelector(".ah-refresh-countdown")?.textContent,
    ).toContain("(refreshing in 50s)");

    vi.advanceTimersByTime(50_000);
    await nextTick();

    expect(refresh).toHaveBeenCalledTimes(1);

    await mounted.unmount();
  });

  it("hides the countdown and skips automatic refresh when disabled", async () => {
    vi.useFakeTimers();
    const mounted = await mountHeader({ issue_auto_refresh_enabled: false });
    const refresh = vi.fn();

    mounted.store.show(null, refresh);
    await flushCenterSwapTransitionWithFakeTimers();

    expect(mounted.el.querySelector(".ah-refresh-countdown")).toBeFalsy();

    vi.advanceTimersByTime(60_000);
    await nextTick();

    expect(refresh).not.toHaveBeenCalled();

    await mounted.unmount();
  });
});
