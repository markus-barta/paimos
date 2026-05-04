import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import SearchPalette from "@/components/SearchPalette.vue";
import { useSearchStore } from "@/stores/search";
import { api } from "@/api/client";

vi.mock("@/api/client", () => ({
  api: {
    get: vi.fn(),
  },
}));

vi.mock("@/components/AppIcon.vue", () => ({
  default: {
    props: ["name"],
    template: '<span class="icon-stub" :data-icon="name"></span>',
  },
}));

vi.mock("@/components/StatusDot.vue", () => ({
  default: {
    props: ["status"],
    template: '<span class="status-dot-stub" :data-status="status"></span>',
  },
}));

interface MountedPalette {
  el: HTMLElement;
  vm: Record<string, unknown>;
  search: ReturnType<typeof useSearchStore>;
  navigate: ReturnType<typeof vi.fn>;
  close: ReturnType<typeof vi.fn>;
  unmount: () => Promise<void>;
}

function issue(id: number, issueKey: string, title: string) {
  return {
    id,
    issue_key: issueKey,
    title,
    type: "ticket",
    status: "new",
    priority: "medium",
    project_id: 6,
    project_key: "PAI",
    assignee_username: null,
  };
}

async function mountPalette(): Promise<MountedPalette> {
  const el = document.createElement("div");
  document.body.appendChild(el);

  const anchor = document.createElement("div");
  anchor.getBoundingClientRect = () =>
    ({
      top: 10,
      bottom: 42,
      left: 20,
      width: 320,
      height: 32,
      right: 340,
      x: 20,
      y: 10,
      toJSON: () => ({}),
    }) as DOMRect;

  const navigate = vi.fn();
  const close = vi.fn();
  const pinia = createPinia();
  setActivePinia(pinia);

  const app = createApp(SearchPalette, {
    visible: true,
    anchor,
    onNavigate: navigate,
    onClose: close,
  });
  app.use(pinia);
  const vm = app.mount(el) as unknown as Record<string, unknown>;
  await nextTick();

  return {
    el,
    vm,
    search: useSearchStore(pinia),
    navigate,
    close,
    async unmount() {
      app.unmount();
      el.remove();
      await nextTick();
    },
  };
}

async function resolveSearch() {
  await nextTick();
  await vi.advanceTimersByTimeAsync(151);
  await nextTick();
}

function activeIssueKey() {
  return document.body
    .querySelector(".sp-item--active .sp-key")
    ?.textContent?.trim();
}

function keydown(mounted: MountedPalette, key: string, init: KeyboardEventInit = {}) {
  const event = new KeyboardEvent("keydown", {
    key,
    bubbles: true,
    cancelable: true,
    ...init,
  });
  (mounted.vm.handleKeydown as (event: KeyboardEvent) => void)(event);
  return event;
}

describe("SearchPalette keyboard selection", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(api.get).mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
    document.body.innerHTML = "";
    localStorage.clear();
  });

  it("moves through rendered issue rows and the all-results action", async () => {
    vi.mocked(api.get).mockResolvedValue({
      issues: [
        issue(1, "PAI-1", "First"),
        issue(2, "PAI-2", "Direct match"),
        issue(3, "PAI-3", "Third"),
      ],
      projects: [],
      has_more: true,
    });
    const mounted = await mountPalette();

    mounted.search.setQuery("PAI-2");
    await resolveSearch();

    expect(activeIssueKey()).toBe("PAI-2");

    keydown(mounted, "ArrowDown");
    await nextTick();
    expect(activeIssueKey()).toBe("PAI-1");

    keydown(mounted, "ArrowDown");
    await nextTick();
    expect(activeIssueKey()).toBe("PAI-3");

    keydown(mounted, "ArrowDown");
    await nextTick();
    expect(document.body.querySelector(".sp-more--active")).toBeTruthy();

    const enter = keydown(mounted, "Enter");

    expect(enter.defaultPrevented).toBe(true);
    expect(mounted.navigate).toHaveBeenCalledWith("/issues?q=PAI-2");
    expect(mounted.close).toHaveBeenCalledTimes(1);
    expect(mounted.search.query).toBe("PAI-2");

    await mounted.unmount();
  });

  it("opens the active issue with Enter and all results with Command Enter", async () => {
    vi.mocked(api.get).mockResolvedValue({
      issues: [issue(7, "PAI-7", "Selected result")],
      projects: [],
      has_more: false,
    });
    const mounted = await mountPalette();

    mounted.search.setQuery("selected");
    await resolveSearch();

    keydown(mounted, "Enter");
    expect(mounted.navigate).toHaveBeenCalledWith("/projects/6/issues/7");

    mounted.navigate.mockClear();
    mounted.close.mockClear();
    keydown(mounted, "Enter", { metaKey: true });

    expect(mounted.navigate).toHaveBeenCalledWith("/issues?q=selected");
    expect(mounted.close).toHaveBeenCalledTimes(1);

    await mounted.unmount();
  });

  it("scopes palette fetches and all-results navigation to the current project", async () => {
    vi.mocked(api.get).mockResolvedValue({
      issues: [issue(8, "PAI-8", "Project result")],
      projects: [],
      has_more: true,
    });
    const mounted = await mountPalette();

    mounted.search.setProjectContext(6, "PAI");
    mounted.search.setQuery("project");
    await resolveSearch();

    expect(api.get).toHaveBeenCalledWith(
      "/search?q=project&limit=10&scope=project&project_id=6",
    );

    keydown(mounted, "Enter", { metaKey: true });

    expect(mounted.navigate).toHaveBeenCalledWith("/projects/6?q=project");

    await mounted.unmount();
  });
});
