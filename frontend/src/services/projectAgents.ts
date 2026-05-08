/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-326 — thin service layer over /api/projects/:id/agents. Frontend
// validation mirrors the server's rules so the user sees errors before
// submit; the server is still the source of truth for uniqueness.

import { api } from "@/api/client";
import type { ProjectAgent, ProjectAgentInput } from "@/types";

const AGENT_NAME_PATTERN = /^[a-z][a-z0-9_-]*$/;
const AGENT_NAME_MAX_LEN = 32;
const RESERVED_AGENT_NAMES = new Set<string>(["web-ui"]);

/** Echo of the backend validation rules. Returns "" when the candidate
 * is valid, otherwise the user-facing error message. */
export function validateAgentName(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return "Name is required.";
  if (trimmed.length > AGENT_NAME_MAX_LEN) return "Max 32 characters.";
  if (!AGENT_NAME_PATTERN.test(trimmed))
    return "Lowercase letters, digits, _ or -; must start with a letter.";
  if (RESERVED_AGENT_NAMES.has(trimmed))
    return `"${trimmed}" is reserved.`;
  return "";
}

export function listProjectAgents(projectId: number): Promise<ProjectAgent[]> {
  return api.get<ProjectAgent[]>(`/projects/${projectId}/agents`);
}

export function createProjectAgent(
  projectId: number,
  payload: ProjectAgentInput,
): Promise<ProjectAgent> {
  return api.post<ProjectAgent>(`/projects/${projectId}/agents`, payload);
}

export function updateProjectAgent(
  projectId: number,
  currentName: string,
  payload: ProjectAgentInput,
): Promise<ProjectAgent> {
  return api.put<ProjectAgent>(
    `/projects/${projectId}/agents/${encodeURIComponent(currentName)}`,
    payload,
  );
}

export function deleteProjectAgent(
  projectId: number,
  name: string,
): Promise<void> {
  return api.delete(`/projects/${projectId}/agents/${encodeURIComponent(name)}`);
}
