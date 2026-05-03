<script setup lang="ts">
import LoadingText from "@/components/LoadingText.vue";
import { ref, computed, onMounted, watch, nextTick } from "vue";
import {
  useRoute,
  useRouter,
  RouterLink,
  onBeforeRouteLeave,
} from "vue-router";
import { useAuthStore } from "@/stores/auth";
import IssueList from "@/components/IssueList.vue";
import AppIcon from "@/components/AppIcon.vue";
import StatusDot from "@/components/StatusDot.vue";
import { useDirtyGuard } from "@/composables/useDirtyGuard";
import { useConfirm } from "@/composables/useConfirm";
import { useMarkdown } from "@/composables/useMarkdown";
import { useTimeUnit } from "@/composables/useTimeUnit";
import { ApiError, api, errMsg } from "@/api/client";
import { isDevFixtureUser } from "@/utils/devUsers";
import { attachmentsEnabled } from "@/api/instance";
import { useNewIssueStore } from "@/stores/newIssue";
import { provideIssueContext } from "@/composables/useIssueContext";
import {
  emptyIssueDetailForm,
  issueToDetailForm,
} from "@/config/issueDetailForm";
import type { Issue, Tag, Project, Sprint, User, Attachment } from "@/types";
import {
  addIssueTag,
  assignIssueSprint,
  cloneIssueDetail,
  deleteIssueDetail,
  loadIssueAggregation,
  loadIssueDetailData,
  loadIssueParent,
  removeIssueSprint,
  removeIssueTag,
  saveIssueDetail,
  type IssueAggregation as Aggregation,
} from "@/services/issueDetail";
import { uploadInlineIssueAttachment } from "@/services/issueInlineAttachments";
import { addIssueRelation } from "@/services/issueRelations";
import {
  useIssueDisplay,
  TYPE_SVGS,
  STATUS_LABEL,
  PRIORITY_ICON,
  PRIORITY_COLOR,
  PRIORITY_LABEL,
} from "@/composables/useIssueDisplay";
import AiActionMenu from "@/components/ai/AiActionMenu.vue";
import AiSurfaceFeedback from "@/components/ai/AiSurfaceFeedback.vue";
import {
  aiMutationHeaders,
  applyIssueTextMutations,
  type AiApplyInfo,
} from "@/services/aiActionApply";
import { undoMutationByRequestId } from "@/services/aiPaperTrail";
import { useUndoStore } from "@/stores/undo";

// Sub-components
import IssueTimeEntries from "@/components/issue/IssueTimeEntries.vue";
import IssueHistory from "@/components/issue/IssueHistory.vue";
import IssueRelations from "@/components/issue/IssueRelations.vue";
import IssueAttachments from "@/components/issue/IssueAttachments.vue";
import IssueComments from "@/components/issue/IssueComments.vue";
import IssueAnchors from "@/components/issue/IssueAnchors.vue";
import IssueGroupMembers from "@/components/issue/IssueGroupMembers.vue";
import IssueMetaGrid from "@/components/issue/IssueMetaGrid.vue";
import IssueEditSidebar from "@/components/issue/IssueEditSidebar.vue";
import IssueBillingSummary from "@/components/issue/IssueBillingSummary.vue";
import IssueCompleteEpicModal from "@/components/issue/IssueCompleteEpicModal.vue";
import IssueDetailBody from "@/components/issue/IssueDetailBody.vue";
import IssueDetailFooter from "@/components/issue/IssueDetailFooter.vue";
import IssueTextEditField from "@/components/issue/IssueTextEditField.vue";

const route = useRoute();
const router = useRouter();
const undoStore = useUndoStore();
const { confirm } = useConfirm();

const ISSUE_KEY_PATTERN = /^[A-Z][A-Z0-9]{0,15}-\d+$/;

type IssueRouteRef =
  | { ok: true; ref: string }
  | { ok: false; message: string };

function firstRouteParam(value: unknown): string {
  const raw = Array.isArray(value) ? value[0] : value;
  return String(raw ?? "").trim();
}

function parseProjectId(value: unknown): number | null {
  const raw = firstRouteParam(value);
  if (!raw) return null;
  const id = Number(raw);
  return Number.isInteger(id) && id > 0 ? id : null;
}

function parseIssueRef(value: unknown): IssueRouteRef {
  const raw = firstRouteParam(value);
  if (!raw) {
    return { ok: false, message: "Issue link is missing an issue ID." };
  }
  if (/^\d+$/.test(raw) && Number(raw) > 0) return { ok: true, ref: raw };
  if (ISSUE_KEY_PATTERN.test(raw)) return { ok: true, ref: raw };
  return {
    ok: false,
    message: `Issue links must use a positive numeric ID or an uppercase issue key like PAI-265. "${raw}" is not valid.`,
  };
}

const issueId = ref(0);
const projectId = ref<number | null>(parseProjectId(route.params.id));

const issue = ref<Issue | null>(null);
const project = ref<Project | null>(null);
const parentIssue = ref<Issue | null>(null);
const children = ref<Issue[]>([]);
const projectIssues = ref<Issue[]>([]);
const users = ref<User[]>([]);
const allTags = ref<Tag[]>([]);
const allSprints = ref<Sprint[]>([]);
const costUnits = ref<string[]>([]);
const releases = ref<string[]>([]);

provideIssueContext({
  users,
  allTags,
  costUnits,
  releases,
  projects: ref([]),
  sprints: allSprints,
});

const loading = ref(true);
const loadError = ref("");
const loadErrorTitle = ref("");
const loadErrorRetryable = ref(false);
const editing = ref(false);
const saving = ref(false);
const saveError = ref("");

const issueTagIds = computed(() => issue.value?.tags?.map((t) => t.id) ?? []);

const form = ref(emptyIssueDetailForm());

// Sub-component refs
const timeEntriesRef = ref<InstanceType<typeof IssueTimeEntries> | null>(null);
const historyRef = ref<InstanceType<typeof IssueHistory> | null>(null);
const relationsRef = ref<InstanceType<typeof IssueRelations> | null>(null);
const attachmentsRef = ref<InstanceType<typeof IssueAttachments> | null>(null);
const commentsRef = ref<InstanceType<typeof IssueComments> | null>(null);
const groupMembersRef = ref<InstanceType<typeof IssueGroupMembers> | null>(
  null,
);

function clearIssueState() {
  issue.value = null;
  project.value = null;
  parentIssue.value = null;
  children.value = [];
  projectIssues.value = [];
  costUnits.value = [];
  releases.value = [];
  aggregation.value = null;
}

