/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-339 — pure filter / sort helpers for the Knowledge tab. Lives
// outside the component so tests can exercise the contract without
// mounting Vue, and so the behaviour stays centralized — when search
// graduates from substring to FTS in v2 there's exactly one call site
// to update.

import type { KnowledgeCategory, KnowledgeEntry } from "@/types";
import { isArchived } from "@/services/projectKnowledge";

export type KnowledgeSortMode = "recency" | "alpha" | "confidence";

export interface KnowledgeFilterOptions {
  category: KnowledgeCategory;
  search?: string;
  memoryType?: string; // 'all' | 'feedback' | 'project' | 'reference' | 'user'
  showArchived?: boolean;
  environment?: string;
  sort?: KnowledgeSortMode;
  // PAI-347 — when true, the filter only returns memory entries the
  // server flagged as stale (the parent component fetches the
  // proposal list separately and passes the id-set down here so the
  // existing pipeline still owns search + archive + env filters).
  staleIds?: Set<number>;
}

/**
 * Apply the user-facing filter / sort pipeline to a list of knowledge
 * entries. The contract:
 *
 *   - search matches case-insensitive substrings against
 *     title + slug + body (single shared search box per the spec).
 *   - showArchived defaults to false; archived entries (status =
 *     `cancelled` per PAI-346) are hidden unless the caller opts in.
 *   - memoryType, when present and != 'all', filters memory entries
 *     by metadata.type. No-op for non-memory categories.
 *   - environment, when non-empty, filters entries whose metadata
 *     includes the environment in `applies_to_environments`.
 *   - sort:
 *       recency  → most-recently-updated first (default)
 *       alpha    → slug ascending
 *       confidence → memory.metadata.confidence high → medium → low,
 *                    treating missing as medium. Only meaningful for
 *                    the memory category; falls through to recency
 *                    otherwise.
 */
export function filterKnowledge(
  entries: KnowledgeEntry[],
  opts: KnowledgeFilterOptions,
): KnowledgeEntry[] {
  const search = (opts.search ?? "").trim().toLowerCase();
  const showArchived = opts.showArchived ?? false;
  const memoryType = opts.memoryType ?? "all";
  const environment = (opts.environment ?? "").trim().toLowerCase();
  const sort = opts.sort ?? "recency";

  const filtered = entries.filter((e) => {
    if (opts.staleIds && opts.staleIds.size > 0) {
      if (!opts.staleIds.has(e.id)) return false;
    }
    if (!showArchived && isArchived(e)) return false;
    if (opts.category === "memory" && memoryType !== "all") {
      const t = (e.metadata?.["type"] as string | undefined) ?? "";
      if (t !== memoryType) return false;
    }
    if (environment !== "") {
      const envs = (e.metadata?.["applies_to_environments"] as unknown[] | undefined) ?? [];
      const ok = envs.some(
        (v) => typeof v === "string" && v.toLowerCase() === environment,
      );
      if (!ok) return false;
    }
    if (search !== "") {
      const hay = `${e.title} ${e.slug} ${e.body}`.toLowerCase();
      if (!hay.includes(search)) return false;
    }
    return true;
  });

  filtered.sort((a, b) => {
    if (sort === "alpha") {
      return a.slug.localeCompare(b.slug);
    }
    if (sort === "confidence" && opts.category === "memory") {
      // PAI-347 ordering: high → medium → low → unscored, with
      // last_referenced_at DESC as the tiebreaker (then updated_at
      // DESC as a safety net for entries the bundle path hasn't
      // yet bumped). "unscored" is anything not in the canonical
      // {high, medium, low} set — kept as its own bucket so users
      // can spot uncalibrated entries at the bottom.
      const order = (e: KnowledgeEntry): number => {
        const v = (e.metadata?.["confidence"] as string | undefined) ?? "";
        if (v === "high") return 0;
        if (v === "medium") return 1;
        if (v === "low") return 2;
        return 3;
      };
      const ordA = order(a);
      const ordB = order(b);
      if (ordA !== ordB) return ordA - ordB;
      const refA = (a.last_referenced_at ?? "") || "";
      const refB = (b.last_referenced_at ?? "") || "";
      if (refA !== refB) return refB.localeCompare(refA);
      return b.updated_at.localeCompare(a.updated_at);
    }
    return b.updated_at.localeCompare(a.updated_at);
  });

  return filtered;
}
