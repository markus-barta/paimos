/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-342 — thin service over the new GET /api/issues/:id/applicable-
// memories endpoint. The backing endpoint joins issue_relations to
// issues filtered by type='memory' so the manual-list path returns
// the curated set; passing `?suggest=1` switches to the v1 scoring
// path that surfaces up to 3 candidates the ticket isn't yet linked
// to (tag overlap, parent-epic body match, env overlap).
//
// Mutations reuse the existing /api/issues/:id/relations POST/DELETE
// with type='applies_to_memory' — see services/issueRelations.ts.

import { api } from "@/api/client";

export interface ApplicableMemory {
  id: number;
  project_id: number;
  project_key?: string;
  slug: string;
  title: string;
  /** First non-empty body line, capped at 160 chars. */
  preview?: string;
  /** Convenience: matches the ticket-key shape (e.g. "PAI-342") when
   * the memory lives in a project with a key + non-zero issue_number. */
  issue_key?: string;
  /** Suggest path only — final score after v1 rules. */
  score?: number;
  /** Suggest path only — list of human-readable rule hits, e.g.
   *  "tag:bug" / "parent:knowledge plane" / "env:prod". */
  matched?: string[];
}

/** GET /api/issues/:id/applicable-memories — manually-curated set,
 * ordered by slug for stable rendering. */
export function listApplicableMemories(
  issueId: number,
): Promise<ApplicableMemory[]> {
  return api.get<ApplicableMemory[]>(
    `/issues/${issueId}/applicable-memories`,
  );
}

/** GET /api/issues/:id/applicable-memories?suggest=1 — top-3 scored
 * candidates not yet linked to the ticket. */
export function suggestApplicableMemories(
  issueId: number,
): Promise<ApplicableMemory[]> {
  return api.get<ApplicableMemory[]>(
    `/issues/${issueId}/applicable-memories?suggest=1`,
  );
}