function setLoadError(title: string, message: string, retryable: boolean) {
  loadErrorTitle.value = title;
  loadError.value = message;
  loadErrorRetryable.value = retryable;
  clearIssueState();
  issueId.value = 0;
}

function setLoadErrorFromUnknown(e: unknown) {
  if (e instanceof ApiError) {
    if (e.status === 400) {
      setLoadError(
        "Invalid issue link",
        errMsg(e, "Invalid issue ID."),
        false,
      );
      return;
    }
    if (e.status === 404) {
      setLoadError(
        "Issue not found",
        "This issue does not exist, was deleted, or is not visible to your account.",
        true,
      );
      return;
    }
    if (e.status === 401) {
      setLoadError(
        "Sign in required",
        "Your session is no longer active. Sign in again to open this issue.",
        true,
      );
      return;
    }
    if (e.status === 0) {
      setLoadError("Issue did not load", errMsg(e, "Network error."), true);
      return;
    }
  }
  setLoadError(
    "Issue did not load",
    errMsg(e, "Issue could not be loaded."),
    true,
  );
}

// Reload when the route target changes.
watch(
  () => [route.params.issueId, route.params.id],
  () => load(),
);

let loadSeq = 0;
async function load() {
  const seq = ++loadSeq;
  const parsed = parseIssueRef(route.params.issueId);
  projectId.value = parseProjectId(route.params.id);
  loading.value = true;
  editing.value = false;
  saveError.value = "";
  loadError.value = "";
  loadErrorTitle.value = "";
  loadErrorRetryable.value = false;

  if (!parsed.ok) {
    setLoadError("Invalid issue link", parsed.message, false);
    loading.value = false;
    return;
  }

  let loaded = false;
  try {
    const data = await loadIssueDetailData(parsed.ref, projectId.value);
    if (seq !== loadSeq) return;
    issueId.value = data.issue.id;
    projectId.value = data.issue.project_id ?? projectId.value;
    issue.value = data.issue;
    project.value = data.project;
    parentIssue.value = data.parentIssue;
    children.value = data.children;
    projectIssues.value = data.projectIssues;
    // PAI-267: filter dev_* fixture users out of the assignee picker.
    users.value = data.users.filter((u) => !isDevFixtureUser(u.username));
    allTags.value = data.allTags;
    allSprints.value = data.allSprints;
    costUnits.value = data.costUnits;
    releases.value = data.releases;
    aggregation.value = null;
    resetForm();
    loaded = true;
  } catch (e: unknown) {
    if (seq !== loadSeq) return;
    setLoadErrorFromUnknown(e);
  } finally {
    if (seq === loadSeq) loading.value = false;
  }

  if (loaded && seq === loadSeq) {
    // Sub-components load their own data once the canonical numeric issue id is known.
    nextTick(() => {
      if (seq !== loadSeq) return;
      commentsRef.value?.load();
      relationsRef.value?.load();
      timeEntriesRef.value?.load();
      groupMembersRef.value?.load();
      attachmentsRef.value?.load();
      loadAggregation();
    });
  }
}

onMounted(async () => {
  await load();
  initMdModes();
  if (route.query.edit === "1" && issue.value) {
    editing.value = true;
    router.replace({ query: { ...route.query, edit: undefined } });
  }
});

function resetForm() {
  if (!issue.value) return;
  form.value = issueToDetailForm(issue.value);
}

// Dirty guard for unsaved changes
const detailSavedSnapshot = ref("");
const detailCurrentSnapshot = computed(() =>
  editing.value ? JSON.stringify(form.value) : "",
);
const { isDirty: isDetailDirty, reset: resetDetailDirty } = useDirtyGuard(
  detailCurrentSnapshot,
  detailSavedSnapshot,
);

onBeforeRouteLeave(async () => {
  if (pendingInlineUploads.value > 0) {
    return await confirm({
      message: `An attachment upload is still in progress (${pendingInlineUploads.value}). Leave anyway? Pending placeholders will be lost.`,
      confirmLabel: "Leave",
      danger: true,
    });
  }
  if (isDetailDirty.value) {
    return await confirm({
      message: "You have unsaved changes. Discard and leave?",
      confirmLabel: "Discard",
      danger: true,
    });
  }
});

function enterEditMode() {
  resetForm();
  editing.value = true;
  nextTick(() => {
    detailSavedSnapshot.value = JSON.stringify(form.value);
  });
}

async function save() {
  if (pendingInlineUploads.value > 0) {
    saveError.value = `Please wait — ${pendingInlineUploads.value} attachment upload${pendingInlineUploads.value > 1 ? "s" : ""} still in progress.`;
    return;
  }
  saveError.value = "";
  saving.value = true;
  try {
    issue.value = await saveIssueDetail(issueId.value, form.value);
    parentIssue.value = issue.value.parent_id
      ? await loadIssueParent(issue.value.parent_id)
      : null;
    editing.value = false;
    resetDetailDirty();
    const cu = issue.value.cost_unit?.trim();
    if (cu && !costUnits.value.includes(cu))
      costUnits.value = [...costUnits.value, cu].sort((a, b) =>
        a.localeCompare(b),
      );
    const rel = issue.value.release?.trim();
    if (rel && !releases.value.includes(rel))
      releases.value = [...releases.value, rel].sort((a, b) =>
        a.localeCompare(b),
      );
  } catch (e: unknown) {
    saveError.value = errMsg(e, "Save failed.");
  } finally {
    saving.value = false;
  }
}

async function deleteIssue() {
  if (saving.value) return;
  if (
    !(await confirm({
      message: `Delete ${issue.value?.issue_key} "${issue.value?.title}"?`,
      confirmLabel: "Delete",
      danger: true,
    }))
  )
    return;
  saving.value = true;
  try {
    await deleteIssueDetail(issueId.value);
    router.push(projectRoute.value);
  } finally {
    saving.value = false;
  }
}

// ── Clone ────────────────────────────────────────────────────────────────────
const cloning = ref(false);
async function cloneIssue() {
  if (cloning.value) return;
  cloning.value = true;
  try {
    const clone = await cloneIssueDetail(issueId.value);
    const pid = clone.project_id ?? effectiveProjectId.value;
    router.push(
      pid
        ? `/projects/${pid}/issues/${clone.id}?edit=1`
        : `/issues/${clone.id}?edit=1`,
    );
  } catch (e: unknown) {
    alert(errMsg(e, "Clone failed."));
  } finally {
    cloning.value = false;
  }
}

