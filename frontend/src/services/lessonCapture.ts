/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-343 — service layer for the lesson-capture flow.
//
// Two responsibilities:
//
//   1. Trigger detection — `getLessonCapturePrompt(issueId)` proxies
//      the read-only GET /api/issues/:id/lesson-capture-prompt endpoint
//      so the trigger logic stays server-side and reusable from the
//      CLI's `--draft-memory` flag.
//
//   2. Submission — `submitLessonCapture(...)` performs the two-step
//      "create memory + link to ticket" flow. POST to the convenience
//      memory endpoint inherits the PAI-353 history / mutation_log /
//      attribution path; the `applies_to_memory` relation is added
//      via the existing /issues/:id/relations API so it surfaces in
//      both the issue detail panel and the memory editor's
//      "Originating Tickets" panel (PAI-342).

import { api } from "@/api/client";
import { createKnowledgeEntry } from "@/services/projectKnowledge";
import { addIssueRelation } from "@/services/issueRelations";
import type { KnowledgeEntry } from "@/types";

export interface LessonCapturePromptResponse {
  /** True when the trigger fires for the ticket — the UI should
   * show the prompt before the terminal-state transition. */
  should_prompt: boolean;
  /** Human-readable explanation of why the prompt fired (e.g.
   * "tag:bug + epic:Hardening Q3"). Empty when should_prompt=false. */
  reason?: string;
  /** Pre-populated slug suggestion built from the ticket title +
   * type prefix. The user can still edit before save. */
  suggested_name?: string;
  /** Convenience: the ticket's PAI-NNN key, used in the
   * originating_tickets cross-link. */
  ticket_key?: string;
}

/** GET /api/issues/:id/lesson-capture-prompt — never fails; returns
 * { should_prompt:false } on transient errors so a flaky network
 * doesn't block the user from closing the ticket. */
export async function getLessonCapturePrompt(
  issueId: number,
): Promise<LessonCapturePromptResponse> {
  try {
    return await api.get<LessonCapturePromptResponse>(
      `/issues/${issueId}/lesson-capture-prompt`,
    );
  } catch {
    return { should_prompt: false };
  }
}

export type MemoryType = "feedback" | "project" | "reference";

export interface LessonCaptureSubmission {
  projectId: number;
  ticketId: number;
  ticketKey?: string;
  slug: string;
  rule: string; // becomes the memory title
  why: string;
  how: string;
  type: MemoryType;
  tags: string[];
}

/** Submit the captured lesson:
 *   1. POST /api/projects/:id/memory — creates the memory entry
 *      with category_metadata.type / .tags / .originating_tickets.
 *   2. POST /api/issues/:ticket-id/relations — adds the
 *      `applies_to_memory` link (bidirectional via issue_relations).
 *
 * Both calls are independent of the actual ticket close — failure
 * here doesn't roll back the status transition (the user can retry
 * the lesson capture without re-opening the ticket).
 *
 * Returns the created memory so the caller can route to it on
 * success ("View memory" link in the post-submit toast).
 */
export async function submitLessonCapture(
  s: LessonCaptureSubmission,
): Promise<KnowledgeEntry> {
  const body = `## Why\n\n${s.why.trim()}\n\n## How to apply\n\n${s.how.trim()}\n`;
  const instanceUrl = window.location.origin;
  const memory = await createKnowledgeEntry(s.projectId, "memory", {
    slug: s.slug.trim(),
    title: s.rule.trim(),
    body,
    metadata: {
      type: s.type,
      tags: s.tags.map((t) => t.trim()).filter(Boolean),
      originating_tickets: s.ticketKey
        ? [{ key: s.ticketKey, instance_url: instanceUrl }]
        : [],
    },
  });
  // Bidirectional link (PAI-342). Best-effort — don't block the
  // returned memory on a relation failure; the user can still see
  // the memory in the project's Knowledge tab even without the link.
  try {
    await addIssueRelation(s.ticketId, memory.id, "applies_to_memory");
  } catch (e) {
    // Surfaced via console so a flaky relation insert is debuggable
    // without surfacing a confusing toast — the memory exists either
    // way.
    console.warn("lesson-capture: failed to link memory to ticket", e);
  }
  return memory;
}

/** Build a memory slug of the form "<type>_<first-six-words>".
 * Mirrors the backend's SuggestMemorySlug — kept in sync so the
 * pre-populated suggestion is identical whether the prompt is
 * fired by the API endpoint or computed locally as the user edits
 * the rule.
 *
 * Pure function — no I/O — easy to unit-test. */
export function suggestMemorySlug(typ: MemoryType, rule: string): string {
  const mt = (typ || "feedback").toLowerCase();
  const r = (rule || "").trim();
  if (!r) return `${mt}_lesson`;
  const fields = r
    .split(/[^a-zA-Z0-9]+/)
    .filter(Boolean)
    .slice(0, 6)
    .map((f) => f.toLowerCase());
  const tail = fields.join("_") || "lesson";
  return `${mt}_${tail}`;
}
