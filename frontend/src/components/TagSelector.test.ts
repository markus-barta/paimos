import { afterEach, describe, expect, it } from "vitest";
import { nextTick } from "vue";
import { mountComponent } from "@/components/ai/testMount";
import TagSelector from "@/components/TagSelector.vue";

const tags = [
  { id: 1, name: "bug", color: "red", description: "", created_at: "" },
  { id: 2, name: "ux", color: "blue", description: "", created_at: "" },
];

describe("TagSelector", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("renders pill mode as removable tags plus a ghost add pill", async () => {
    const mounted = await mountComponent(TagSelector, {
      allTags: tags,
      selectedIds: [1],
      variant: "pills",
      addLabel: "Add tag",
    });

    expect(mounted.el.textContent).toContain("bug");
    expect(mounted.el.querySelector(".tag-remove")).toBeTruthy();
    expect(mounted.el.querySelector(".tag-add-pill")?.textContent).toContain("Add tag");
    expect(mounted.el.querySelector(".tag-input")).toBeFalsy();

    mounted.el.querySelector<HTMLButtonElement>(".tag-add-pill")?.click();
    await nextTick();

    expect(mounted.el.querySelector(".tag-input--dropdown")).toBeTruthy();
    expect(mounted.el.textContent).toContain("ux");

    await mounted.unmount();
  });
});