// ── Complete Epic ────────────────────────────────────────────────────────────
const completeEpicRef = ref<InstanceType<typeof IssueCompleteEpicModal> | null>(
  null,
);

function onEpicCompleted(updated: Issue, ch: Issue[]) {
  issue.value = updated;
  children.value = ch;
}

// ── Inline file paste/drop (ACME-1 / 581 / 583 / 584 / 585) ──────────────
const pendingAttachmentIds = ref<number[]>([]);
let pendingUploadSeq = 0;

type InlineField = "description" | "acceptance_criteria";
type UploadStatus = "pending" | "done" | "failed";

interface UploadJob {
  seq: number;
  field: InlineField;
  filename: string;
  file: File;
  isImage: boolean;
  progress: number;
  status: UploadStatus;
  error?: string;
  insertAt: number;
}

// Sidecar upload state — NOT mixed into the textarea. The textarea stays clean;
// the markdown link is only inserted when the upload resolves successfully.
const uploadJobs = ref<UploadJob[]>([]);

const pendingInlineUploads = computed(
  () => uploadJobs.value.filter((j) => j.status === "pending").length,
);
const avgUploadProgress = computed(() => {
  const active = uploadJobs.value.filter((j) => j.status === "pending");
  if (!active.length) return 0;
  return Math.round(active.reduce((s, j) => s + j.progress, 0) / active.length);
});
function jobsFor(field: InlineField): UploadJob[] {
  return uploadJobs.value.filter((j) => j.field === field);
}

// Escape characters that would break a markdown link's text segment.
function escapeLinkText(name: string): string {
  return name.replace(/[\[\]]/g, (m) => "\\" + m).replace(/[\r\n]+/g, " ");
}

function startUpload(job: UploadJob) {
  job.status = "pending";
  job.progress = 0;
  job.error = undefined;
  uploadInlineIssueAttachment(issue.value?.id ?? 0, job.file, (pct) => {
    job.progress = pct;
  })
    .then((a) => {
      const url = `/api/attachments/${a.id}`;
      const safeName = escapeLinkText(a.filename);
      const snippet = job.isImage
        ? `![${safeName}](${url})`
        : `[${safeName}](${url})`;

      // Insert at the saved cursor position, clamped to current text length.
      // Prefix a newline if we're not already on a fresh line, so successive
      // drops don't smash into each other or into existing prose.
      const text = form.value[job.field];
      const pos = Math.min(Math.max(job.insertAt, 0), text.length);
      const needsLeadingNL = pos > 0 && text[pos - 1] !== "\n";
      const needsTrailingNL = pos < text.length && text[pos] !== "\n";
      const inserted =
        (needsLeadingNL ? "\n" : "") + snippet + (needsTrailingNL ? "\n" : "");
      form.value[job.field] = text.slice(0, pos) + inserted + text.slice(pos);

      if (issue.value?.id) {
        attachmentsRef.value?.load();
      } else {
        pendingAttachmentIds.value.push(a.id);
      }

      job.status = "done";
      job.progress = 100;
      // Auto-dismiss success chips so the row doesn't pile up.
      setTimeout(() => {
        uploadJobs.value = uploadJobs.value.filter((j) => j !== job);
      }, 1500);
    })
    .catch((err: unknown) => {
      job.status = "failed";
      job.error = errMsg(err, "upload failed");
    });
}

function uploadInlineFiles(
  files: FileList | File[],
  modelField: InlineField,
  insertAt: number,
) {
  const list = Array.from(files);
  if (!list.length) return;

  const newJobs: UploadJob[] = list.map((file) => ({
    seq: ++pendingUploadSeq,
    field: modelField,
    filename: file.name,
    file,
    isImage: file.type.startsWith("image/"),
    progress: 0,
    status: "pending",
    insertAt,
  }));

  uploadJobs.value.push(...newJobs);
  for (const job of newJobs) startUpload(job);
}

function retryUpload(job: UploadJob) {
  startUpload(job);
}

function dismissJob(job: UploadJob) {
  uploadJobs.value = uploadJobs.value.filter((j) => j !== job);
}

async function addTag(tagId: number) {
  await addIssueTag(issueId.value, tagId);
  const tag = allTags.value.find((t) => t.id === tagId);
  if (tag && issue.value)
    issue.value = { ...issue.value, tags: [...(issue.value.tags ?? []), tag] };
}

async function removeTag(tagId: number) {
  if (!(await confirm({ message: "Remove this tag?", confirmLabel: "Remove" })))
    return;
  await removeIssueTag(issueId.value, tagId);
  if (issue.value)
    issue.value = {
      ...issue.value,
      tags: (issue.value.tags ?? []).filter((t) => t.id !== tagId),
    };
}

// ── Sprint assignment ────────────────────────────────────────────────────────
const sprintSearchQuery = ref("");
const sprintDropdownOpen = ref(false);
const sprintSearchRef = ref<HTMLInputElement | null>(null);
const sprintWrapperRef = ref<HTMLElement | null>(null);
const sprintDropdownPos = ref({ top: 0, left: 0 });

function onSprintOutsideClick(e: MouseEvent) {
  const target = e.target as Node;
  if (sprintWrapperRef.value && !sprintWrapperRef.value.contains(target)) {
    const dd = document.querySelector(".sprint-dropdown--teleported");
    if (dd && dd.contains(target)) return;
    sprintDropdownOpen.value = false;
  }
}
watch(sprintDropdownOpen, (open) => {
  if (open) document.addEventListener("mousedown", onSprintOutsideClick);
  else document.removeEventListener("mousedown", onSprintOutsideClick);
});

const assignedSprints = computed(() =>
  allSprints.value.filter((s) => issue.value?.sprint_ids?.includes(s.id)),
);

const availableSprintsFiltered = computed(() => {
  const assigned = issue.value?.sprint_ids ?? [];
  const q = sprintSearchQuery.value.toLowerCase();
  return allSprints.value
    .filter((s) => !assigned.includes(s.id))
    .filter((s) => !q || s.title.toLowerCase().includes(q))
    .slice(0, 20);
});

function toggleSprintDropdown() {
  sprintDropdownOpen.value = !sprintDropdownOpen.value;
  if (sprintDropdownOpen.value) {
    nextTick(() => {
      if (sprintWrapperRef.value) {
        const rect = sprintWrapperRef.value.getBoundingClientRect();
        sprintDropdownPos.value = { top: rect.bottom + 4, left: rect.left };
      }
      sprintSearchRef.value?.focus();
    });
  }
}

