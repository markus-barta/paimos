/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-339. Thin service layer over the convenience endpoints PAI-338
// shipped under /api/projects/:id/{memory|runbooks|external-systems|
// related-projects|guidelines}. The shape is identical across the
// five sibling endpoints — only the URL alias differs — so this file
// keeps a single helper per HTTP verb that takes the alias as a
// discriminator, matching the dispatcher pattern on the backend
// (RouteByPath in handlers/knowledge/dispatcher.go).
//
// PAI-353 is in flight in parallel and consolidates the backend write
// paths through UpdateIssue so knowledge edits inherit history +
// mutation_log + attribution. The frontend talks to the convenience
// endpoints regardless — the change is server-side only.

import { api } from "@/api/client";
import type {
  KnowledgeCategory,
  KnowledgeEntry,
  KnowledgeEntryInput,
} from "@/types";
import { KNOWLEDGE_PATH_ALIAS } from "@/types";

// Mirrors the backend's slug constraint (handlers/knowledge/module.go).
// Echoed in the UI so users see errors before submit; the server is
// still the source of truth for uniqueness (409 on collision).
const SLUG_PATTERN = /^[a-z][a-z0-9_-]*$/;
const SLUG_MAX_LEN = 64;

export function validateKnowledgeSlug(slug: string): string {
  const trimmed = slug.trim();
  if (!trimmed) return "Slug is required.";
  if (trimmed.length > SLUG_MAX_LEN) return `Max ${SLUG_MAX_LEN} characters.`;
  if (!SLUG_PATTERN.test(trimmed))
    return "Lowercase letters, digits, _ or -; must start with a letter.";
  return "";
}

/** Auto-suggest a slug from a free-text title. Best-effort — caller
 * should still run validateKnowledgeSlug on the result, or surface
 * the validation error. Diacritics are stripped via NFKD; everything
 * else collapses to `_`. */
export function suggestSlug(title: string): string {
  const normalized = title
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .replace(/_+/g, "_");
  // Slugs must start with a letter — prefix `m_` if the cleaned form
  // begins with a digit / dash / underscore.
  const head = normalized.charAt(0);
  if (!head || /[0-9_-]/.test(head)) return ("m_" + normalized).slice(0, SLUG_MAX_LEN);
  return normalized.slice(0, SLUG_MAX_LEN);
}

function aliasFor(category: KnowledgeCategory): string {
  return KNOWLEDGE_PATH_ALIAS[category];
}

export function listKnowledgeEntries(
  projectId: number,
  category: KnowledgeCategory,
): Promise<KnowledgeEntry[]> {
  return api.get<KnowledgeEntry[]>(
    `/projects/${projectId}/${aliasFor(category)}`,
  );
}

export function getKnowledgeEntry(
  projectId: number,
  category: KnowledgeCategory,
  slug: string,
): Promise<KnowledgeEntry> {
  return api.get<KnowledgeEntry>(
    `/projects/${projectId}/${aliasFor(category)}/${encodeURIComponent(slug)}`,
  );
}

export function createKnowledgeEntry(
  projectId: number,
  category: KnowledgeCategory,
  payload: KnowledgeEntryInput,
): Promise<KnowledgeEntry> {
  return api.post<KnowledgeEntry>(
    `/projects/${projectId}/${aliasFor(category)}`,
    payload,
  );
}

export function updateKnowledgeEntry(
  projectId: number,
  category: KnowledgeCategory,
  currentSlug: string,
  payload: KnowledgeEntryInput,
): Promise<KnowledgeEntry> {
  return api.put<KnowledgeEntry>(
    `/projects/${projectId}/${aliasFor(category)}/${encodeURIComponent(currentSlug)}`,
    payload,
  );
}

export function deleteKnowledgeEntry(
  projectId: number,
  category: KnowledgeCategory,
  slug: string,
): Promise<void> {
  return api.delete(
    `/projects/${projectId}/${aliasFor(category)}/${encodeURIComponent(slug)}`,
  );
}

// ── PAI-345: cross-scope promotion ─────────────────────────────────

// promoteMemory POSTs to /api/memory/:slug/promote so a project /
// user / instance memory entry can be lifted to a higher scope. The
// server creates a new row at the destination and soft-deletes the
// source (history + tags + body preserved) — see knowledge_promote.go.
export type MemoryScope = "project" | "user" | "instance";

export interface PromoteMemoryRequest {
  to: MemoryScope;
  from_project_id?: number;
  to_project_id?: number;
}

