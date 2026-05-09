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

// ── PAI-347 — decay-based archive proposals ───────────────────────

export interface StaleMemoryProposal extends KnowledgeEntry {
  confidence: string;
  reference_count: number;
  last_referenced_at?: string;
  days_since_reference: number;
}

/**
 * Fetch the project's stale-memory archive proposals. The server
 * applies the three conditions (no recent reference + confidence ≤
 * medium + no in-flight originating ticket); the UI is responsible
 * only for rendering + allowing the user to one-click archive or
 * "still relevant" reset.
 *
 * `days` defaults to 90 (server default). Pass a different value
 * to widen / narrow the window from a Knowledge tab control.
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
 * Reset the decay clock for a list of memory ids. Powers the
 * "still relevant" UI button — bumps the same reference_count
 * counter the bundle resolver bumps so the entry won't show up
 * in the next stale list. Cross-project ids are silently dropped
 * server-side.
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
// "active" / "archived". Anything except `cancelled` is considered
// active for v1 (proposed lands with PAI-349). Centralised here so
// every component reads + writes the same labels.
const ARCHIVED_STATUS = "cancelled";
const ACTIVE_STATUS = "backlog";

export function isArchived(entry: KnowledgeEntry): boolean {
  return entry.status === ARCHIVED_STATUS;
}

export function archivedStatusValue(): string {
  return ARCHIVED_STATUS;
}

export function activeStatusValue(): string {
  return ACTIVE_STATUS;
}