async function assignSprint(sprint: Sprint) {
  if (!issue.value) return;
  await assignIssueSprint(issueId.value, sprint.id);
  issue.value = {
    ...issue.value,
    sprint_ids: [...(issue.value.sprint_ids ?? []), sprint.id],
  };
  sprintDropdownOpen.value = false;
  sprintSearchQuery.value = "";
}

async function removeSprint(sprintId: number) {
  if (!issue.value) return;
  if (
    !(await confirm({
      message: "Remove sprint assignment?",
      confirmLabel: "Remove",
    }))
  )
    return;
  await removeIssueSprint(issueId.value, sprintId);
  issue.value = {
    ...issue.value,
    sprint_ids: (issue.value.sprint_ids ?? []).filter((id) => id !== sprintId),
  };
}

// IssueList ref
const childIssueListRef = ref<InstanceType<typeof IssueList> | null>(null);

const newIssueStore = useNewIssueStore();
watch(
  () => newIssueStore.trigger,
  () => {
    const ctx = newIssueStore.context;
    if (
      ctx.projectId !== undefined &&
      ctx.projectId !== effectiveProjectId.value
    )
      return;
    if (ctx.parentId !== undefined && ctx.parentId !== issueId.value) return;
    if (
      issue.value &&
      childLabel(issue.value.type) &&
      childIssueListRef.value
    ) {
      childIssueListRef.value.openCreate();
      return;
    }
  },
);

function onChildCreated(child: Issue) {
  children.value.push(child);
}
function onChildUpdated(child: Issue) {
  const idx = children.value.findIndex((c) => c.id === child.id);
  if (idx >= 0) children.value[idx] = child;
}
function onChildDeleted(id: number) {
  children.value = children.value.filter((c) => c.id !== id);
}

const { showTypeIcon, showTypeText } = useIssueDisplay();
const authStore = useAuthStore();
const effectiveProjectId = computed(
  () => project.value?.id ?? issue.value?.project_id ?? projectId.value,
);
const projectRoute = computed(() =>
  effectiveProjectId.value
    ? `/projects/${effectiveProjectId.value}`
    : "/issues",
);
const projectIssuesLabel = computed(() =>
  project.value?.key ? `${project.value.key} Issues` : "Issues",
);
// Per-project edit flag for the current user. Consumed by templates to
// hide edit affordances when the caller only has viewer access.
const canEditThisProject = computed(() => {
  return authStore.canEdit(effectiveProjectId.value);
});
const validParents = computed(() => {
  const currentId = issue.value?.id;
  const t = form.value.type;
  if (t === "epic") return [];
  if (t === "ticket")
    return projectIssues.value.filter(
      (i) => i.type === "epic" && i.id !== currentId,
    );
  if (t === "task")
    return projectIssues.value.filter(
      (i) => i.type === "ticket" && i.id !== currentId,
    );
  return projectIssues.value.filter(
    (i) => i.type === "epic" && i.id !== currentId,
  );
});

const typeChangeWarning = computed(() => {
  if (!issue.value || form.value.type === issue.value.type) return "";
  if (children.value.length > 0)
    return `This issue has ${children.value.length} child issue${children.value.length > 1 ? "s" : ""} — changing its type may break the hierarchy.`;
  return "";
});

const childLabel = (type: string) =>
  type === "epic" ? "Tickets" : type === "ticket" ? "Tasks" : null;

// ── Markdown / monospace preferences ─────────────────────────────────────────
const mdMode = ref(false);
function initMdModes() {
  mdMode.value = authStore.user?.markdown_default ?? false;
}
const isMonospace = computed(() => authStore.user?.monospace_fields ?? false);

const descriptionRef = computed(() => issue.value?.description ?? "");
const acRef = computed(() => issue.value?.acceptance_criteria ?? "");
const notesRef = computed(() => issue.value?.notes ?? "");
const { html: descHtml } = useMarkdown(descriptionRef, mdMode);
const { html: acHtml } = useMarkdown(acRef, mdMode);
const { html: notesHtml } = useMarkdown(notesRef, mdMode);

// PAI-146: AI text optimization. The composable manages availability,
// in-flight state, and the overlay slot; we just provide the per-field
// onAccept callback that writes the rewrite back into the form.
//
// `lastError`/`clearError` are destructured so they become top-level
// bindings — Vue auto-unwraps top-level refs in templates. Accessing
// the ref via `aiOptimize.lastError` would yield the Ref object
// (always truthy) in v-if, which kept the error banner permanently
// visible with empty content.
function onOptimizeAccept(
  field: "description" | "acceptance_criteria" | "notes",
) {
  return (text: string) => {
    form.value[field] = text;
  };
}

