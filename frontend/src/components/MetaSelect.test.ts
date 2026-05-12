import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, nextTick, ref } from "vue";
import MetaSelect from "@/components/MetaSelect.vue";

vi.mock("@/components/AppIcon.vue", () => ({
  default: {
    props: ["name"],
    template: '<span class="icon-stub" :data-icon="name"></span>',
  },
}));

function mountParent(onParentClick: () => void, onUpdate: (value: string) => void) {
  const el = document.createElement("div");
  document.body.appendChild(el);
  const Parent = defineComponent({
    components: { MetaSelect },
    setup() {
      const value = ref("");
      const options = [
        { value: "backlog", label: "Backlog", dotColor: "#4b5563", dotOutline: true },
        { value: "qa", label: "QA", dotColor: "#a855f7" },
      ];
      function update(next: string) {
        value.value = next;
        onUpdate(next);
      }
      return { value, options, onParentClick, update };
    },
    template: `
      <div class="parent-row" @click="onParentClick">
        <MetaSelect :model-value="value" :options="options" @update:model-value="update" />
      </div>
    `,
  });
  const app = createApp(Parent);
  app.mount(el);
  return {
    el,
    unmount() {
      app.unmount();
      el.remove();
      document.querySelectorAll(".meta-select-dropdown--teleported").forEach((n) => n.remove());
    },
  };
}

describe("MetaSelect", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("does not bubble trigger clicks to parent row handlers", async () => {
    const parentClick = vi.fn();
    const update = vi.fn();
    const mounted = mountParent(parentClick, update);

    mounted.el.querySelector<HTMLButtonElement>(".meta-select-trigger")?.click();
    await nextTick();

    expect(parentClick).not.toHaveBeenCalled();
    expect(document.querySelector(".meta-select-dropdown--teleported")).toBeTruthy();

    mounted.unmount();
  });

  it("renders visual option state and emits selected values", async () => {
    const parentClick = vi.fn();
    const update = vi.fn();
    const mounted = mountParent(parentClick, update);

    mounted.el.querySelector<HTMLButtonElement>(".meta-select-trigger")?.click();
    await nextTick();
    document.querySelectorAll<HTMLButtonElement>(".ms-option")[1]?.click();
    await nextTick();

    expect(update).toHaveBeenCalledWith("qa");
    expect(parentClick).not.toHaveBeenCalled();

    mounted.unmount();
  });
});
