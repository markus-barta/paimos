import { api } from "@/api/client";

export interface UndoConflictOption {
  id: string;
  label: string;
  default: boolean;
}

export interface UndoFieldConflict {
  pattern: string;
  field: string;
  their_value: unknown;
  target_value: unknown;
  current_value: unknown;
  options: UndoConflictOption[];
}

export interface UndoCascadeBlocker {
  pattern: string;
  target_id?: number;
  description: string;
  options: UndoConflictOption[];
}

export interface UndoConflictResponse {
  status: "conflict";
  log_id: number;
  request_id: string;
  mode: "undo" | "redo";
  mutation_type: string;
  conflicts: UndoFieldConflict[];
  cascading_blockers: UndoCascadeBlocker[];
}

export interface MutationActivityRow {
  id: number;
  request_id: string;
  mutation_type: string;
  subject_type: string;
  subject_id: number;
  subject_label: string;
  summary: string;
  undoable: boolean;
  on_user_stack: boolean;
  redoable: boolean;
  undone: boolean;
  created_at: string;
}

export interface MutationActivityResponse {
  undo_rows: MutationActivityRow[];
  redo_rows: MutationActivityRow[];
  history_rows: MutationActivityRow[];
  stack_depth: number;
}

export interface UndoResolutionPayload {
  field_choices: Record<string, string>;
  cascade_choices: Record<string, string>;
}

export function loadUndoActivity(): Promise<MutationActivityResponse> {
  return api.get("/undo/activity");
}

export function loadIssueActivity(
  issueId: number,
): Promise<MutationActivityResponse> {
  return api.get(`/issues/${issueId}/activity`);
}

export function undoByLogId(
  logId: number,
): Promise<{
  undone?: boolean;
  applied?: boolean;
  log_id: number;
  request_id: string;
}> {
  return api.post(`/undo/${logId}`, {});
}

export function redoByLogId(
  logId: number,
): Promise<{
  redone?: boolean;
  applied?: boolean;
  log_id: number;
  request_id: string;
}> {
  return api.post(`/redo/${logId}`, {});
}

export function undoByRequestId(
  requestId: string,
): Promise<{
  undone?: boolean;
  applied?: boolean;
  log_id: number;
  request_id: string;
}> {
  return api.post(`/undo/request/${encodeURIComponent(requestId)}`, {});
}

export function redoByRequestId(
  requestId: string,
): Promise<{
  redone?: boolean;
  applied?: boolean;
  log_id: number;
  request_id: string;
}> {
  return api.post(`/redo/request/${encodeURIComponent(requestId)}`, {});
}

export function resolveUndo(
  logId: number,
  payload: UndoResolutionPayload,
): Promise<{
  applied?: boolean;
  resolved?: boolean;
  log_id: number;
  request_id: string;
}> {
  return api.post(`/undo/${logId}/resolve`, payload);
}

export function resolveRedo(
  logId: number,
  payload: UndoResolutionPayload,
): Promise<{
  applied?: boolean;
  resolved?: boolean;
  log_id: number;
  request_id: string;
}> {
  return api.post(`/redo/${logId}/resolve`, payload);
}