async function applyAiResult(info: AiApplyInfo) {
  if (info.action === "estimate_effort") {
    const hours = Number(info.values?.hours ?? (info.body as any)?.hours ?? 0);
    const lp = Number(info.values?.lp ?? (info.body as any)?.lp ?? 0);
    if (editing.value) {
      const prevHours = form.value.estimate_hours;
      const prevLp = form.value.estimate_lp;
      form.value.estimate_hours = hours;
      form.value.estimate_lp = lp;
      return {
        undoLabel: `Estimate ${hours}h / ${lp} LP applied`,
        undo: () => {
          form.value.estimate_hours = prevHours;
          form.value.estimate_lp = prevLp;
        },
      };
    }
    const prevHours = issue.value?.estimate_hours ?? null;
    const prevLp = issue.value?.estimate_lp ?? null;
    issue.value = await api.put<Issue>(
      `/issues/${issueId.value}`,
      { estimate_hours: hours, estimate_lp: lp },
      { headers: aiMutationHeaders(info) },
    );
    if (info.requestId) {
      undoStore.showSyntheticToast(
        {
          id: Date.now(),
          title: issue.value.issue_key,
          detail: `Estimate ${hours}h / ${lp} LP applied`,
        },
        "undo",
      );
      void undoStore.refresh();
    }
    return {
      undoLabel: `Estimate ${hours}h / ${lp} LP applied`,
      undo: async () => {
        if (info.requestId) {
          await undoMutationByRequestId(info.requestId);
          await load();
          return;
        }
        issue.value = await api.put<Issue>(`/issues/${issueId.value}`, {
          estimate_hours: prevHours,
          estimate_lp: prevLp,
        });
      },
      undoAutoDismissMs: 15000,
    };
  }
  if (info.action === "find_parent") {
    const issueKey = String(info.values?.issue_key ?? "");
    const parent = projectIssues.value.find((i) => i.issue_key === issueKey);
    if (!parent) return;
    if (editing.value) {
      const prevParent = form.value.parent_id;
      form.value.parent_id = parent.id;
      return {
        undoLabel: `Parent set to ${parent.issue_key}`,
        undo: () => {
          form.value.parent_id = prevParent;
        },
      };
    }
    const prevParent = issue.value?.parent_id ?? null;
    const prevParentIssue = parentIssue.value;
    issue.value = await api.put<Issue>(
      `/issues/${issueId.value}`,
      { parent_id: parent.id },
      { headers: aiMutationHeaders(info) },
    );
    parentIssue.value = parent;
    if (info.requestId) {
      undoStore.showSyntheticToast(
        {
          id: Date.now(),
          title: issue.value.issue_key,
          detail: `Parent set to ${parent.issue_key}`,
        },
        "undo",
      );
      void undoStore.refresh();
    }
    return {
      undoLabel: `Parent set to ${parent.issue_key}`,
      undo: async () => {
        if (info.requestId) {
          await undoMutationByRequestId(info.requestId);
          await load();
          return;
        }
        issue.value = await api.put<Issue>(`/issues/${issueId.value}`, {
          parent_id: prevParent,
        });
        parentIssue.value = prevParentIssue;
      },
      undoAutoDismissMs: 15000,
    };
  }
  if (info.action === "detect_duplicates") {
    const issueKey = String(info.values?.issue_key ?? "");
    const relationType = String(info.values?.relation_type ?? "related") as
      | "depends_on"
      | "impacts"
      | "follows_from"
      | "blocks"
      | "related";
    const requestId = info.requestId;
    const target = projectIssues.value.find((i) => i.issue_key === issueKey);
    if (!target) return;
    await addIssueRelation(issueId.value, target.id, relationType, {
      headers: aiMutationHeaders(info),
    });
    relationsRef.value?.load();
    if (requestId) {
      undoStore.showSyntheticToast(
        {
          id: Date.now(),
          title: issue.value?.issue_key ?? "Issue",
          detail: `${relationType.replace(/_/g, " ")} link to ${target.issue_key} added`,
        },
        "undo",
      );
      void undoStore.refresh();
    }
    return requestId
      ? {
          undoLabel: `${relationType.replace(/_/g, " ")} link to ${target.issue_key} added`,
          undo: async () => {
            await undoMutationByRequestId(requestId);
            relationsRef.value?.load();
          },
          undoAutoDismissMs: 15000,
        }
      : undefined;
  }
  if (info.action === "generate_subtasks") {
    if (!effectiveProjectId.value) return;
    const suggestions = (info.body as any)?.suggestions ?? [];
    const selected = info.selection?.length
      ? info.selection
      : suggestions.map((_: unknown, idx: number) => idx);
    const overrides = (info.values?.titleOverrides ?? {}) as Record<
      string,
      string
    >;
    for (const idx of selected) {
      const item = suggestions[idx];
      if (!item) continue;
      await api.post(`/projects/${effectiveProjectId.value}/issues`, {
        parent_id: issueId.value,
        title: overrides[idx] || item.title,
        description: item.description || "",
        type: item.type || "task",
        status: "backlog",
        priority: "medium",
      });
    }
    children.value = await api
      .get<Issue[]>(`/issues/${issueId.value}/children`)
      .catch(() => children.value);
    return;
  }
  if (editing.value) {
    const next = applyIssueTextMutations(info, {
      description: form.value.description,
      acceptance_criteria: form.value.acceptance_criteria,
      notes: form.value.notes,
    });
    form.value.description = next.description;
    form.value.acceptance_criteria = next.acceptance_criteria;
    form.value.notes = next.notes;
  }
}

// ── h/PT toggle + EUR calculations ───────────────────────────────────────────
const {
  unit: timeUnit,
  toggle: toggleTimeUnit,
  formatHours,
  label: timeLabel,
} = useTimeUnit();

const linkedBillingType = computed(() => {
  const i = issue.value;
  if (!i || !i.cost_unit) return null;
  if (i.type === "cost_unit" || i.type === "epic")
    return i.billing_type || null;
  const cu = projectIssues.value.find(
    (p) => p.type === "cost_unit" && p.title === i.cost_unit,
  );
  return cu?.billing_type || null;
});

function fmtDateTime(s: string): string {
  if (!s) return "—";
  const d = new Date(s.endsWith("Z") ? s : s + "Z");
  return isNaN(d.getTime())
    ? s
    : d.toLocaleString(undefined, {
        year: "numeric",
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      });
}

// ── Aggregation (cost_unit / epic) ──────────────────────────────────────────
const aggregation = ref<Aggregation | null>(null);
const aggLoading = ref(false);
const isCostUnitOrEpic = computed(
  () => issue.value?.type === "cost_unit" || issue.value?.type === "epic",
);

async function loadAggregation() {
  if (!issueId.value || !isCostUnitOrEpic.value) return;
  aggLoading.value = true;
  try {
    aggregation.value = await loadIssueAggregation(issueId.value);
  } catch {
    aggregation.value = null;
  } finally {
    aggLoading.value = false;
  }
}

const BILLING_LABEL: Record<string, string> = {
  time_and_material: "Time & Material",
  fixed_price: "Fixed Price",
  mixed: "Mixed",
};

// ── History overlay ──────────────────────────────────────────────────────────
const historyOpen = ref(false);
async function openHistory() {
  historyOpen.value = true;
  historyRef.value?.load();
}

async function cancelEdit() {
  if (pendingInlineUploads.value > 0) {
    const ok = await confirm({
      message: `An attachment upload is still in progress (${pendingInlineUploads.value}). Cancel anyway? Pending placeholders will be lost.`,
      confirmLabel: "Cancel edit",
      danger: true,
    });
    if (!ok) return;
  }
  editing.value = false;
  resetForm();
  saveError.value = "";
  resetDetailDirty();
}
</script>

