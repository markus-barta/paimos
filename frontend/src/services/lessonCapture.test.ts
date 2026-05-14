/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-343 — service tests. Three things matter:
//
//   1. getLessonCapturePrompt round-trips the GET, and returns a
//      safe should_prompt=false on transient errors so a flaky
//      network never blocks the user from closing a ticket.
//   2. submitLessonCapture wires the POST /memory + POST /relations
//      pair correctly, including the "## Why / ## How to apply"
//      body shape and the originating_tickets cross-link metadata.
//   3. suggestMemorySlug builds slugs that match the backend's
//      canonical form (mirrors handlers.SuggestMemorySlug).

import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

import { api } from "@/api/client";
import {
  getLessonCapturePrompt,
  submitLessonCapture,
  suggestMemorySlug,
} from "./lessonCapture";

describe("lessonCapture service", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("getLessonCapturePrompt fetches the trigger endpoint", async () => {
    vi.mocked(api.get).mockResolvedValue({
      should_prompt: true,
      reason: "tag:bug",
      suggested_name: "feedback_crash_on_signup",
      ticket_key: "PAI-99",
    } as never);
    const out = await getLessonCapturePrompt(99);
    expect(api.get).toHaveBeenCalledWith("/issues/99/lesson-capture-prompt");
    expect(out.should_prompt).toBe(true);
    expect(out.suggested_name).toBe("feedback_crash_on_signup");
  });

  it("getLessonCapturePrompt swallows transient errors safely", async () => {
    vi.mocked(api.get).mockRejectedValue(new Error("network down"));
    const out = await getLessonCapturePrompt(99);
    expect(out.should_prompt).toBe(false);
  });

  it("submitLessonCapture creates the memory + bidirectional link", async () => {
    const created = {
      id: 555,
      project_id: 6,
      type: "memory",
      slug: "feedback_use_line_buffered_in_pipes",
      title: "Use --line-buffered in pipes",
      body: "## Why\n\ncause\n\n## How to apply\n\nhow\n",
      status: "backlog",
      metadata: {},
      created_at: "",
      updated_at: "",
    };
    vi.mocked(api.post).mockResolvedValueOnce(created as never).mockResolvedValueOnce(undefined as never);

    const memory = await submitLessonCapture({
      projectId: 6,
      ticketId: 42,
      ticketKey: "PAI-42",
      slug: "feedback_use_line_buffered_in_pipes",
      rule: "Use --line-buffered in pipes",
      why: "cause",
      how: "how",
      type: "feedback",
      tags: ["cli", "logging"],
    });

    // Memory POST.
    expect(api.post).toHaveBeenNthCalledWith(
      1,
      "/projects/6/knowledge?type=memory",
      expect.objectContaining({
        slug: "feedback_use_line_buffered_in_pipes",
        title: "Use --line-buffered in pipes",
        body: expect.stringContaining("## Why"),
        metadata: expect.objectContaining({
          type: "feedback",
          tags: ["cli", "logging"],
          originating_tickets: [
            expect.objectContaining({ key: "PAI-42" }),
          ],
        }),
      }),
    );
    // Relation POST.
    expect(api.post).toHaveBeenNthCalledWith(
      2,
      "/issues/42/relations",
      { target_id: 555, type: "applies_to_memory" },
      undefined,
    );
    expect(memory.id).toBe(555);
  });

  it("submitLessonCapture survives a relation insert failure", async () => {
    const created = {
      id: 1,
      project_id: 6,
      type: "memory",
      slug: "feedback_x",
      title: "x",
      body: "",
      status: "backlog",
      metadata: {},
      created_at: "",
      updated_at: "",
    };
    vi.mocked(api.post)
      .mockResolvedValueOnce(created as never)
      .mockRejectedValueOnce(new Error("relation broke"));
    // Suppress the warn the service emits on relation failure.
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const memory = await submitLessonCapture({
      projectId: 6,
      ticketId: 42,
      ticketKey: "PAI-42",
      slug: "feedback_x",
      rule: "x",
      why: "y",
      how: "z",
      type: "feedback",
      tags: [],
    });

    expect(memory.id).toBe(1);
    warnSpy.mockRestore();
  });

  it("suggestMemorySlug matches the backend's canonical form", () => {
    expect(suggestMemorySlug("feedback", "Use --line-buffered in pipes")).toBe(
      "feedback_use_line_buffered_in_pipes",
    );
    expect(suggestMemorySlug("feedback", "")).toBe("feedback_lesson");
    expect(
      suggestMemorySlug("feedback", "One two three four five six seven eight"),
    ).toBe("feedback_one_two_three_four_five_six");
  });
});
