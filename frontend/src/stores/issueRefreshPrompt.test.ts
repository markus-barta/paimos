import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { useIssueRefreshPromptStore } from "./issueRefreshPrompt";

describe("issueRefreshPrompt store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("only triggers refresh while a stale issue list is visible", () => {
    const store = useIssueRefreshPromptStore();
    const refresh = vi.fn();

    expect(store.triggerRefresh()).toBe(false);
    expect(refresh).not.toHaveBeenCalled();

    store.show(2, refresh);
    expect(store.visible).toBe(true);
    expect(store.label).toBe("2 issues updated");
    expect(store.triggerRefresh()).toBe(true);
    expect(refresh).toHaveBeenCalledTimes(1);

    store.clear(refresh);
    expect(store.visible).toBe(false);
    expect(store.triggerRefresh()).toBe(false);
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("keeps a newer owner from being cleared by a stale owner", () => {
    const store = useIssueRefreshPromptStore();
    const oldRefresh = vi.fn();
    const nextRefresh = vi.fn();

    store.show(null, oldRefresh);
    store.show(1, nextRefresh);
    store.clear(oldRefresh);

    expect(store.visible).toBe(true);
    expect(store.label).toBe("1 issue updated");
    expect(store.triggerRefresh()).toBe(true);
    expect(oldRefresh).not.toHaveBeenCalled();
    expect(nextRefresh).toHaveBeenCalledTimes(1);
  });
});