<template>
  <LoadingText v-if="loading" class="loading" label="Loading…" />
  <div v-else-if="loadError" class="issue-load-state" role="alert">
    <Teleport defer to="#app-header-left">
      <RouterLink :to="projectRoute" class="ah-back">
        <AppIcon name="arrow-left" :size="13" />
        {{ projectIssuesLabel }}
      </RouterLink>
    </Teleport>
    <div class="issue-load-icon" aria-hidden="true">
      <AppIcon name="alert-circle" :size="20" />
    </div>
    <div class="issue-load-copy">
      <h1>{{ loadErrorTitle || "Issue did not load" }}</h1>
      <p>{{ loadError }}</p>
    </div>
    <div class="issue-load-actions">
      <button
        v-if="loadErrorRetryable"
        class="btn btn-primary"
        type="button"
        @click="load"
      >
        Retry
      </button>
      <RouterLink class="btn btn-ghost" :to="projectRoute">
        Back to issues
      </RouterLink>
    </div>
  </div>
  <template v-else-if="issue">
    <!-- Breadcrumb -->
    <Teleport defer to="#app-header-left">
      <RouterLink :to="projectRoute" class="ah-back">
        <AppIcon name="arrow-left" :size="13" />
        {{ projectIssuesLabel }}
      </RouterLink>
      <template v-if="parentIssue">
        <span class="ah-sep">/</span>
        <RouterLink
          :to="
            effectiveProjectId
              ? `/projects/${effectiveProjectId}/issues/${parentIssue.id}`
              : `/issues/${parentIssue.id}`
          "
          class="ah-crumb"
        >
          {{ parentIssue.issue_key }}
        </RouterLink>
      </template>
      <span class="ah-sep">/</span>
      <span class="ah-crumb ah-crumb--current">{{ issue.issue_key }}</span>
    </Teleport>

    <div class="issue-card">
      <!-- Header -->
      <div class="issue-header">
        <div class="issue-header-left">
          <div class="issue-subheader">
            <span class="issue-key-text">{{ issue.issue_key }}</span>
            <span class="subheader-sep">·</span>
            <span :class="`issue-type issue-type--${issue.type}`">
              <span
                v-if="showTypeIcon"
                v-html="TYPE_SVGS[issue.type] ?? ''"
              ></span>
              <span v-if="showTypeText" class="type-label-text">{{
                issue.type.charAt(0).toUpperCase() + issue.type.slice(1)
              }}</span>
            </span>
            <span class="subheader-sep">·</span>
            <span class="issue-status">
              <StatusDot :status="issue.status" />
              {{ STATUS_LABEL[issue.status] }}
            </span>
            <template v-if="issue.type !== 'sprint'">
              <span class="subheader-sep">·</span>
              <span
                class="issue-priority"
                :style="{ color: PRIORITY_COLOR[issue.priority] }"
              >
                <AppIcon
                  :name="PRIORITY_ICON[issue.priority]"
                  :size="11"
                  :stroke-width="2.5"
                />
                {{ PRIORITY_LABEL[issue.priority] }}
              </span>
            </template>
          </div>
          <h1 v-if="!editing" class="issue-title">{{ issue.title }}</h1>
          <input v-else v-model="form.title" class="title-input" type="text" />
        </div>

        <div class="issue-header-actions">
          <template v-if="!editing">
            <!-- PAI-179: issue-level AI menu — surfaces actions
                   that operate on the whole record (find parent,
                   generate sub-tasks, estimate effort, detect
                   duplicates). Sits next to the other header
                   buttons so admins discover it where they expect
                   issue-scoped controls. -->
            <AiActionMenu
              surface="issue"
              placement="issue"
              :host-key="`issue-detail:${issueId}:record`"
              field=""
              field-label="Issue"
              :issue-id="issueId"
              :text="() => issue?.title ?? ''"
              :on-accept="
                () => {
                  /* issue actions don't rewrite a single text field */
                }
              "
            />
            <button
              v-if="authStore.user?.role === 'admin'"
              class="btn btn-danger"
              @click="deleteIssue"
            >
              Delete
            </button>
            <button
              v-if="
                issue.type === 'epic' &&
                issue.status !== 'done' &&
                issue.status !== 'cancelled'
              "
              class="btn btn-ghost"
              @click="completeEpicRef?.show()"
            >
              Mark as Done
            </button>
            <button
              class="btn btn-ghost"
              :disabled="cloning"
              @click="cloneIssue"
            >
              <AppIcon name="copy" :size="13" /> Clone
            </button>
            <button class="btn btn-ghost" @click="enterEditMode">Edit</button>
            <button
              class="btn btn-ghost"
              @click="router.push(projectRoute)"
            >
              <AppIcon name="x" :size="13" /> Close
            </button>
          </template>
          <template v-else>
            <!-- PAI-179: same issue-level menu in edit mode. -->
            <AiActionMenu
              surface="issue"
              placement="issue"
              :host-key="`issue-detail:${issueId}:record`"
              field=""
              field-label="Issue"
              :issue-id="issueId"
              :text="() => issue?.title ?? ''"
              :on-accept="
                () => {
                  /* issue actions don't rewrite a single text field */
                }
              "
            />
            <button
              v-if="authStore.user?.role === 'admin'"
              class="btn btn-danger"
              @click="deleteIssue"
            >
              Delete
            </button>
            <button
              class="btn btn-ghost"
              :disabled="cloning"
              @click="cloneIssue"
            >
              <AppIcon name="copy" :size="13" /> Clone
            </button>
            <button class="btn btn-ghost" @click="cancelEdit">Cancel</button>
            <button
              class="btn btn-primary"
              :class="{ 'btn--uploading': pendingInlineUploads > 0 }"
              :style="
                pendingInlineUploads > 0
                  ? `--upload-progress:${avgUploadProgress}%`
                  : undefined
              "
              @click="save"
              :disabled="saving || pendingInlineUploads > 0"
            >
              {{
                pendingInlineUploads > 0
                  ? `Uploading ${pendingInlineUploads}…`
                  : saving
                    ? "Saving…"
                    : "Save"
              }}
            </button>
          </template>
        </div>
      </div>
      <AiSurfaceFeedback
        :host-key="`issue-detail:${issueId}:record`"
        :apply="applyAiResult"
      />

      <!-- Meta (view mode) -->
      <div class="meta-section">
        <IssueMetaGrid
          v-if="!editing"
          :issue="issue"
          :parent-issue="parentIssue"
          :project-id="effectiveProjectId"
          :assigned-sprints="assignedSprints"
          :all-sprints="allSprints"
          :billing-label="BILLING_LABEL"
          :linked-billing-type="linkedBillingType"
          :time-label="timeLabel"
          :format-hours="formatHours"
          :toggle-time-unit="toggleTimeUnit"
          v-model:md-mode="mdMode"
          @remove-sprint="removeSprint"
          @toggle-sprint-dropdown="toggleSprintDropdown"
        >
          <template #sprint-dropdown>
            <Teleport to="body">
              <div
                v-if="sprintDropdownOpen && !editing"
                class="sprint-dropdown sprint-dropdown--teleported"
                :style="{
                  top: sprintDropdownPos.top + 'px',
                  left: sprintDropdownPos.left + 'px',
                }"
              >
                <input
                  ref="sprintSearchRef"
                  v-model="sprintSearchQuery"
                  class="sprint-search"
                  placeholder="Search sprints…"
                  autocomplete="off"
                  @keydown.escape="sprintDropdownOpen = false"
                />
                <div class="sprint-list">
                  <div
                    v-if="!availableSprintsFiltered.length"
                    class="sprint-empty"
                  >
                    No sprints found
                  </div>
                  <button
                    v-for="s in availableSprintsFiltered"
                    :key="s.id"
                    class="sprint-opt"
                    type="button"
                    @click="assignSprint(s)"
                  >
                    <span class="sprint-opt-title">{{ s.title }}</span>
                    <span
                      v-if="s.sprint_state"
                      :class="[
                        'sprint-opt-state',
                        `sprint-opt-state--${s.sprint_state}`,
                      ]"
                      >{{ s.sprint_state }}</span
                    >
                    <span v-if="s.start_date" class="sprint-opt-dates">{{
                      s.start_date.slice(0, 10)
                    }}</span>
                  </button>
                </div>
              </div>
            </Teleport>
          </template>
        </IssueMetaGrid>
      </div>

      <!-- Billing Summary -->
      <IssueBillingSummary
        v-if="isCostUnitOrEpic && !editing && aggregation"
        :aggregation="aggregation"
        :time-label="timeLabel"
        :format-hours="formatHours"
        :toggle-time-unit="toggleTimeUnit"
      />

      <!-- Time Entries -->
      <IssueTimeEntries ref="timeEntriesRef" :issue-id="issueId" />

      <!-- Body (view mode) -->
      <IssueDetailBody
        v-if="!editing"
        :issue="issue"
        :desc-html="descHtml"
        :ac-html="acHtml"
        :notes-html="notesHtml"
        :is-monospace="isMonospace"
        :md-mode="mdMode"
      />

      <!-- Edit layout -->
      <div v-else class="edit-layout">
        <div class="edit-content">
          <IssueTextEditField
            v-model="form.description"
            label="Description"
            field="description"
            :host-key="`issue-detail:${issueId}:description`"
            :issue-id="issueId"
            placeholder="What needs to be done?"
            :is-monospace="isMonospace"
            :attachments-enabled="attachmentsEnabled"
            enable-uploads
            :jobs="jobsFor('description')"
            :apply="applyAiResult"
            :on-accept="onOptimizeAccept('description')"
            @upload-files="
              (files, insertAt) =>
                uploadInlineFiles(files, 'description', insertAt)
            "
            @retry-job="retryUpload"
            @dismiss-job="dismissJob"
          />
          <IssueTextEditField
            v-if="['epic', 'cost_unit', 'ticket'].includes(form.type)"
            v-model="form.acceptance_criteria"
            label="Acceptance Criteria"
            field="acceptance_criteria"
            :host-key="`issue-detail:${issueId}:acceptance_criteria`"
            :issue-id="issueId"
            placeholder="When is this done?"
            :is-monospace="isMonospace"
            :attachments-enabled="attachmentsEnabled"
            enable-uploads
            :jobs="jobsFor('acceptance_criteria')"
            :apply="applyAiResult"
            :on-accept="onOptimizeAccept('acceptance_criteria')"
            @upload-files="
              (files, insertAt) =>
                uploadInlineFiles(files, 'acceptance_criteria', insertAt)
            "
            @retry-job="retryUpload"
            @dismiss-job="dismissJob"
          />
          <IssueTextEditField
            v-model="form.notes"
            label="Notes"
            field="notes"
            :host-key="`issue-detail:${issueId}:notes`"
            :issue-id="issueId"
            placeholder="Additional context, links, etc."
            :rows="3"
            :is-monospace="isMonospace"
            :apply="applyAiResult"
            :on-accept="onOptimizeAccept('notes')"
          />
        </div>

        <IssueEditSidebar
          :form="form"
          :issue-type="issue.type"
          :cost-units="costUnits"
          :releases="releases"
          :all-tags="allTags"
          :issue-tag-ids="issueTagIds"
          :valid-parents="validParents"
          :users="users"
          :assigned-sprints="assignedSprints"
          :type-change-warning="typeChangeWarning"
          :linked-billing-type="linkedBillingType"
          :time-unit="timeUnit"
          :time-label="timeLabel"
          :toggle-time-unit="toggleTimeUnit"
          :saving="saving || pendingInlineUploads > 0"
          :save-error="saveError"
          @save="save"
          @cancel="cancelEdit"
          @add-tag="addTag"
          @remove-tag="removeTag"
          @remove-sprint="removeSprint"
          @toggle-sprint-dropdown="toggleSprintDropdown"
        >
          <template #sprint-dropdown>
            <Teleport to="body">
              <div
                v-if="sprintDropdownOpen && editing"
                class="sprint-dropdown sprint-dropdown--teleported"
                :style="{
                  top: sprintDropdownPos.top + 'px',
                  left: sprintDropdownPos.left + 'px',
                }"
              >
                <input
                  ref="sprintSearchRef"
                  v-model="sprintSearchQuery"
                  class="sprint-search"
                  placeholder="Search sprints…"
                  autocomplete="off"
                  @keydown.escape="sprintDropdownOpen = false"
                />
                <div class="sprint-list">
                  <div
                    v-if="!availableSprintsFiltered.length"
                    class="sprint-empty"
                  >
                    No sprints found
                  </div>
                  <button
                    v-for="s in availableSprintsFiltered"
                    :key="s.id"
                    class="sprint-opt"
                    type="button"
                    @click="assignSprint(s)"
                  >
                    <span class="sprint-opt-title">{{ s.title }}</span>
                    <span
                      v-if="s.sprint_state"
                      :class="[
                        'sprint-opt-state',
                        `sprint-opt-state--${s.sprint_state}`,
                      ]"
                      >{{ s.sprint_state }}</span
                    >
                    <span v-if="s.start_date" class="sprint-opt-dates">{{
                      s.start_date.slice(0, 10)
                    }}</span>
                  </button>
                </div>
              </div>
            </Teleport>
          </template>
        </IssueEditSidebar>
      </div>
    </div>

    <!-- Children section -->
    <div v-if="childLabel(issue.type)" class="children-section">
      <IssueList
        ref="childIssueListRef"
        :project-id="effectiveProjectId ?? undefined"
        :issues="children"
        :project-all-issues="projectIssues"
        :initial-type="issue.type === 'epic' ? 'ticket' : 'task'"
        :default-parent-id="issueId"
        compact
        :title="childLabel(issue.type)!"
        @created="onChildCreated"
        @updated="onChildUpdated"
        @deleted="onChildDeleted"
        @cost-unit-added="
          (v: string) => {
            if (!costUnits.includes(v))
              costUnits = [...costUnits, v].sort((a, b) => a.localeCompare(b));
          }
        "
        @release-added="
          (v: string) => {
            if (!releases.includes(v))
              releases = [...releases, v].sort((a, b) => a.localeCompare(b));
          }
        "
      />
    </div>

    <!-- Group / Sprint panel -->
    <IssueGroupMembers
      ref="groupMembersRef"
      :issue-id="issueId"
      :issue-type="issue.type"
      :project-id="effectiveProjectId"
    />

    <!-- Issue Relations -->
    <IssueRelations
      v-if="
        issue.type === 'ticket' ||
        issue.type === 'task' ||
        issue.type === 'epic'
      "
      ref="relationsRef"
      :issue-id="issueId"
      :project-id="effectiveProjectId"
      :project-issues="projectIssues"
    />

    <IssueAnchors
      v-if="
        issue.type === 'ticket' ||
        issue.type === 'task' ||
        issue.type === 'epic'
      "
      :issue-id="issueId"
    />

    <!-- Attachments -->
    <IssueAttachments ref="attachmentsRef" :issue-id="issueId" />

    <!-- Comments -->
    <IssueComments
      ref="commentsRef"
      :issue-id="issueId"
      :md-mode="mdMode"
      :is-monospace="isMonospace"
    />

    <!-- Footer -->
    <IssueDetailFooter
      :issue="issue"
      :format-date-time="fmtDateTime"
      @history="openHistory"
    />
  </template>

  <!-- History overlay -->
  <IssueHistory
    ref="historyRef"
    :issue-id="issueId"
    :open="historyOpen"
    @close="historyOpen = false"
  />

  <IssueCompleteEpicModal
    ref="completeEpicRef"
    :issue-id="issueId"
    :issue-key="issue?.issue_key ?? ''"
    :children="children"
    @completed="onEpicCompleted"
  />
