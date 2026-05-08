/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 */

// PAI-329 — thin service layer over /api/projects/:id/environments
// and /api/projects/:id/deploy-recipes. Mirrors the projectAgents
// service shape — server is the source of truth for uniqueness; the
// frontend validation echo is purely for UX.

import { api } from "@/api/client";
import type {
  ProjectDeployRecipe,
  ProjectDeployRecipeInput,
  ProjectEnvironment,
  ProjectEnvironmentInput,
} from "@/types";

const NAME_PATTERN = /^[a-zA-Z][a-zA-Z0-9_-]*$/;
const NAME_MAX_LEN = 64;

/** Echo of the backend `validateInventoryName`. Returns "" when the
 * candidate is valid, otherwise the user-facing error message. */
export function validateInventoryName(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return "Name is required.";
  if (trimmed.length > NAME_MAX_LEN) return "Max 64 characters.";
  if (!NAME_PATTERN.test(trimmed))
    return "Letters, digits, _ or -; must start with a letter.";
  return "";
}

// ── environments ───────────────────────────────────────────────────

export function listProjectEnvironments(
  projectId: number,
): Promise<ProjectEnvironment[]> {
  return api.get<ProjectEnvironment[]>(`/projects/${projectId}/environments`);
}

export function createProjectEnvironment(
  projectId: number,
  payload: ProjectEnvironmentInput,
): Promise<ProjectEnvironment> {
  return api.post<ProjectEnvironment>(
    `/projects/${projectId}/environments`,
    payload,
  );
}

export function updateProjectEnvironment(
  projectId: number,
  envId: number,
  payload: ProjectEnvironmentInput,
): Promise<ProjectEnvironment> {
  return api.put<ProjectEnvironment>(
    `/projects/${projectId}/environments/${envId}`,
    payload,
  );
}

export function deleteProjectEnvironment(
  projectId: number,
  envId: number,
): Promise<void> {
  return api.delete(`/projects/${projectId}/environments/${envId}`);
}

// ── deploy recipes ─────────────────────────────────────────────────

export function listProjectDeployRecipes(
  projectId: number,
): Promise<ProjectDeployRecipe[]> {
  return api.get<ProjectDeployRecipe[]>(
    `/projects/${projectId}/deploy-recipes`,
  );
}

export function createProjectDeployRecipe(
  projectId: number,
  payload: ProjectDeployRecipeInput,
): Promise<ProjectDeployRecipe> {
  return api.post<ProjectDeployRecipe>(
    `/projects/${projectId}/deploy-recipes`,
    payload,
  );
}

export function updateProjectDeployRecipe(
  projectId: number,
  recipeId: number,
  payload: ProjectDeployRecipeInput,
): Promise<ProjectDeployRecipe> {
  return api.put<ProjectDeployRecipe>(
    `/projects/${projectId}/deploy-recipes/${recipeId}`,
    payload,
  );
}

export function deleteProjectDeployRecipe(
  projectId: number,
  recipeId: number,
): Promise<void> {
  return api.delete(`/projects/${projectId}/deploy-recipes/${recipeId}`);
}
