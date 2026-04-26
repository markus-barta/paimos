import { describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import UndoConflictModal from "@/components/undo/UndoConflictModal.vue";
import { mountComponent } from "@/components/ai/testMount";

describe("UndoConflictModal", () => {
  it("preselects conservative defaults and emits resolution payload", async () => {
    const apply = vi.fn();
    const mounted = await mountComponent(UndoConflictModal, {
      onApply: apply,
      conflict: {
        status: "conflict",
        log_id: 7,
        request_id: "req-1",
        mode: "undo",
        mutation_type: "issue.update",
        conflicts: [
          {
            pattern: "field-changed-by-other",
            field: "status",
            their_value: "qa",
            current_value: "qa",
            target_value: "backlog",
            options: [
              { id: "overwrite", label: "Use my target value", default: true },
              {
                id: "keep_theirs",
                label: "Keep the newer value",
                default: false,
              },
            ],
          },
        ],
        cascading_blockers: [
          {
            pattern: "parent-deleted",
            target_id: 42,
            description: "Parent missing.",
            options: [
              {
                id: "orphan",
                label: "Make this issue top-level",
                default: true,
              },
              { id: "cancel", label: "Cancel", default: false },
            ],
          },
        ],
      },
    });

    const applyButton = document.body.querySelector(
      ".btn-primary",
    ) as HTMLButtonElement | null;
    expect(applyButton).toBeTruthy();

    applyButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await nextTick();

    expect(apply).toHaveBeenCalledWith({
      field_choices: { status: "overwrite" },
      cascade_choices: { "parent-deleted": "orphan" },
    });

    await mounted.unmount();
  });
});