</template>

<style scoped>
.loading {
  color: var(--text-muted);
  padding: 2rem 0;
}

.issue-load-state {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 1rem;
  padding: 1rem 1.25rem;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow);
}
.issue-load-icon {
  display: grid;
  place-items: center;
  width: 2.25rem;
  height: 2.25rem;
  border-radius: 8px;
  color: var(--danger, #b91c1c);
  background: color-mix(in srgb, currentColor 9%, transparent);
}
.issue-load-copy {
  min-width: 0;
}
.issue-load-copy h1 {
  margin: 0;
  color: var(--text);
  font-size: 15px;
  font-weight: 700;
  line-height: 1.25;
}
.issue-load-copy p {
  margin: 0.2rem 0 0;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.4;
}
.issue-load-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.issue-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow);
  overflow: visible;
}

.issue-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  padding: 1.25rem 1.5rem;
  border-bottom: 1px solid var(--border);
}
.issue-header-left {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  flex: 1;
  min-width: 0;
}
.issue-subheader {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  flex-wrap: wrap;
}
.issue-key-text {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.05em;
  font-family: "DM Mono", monospace;
  color: var(--text-muted);
  white-space: nowrap;
  flex-shrink: 0;
}
.subheader-sep {
  font-size: 11px;
  color: var(--border);
  user-select: none;
}
.type-label-text {
  font-size: 12px;
}
.issue-title {
  font-size: 18px;
  font-weight: 700;
  color: var(--text);
  line-height: 1.3;
}
.title-input {
  font-size: 16px;
  font-weight: 600;
  flex: 1;
  min-width: 200px;
}
.issue-header-actions {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  flex-shrink: 0;
  padding-top: 0.1rem;
}