export interface PromoteMemoryResponse {
  ok: boolean;
  from_scope: MemoryScope;
  to_scope: MemoryScope;
  entry: KnowledgeEntry;
}

export function promoteMemory(
  slug: string,
  payload: PromoteMemoryRequest,
): Promise<PromoteMemoryResponse> {
  return api.post<PromoteMemoryResponse>(
    `/memory/${encodeURIComponent(slug)}/promote`,
    payload,
  );
}

// ── PAI-347 — decay-based archive proposals ───────────────────────

export interface StaleMemoryProposal extends KnowledgeEntry {
  confidence: string;
  reference_count: number;
  last_referenced_at?: string;
  days_since_reference: number;
}

/**
 * Fetch the project's stale-memory archive proposals. Three server-
 * side conditions: no recent reference + confidence ≤ medium + no
 * in-flight originating ticket.
 */
export function listStaleMemory(
  projectId: number,
  days?: number,
): Promise<StaleMemoryProposal[]> {
  const qs = days && days > 0 ? `?days=${days}` : "";
  return api.get<StaleMemoryProposal[]>(
    `/projects/${projectId}/memory/stale${qs}`,
  );
}

/**
 * Reset the decay clock for a list of memory ids. Bumps the same
 * reference_count counter the bundle resolver bumps.
 */
export function bumpMemoryReferences(
  projectId: number,
  memoryIds: number[],
  source = "ui",
): Promise<{ updated: number }> {
  return api.post<{ updated: number }>(
    `/projects/${projectId}/memory/references`,
    { ids: memoryIds, source },
  );
}

// ── derived state helpers ─────────────────────────────────────────

// Status mapping per PAI-346 §"Status values": knowledge entries
// reuse the existing issue status enum but the UI renders them as
// "active" / "archived" / "proposed". Anything except `cancelled` /
// `proposed` is considered active. Centralised here so every component
// reads + writes the same labels.
const ARCHIVED_STATUS = "cancelled";
const ACTIVE_STATUS = "backlog";
// PAI-349 — bot-authored memory drafts pending operator review.
const PROPOSED_STATUS = "proposed";

export function isArchived(entry: KnowledgeEntry): boolean {
  return entry.status === ARCHIVED_STATUS;
}

// PAI-349 — surfaces the "Proposed" filter chip + accept/edit/reject
// flow. Only memory entries can be in this state for v1.
export function isProposed(entry: KnowledgeEntry): boolean {
  return entry.status === PROPOSED_STATUS;
}

export function archivedStatusValue(): string {
  return ARCHIVED_STATUS;
}

export function activeStatusValue(): string {
  return ACTIVE_STATUS;
}

export function proposedStatusValue(): string {
  return PROPOSED_STATUS;
}

// ── PAI-349 — proposed memory drafts ──────────────────────────────

/**
 * Accept a proposed memory entry — flips status to active. Equivalent
 * to PUTting status='backlog' on the existing knowledge endpoint.
 */
export async function acceptProposedMemory(
  projectId: number,
  entry: KnowledgeEntry,
): Promise<KnowledgeEntry> {
  return updateKnowledgeEntry(projectId, "memory", entry.slug, {
    slug: entry.slug,
    title: entry.title,
    body: entry.body,
    status: ACTIVE_STATUS,
    metadata: entry.metadata,
  });
}

/**
 * Reject a proposed memory entry — sets status to archived and stamps
 * `category_metadata.archived_reason='rejected'` so reviewers can
 * filter / audit later.
 */
export async function rejectProposedMemory(
  projectId: number,
  entry: KnowledgeEntry,
): Promise<KnowledgeEntry> {
  const meta = { ...(entry.metadata ?? {}) } as Record<string, unknown>;
  meta["archived_reason"] = "rejected";
  return updateKnowledgeEntry(projectId, "memory", entry.slug, {
    slug: entry.slug,
    title: entry.title,
    body: entry.body,
    status: ARCHIVED_STATUS,
    metadata: meta,
  });
}

/** Stale proposed memory drafts (untouched ≥ N days). */
export interface StaleProposedMemoryProposal extends KnowledgeEntry {
  days_since_update: number;
}

/**
 * Fetch the project's stale proposed-memory drafts. Server returns
 * candidates only — the operator drives the actual archive transition.
 */
export function listStaleProposedMemory(
  projectId: number,
  days?: number,
): Promise<StaleProposedMemoryProposal[]> {
  const qs = days && days > 0 ? `?days=${days}` : "";
  return api.get<StaleProposedMemoryProposal[]>(
    `/projects/${projectId}/memory/proposed/stale${qs}`,
  );
}
