import { afterEach, describe, expect, it } from "vitest";
import { nextTick } from "vue";
import { mountComponent } from "@/components/ai/testMount";
import IssueAssigneeSelect from "@/components/issue/IssueAssigneeSelect.vue";
import IssueStatusSelect from "@/components/issue/IssueStatusSelect.vue";

describe("issue dropdown components", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("renders canonical status labels with status-dot options", async () => {
    const mounted = await mountComponent(IssueStatusSelect, {
      modelValue: "qa",
      size: "sm",
    });

    expect(mounted.el.textContent).toContain("QA");
    expect(mounted.el.querySelector(".ms-dot")).toBeTruthy();

    await mounted.unmount();
  });

  it("renders assignee avatars in selected state and dropdown options", async () => {
    const users = [
      {
        id: 7,
        username: "marta",
        role: "member",
        status: "active",
        nickname: "MB",
        first_name: "Marta",
        last_name: "B",
        email: "marta@example.com",
        avatar_path: "",
      },
    ];
    const mounted = await mountComponent(IssueAssigneeSelect, {
      modelValue: "7",
      users,
      size: "sm",
    });

    expect(mounted.el.textContent).toContain("marta");
    expect(mounted.el.querySelector(".ua")).toBeTruthy();

    mounted.el.querySelector<HTMLButtonElement>(".meta-select-trigger")?.click();
    await nextTick();

    expect(document.querySelector(".meta-select-dropdown--teleported")?.textContent).toContain("marta");
    expect(document.querySelector(".meta-select-dropdown--teleported .ua")).toBeTruthy();

    await mounted.unmount();
  });

  it("renders a single unassigned option in the assignee dropdown", async () => {
    const users = [
      {
        id: 7,
        username: "marta",
        role: "member",
        status: "active",
        nickname: "MB",
        first_name: "Marta",
        last_name: "B",
        email: "marta@example.com",
        avatar_path: "",
      },
    ];
    const mounted = await mountComponent(IssueAssigneeSelect, {
      modelValue: "",
      users,
      size: "sm",
    });

    mounted.el.querySelector<HTMLButtonElement>(".meta-select-trigger")?.click();
    await nextTick();

    const optionLabels = Array.from(document.querySelectorAll(".meta-select-dropdown--teleported .ms-option"))
      .map((el) => el.textContent?.trim());
    expect(optionLabels.filter((label) => label === "Unassigned")).toHaveLength(1);

    await mounted.unmount();
  });
});