.meta-section {
  padding: 0.9rem 1.5rem;
  border-bottom: 1px solid var(--border);
  background: var(--bg);
}

/* Sprint dropdown (teleported to body, not scoped) */
.sprint-dropdown {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  z-index: 300;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow-md);
  width: 220px;
  display: flex;
  flex-direction: column;
}
.sprint-dropdown--teleported {
  position: fixed;
  z-index: 9000;
}
.sprint-search {
  border: none;
  border-bottom: 1px solid var(--border);
  padding: 0.5rem 0.75rem;
  font-size: 13px;
  font-family: inherit;
  outline: none;
  background: transparent;
  color: var(--text);
  border-radius: 8px 8px 0 0;
}
.sprint-list {
  max-height: 220px;
  overflow-y: auto;
}
.sprint-empty {
  padding: 0.65rem 0.75rem;
  font-size: 13px;
  color: var(--text-muted);
}
.sprint-opt {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 0.45rem 0.75rem;
  font-size: 13px;
  background: none;
  border: none;
  cursor: pointer;
  font-family: inherit;
  color: var(--text);
  text-align: left;
  transition: background 0.1s;
}
.sprint-opt:hover {
  background: #f0f2f4;
}
.sprint-opt-title {
  font-weight: 500;
}
.sprint-opt-dates {
  font-size: 11px;
  color: var(--text-muted);
}
.sprint-opt-state {
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  border-radius: 3px;
  padding: 0 0.25rem;
}
.sprint-opt-state--active {
  background: #fff3e0;
  color: #b45309;
}
.sprint-opt-state--planned {
  background: #f3f4f6;
  color: #6b7280;
}
.sprint-opt-state--complete {
  background: #dcfce7;
  color: #166534;
}
.sprint-opt-state--archived {
  background: #e5e7eb;
  color: #6b7280;
}

/* Edit layout */
.edit-layout {
  display: grid;
  grid-template-columns: 1fr 280px;
  gap: 0;
  min-height: 0;
}
.edit-content {
  padding: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
  border-right: 1px solid var(--border);
}

.children-section {
  margin-top: 1.5rem;
}

/* Primary save button — progress bar along bottom edge while uploading */
.btn--uploading {
  position: relative;
  overflow: hidden;
}
.btn--uploading::after {
  content: "";
  position: absolute;
  left: 0;
  bottom: 0;
  height: 2px;
  width: var(--upload-progress, 0%);
  background: rgba(255, 255, 255, 0.85);
  transition: width 140ms linear;
  border-radius: 0 0 var(--radius) var(--radius);
}

@media (max-width: 640px) {
  .issue-load-state {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .issue-load-actions {
    grid-column: 1 / -1;
    justify-content: flex-start;
  }
}
</style>
