import { afterEach, describe, expect, it, vi } from "vitest";
import IssueTextEditField, {
  type IssueTextUploadJob,
} from "@/components/issue/IssueTextEditField.vue";
import { mountComponent } from "@/components/ai/testMount";

vi.mock("@/components/AppIcon.vue", () => ({
  default: {
    props: ["name"],
    template: "<span class=\"icon-stub\" :data-icon=\"name\"></span>",
  },
}));

vi.mock("@/components/ai/AiActionMenu.vue", () => ({
  default: {
    props: ["onAccept"],
    template:
      "<button class=\"ai-action-stub\" type=\"button\" @click=\"onAccept('rewritten')\">AI</button>",
  },
}));

vi.mock("@/components/ai/AiSurfaceFeedback.vue", () => ({
  default: {
    props: ["hostKey"],
    template: "<div class=\"feedback-stub\">{{ hostKey }}</div>",
  },
}));

function pasteEventWithFiles(files: File[]) {
  const event = new Event("paste", {
    bubbles: true,
    cancelable: true,
  }) as ClipboardEvent;
  Object.defineProperty(event, "clipboardData", {
    value: { files },
  });
  return event;
}

describe("IssueTextEditField", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("emits model updates and file paste uploads with cursor position", async () => {
    const update = vi.fn();
    const upload = vi.fn();
    const accept = vi.fn();
    const mounted = await mountComponent(IssueTextEditField, {
      modelValue: "hello",
      label: "Description",
      field: "description",
      hostKey: "issue-detail:7:description",
      issueId: 7,
      enableUploads: true,
      attachmentsEnabled: true,
      jobs: [],
      apply: vi.fn(),
      onAccept: accept,
      "onUpdate:modelValue": update,
      onUploadFiles: upload,
    });

    const textarea = mounted.el.querySelector("textarea") as HTMLTextAreaElement;
    textarea.value = "changed";
    textarea.dispatchEvent(new Event("input", { bubbles: true }));

    textarea.setSelectionRange(2, 2);
    const file = new File(["body"], "diagram.png", { type: "image/png" });
    const paste = pasteEventWithFiles([file]);
    textarea.dispatchEvent(paste);

    mounted.el
      .querySelector<HTMLButtonElement>(".ai-action-stub")
      ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));

    expect(update).toHaveBeenCalledWith("changed");
    expect(paste.defaultPrevented).toBe(true);
    expect(upload).toHaveBeenCalledWith([file], 2);
    expect(accept).toHaveBeenCalledWith("rewritten");
    await mounted.unmount();
  });

  it("does not hijack file paste when attachment storage is disabled", async () => {
    const upload = vi.fn();
    const mounted = await mountComponent(IssueTextEditField, {
      modelValue: "hello",
      label: "Description",
      field: "description",
      hostKey: "issue-detail:7:description",
      issueId: 7,
      enableUploads: true,
      attachmentsEnabled: false,
      jobs: [],
      apply: vi.fn(),
      onAccept: vi.fn(),
      "onUpdate:modelValue": vi.fn(),
      onUploadFiles: upload,
    });

    const textarea = mounted.el.querySelector("textarea") as HTMLTextAreaElement;
    const paste = pasteEventWithFiles([
      new File(["body"], "diagram.png", { type: "image/png" }),
    ]);
    textarea.dispatchEvent(paste);

    expect(paste.defaultPrevented).toBe(false);
    expect(upload).not.toHaveBeenCalled();
    await mounted.unmount();
  });

  it("emits retry and dismiss actions for failed upload jobs", async () => {
    const job: IssueTextUploadJob = {
      seq: 1,
      field: "description",
      filename: "broken.png",
      file: new File(["body"], "broken.png", { type: "image/png" }),
      isImage: true,
      progress: 0,
      status: "failed",
      error: "network failed",
      insertAt: 0,
    };
    const retry = vi.fn();
    const dismiss = vi.fn();
    const mounted = await mountComponent(IssueTextEditField, {
      modelValue: "hello",
      label: "Description",
      field: "description",
      hostKey: "issue-detail:7:description",
      issueId: 7,
      jobs: [job],
      apply: vi.fn(),
      onAccept: vi.fn(),
      "onUpdate:modelValue": vi.fn(),
      onRetryJob: retry,
      onDismissJob: dismiss,
    });

    mounted.el
      .querySelector<HTMLButtonElement>('button[title="Retry upload"]')
      ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    mounted.el
      .querySelector<HTMLButtonElement>('button[title="Dismiss"]')
      ?.dispatchEvent(new MouseEvent("click", { bubbles: true }));

    expect(retry).toHaveBeenCalledWith(job);
    expect(dismiss).toHaveBeenCalledWith(job);
    await mounted.unmount();
  });
});
