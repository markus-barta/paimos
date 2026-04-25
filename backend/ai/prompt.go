// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

// PAI-150. Prompt wrapper + context assembly.
//
// The two functions in this file are the only two places that decide
// what we ask the LLM to do. Keeping that knowledge out of the HTTP
// handler and out of the providers is deliberate: when the prompt
// behaviour drifts, this is the file the change happens in, not seven
// other files.
//
// Why a fixed wrapper at all
// --------------------------
// Admins can edit the OPTIMIZATION INSTRUCTION block (PAI-149), but
// they cannot edit the wrapper around it. The wrapper carries the
// invariants PAIMOS owns: no language translation, no scope addition,
// preserve markdown structure, preserve architecture-significant
// phrasing. An admin who innocently writes "make it concise" should
// not accidentally turn off "preserve technical meaning".
//
// Architecture-significance
// -------------------------
// PAI-146 explicitly calls out that text implying architecture change,
// breaking change, schema change, infra change, or new component work
// must be preserved rather than normalised away. The wrapper restates
// this in language the model is trained to weight highly ("MUST",
// "exactly as written"), and the user-prompt context block restates
// it once more next to the actual text — rule repetition near the
// payload tends to win against rule-hidden-in-system-prompt drift.

package ai

import (
	"fmt"
	"strings"
)

// fixedSystemWrapper is the PAIMOS-owned outer layer of every prompt.
// Edits here are product changes, not config changes. The {{INSTRUCTION}}
// marker is replaced by the admin-editable block at assemble time.
const fixedSystemWrapper = `You are an editorial assistant inside PAIMOS, a project-management tool used by software engineers and product owners.

You will be given:
- the contents of a single multiline field on a software project (e.g. an issue description, acceptance criteria, or notes), and
- some surrounding context about which issue and project it belongs to.

Your job is to return a polished version of the SAME field, ready to replace the original.

Hard invariants you MUST follow regardless of any other instruction:
1. Preserve technical meaning, intent, decisions, and constraints exactly as written.
2. Preserve markdown structure: headings, bullet lists, ordered lists, code blocks, inline code, checklists ("- [ ]", "- [x]").
3. Preserve every signal of architecture change, breaking change, schema change, infra change, new component, or deliberate trade-off. Do NOT soften, generalise, or remove these phrases.
4. Do NOT add new requirements, scope, commitments, deadlines, or assumptions that were not in the source.
5. Do NOT translate the text into another language.
6. Do NOT include any preamble, explanation, or markdown fences around the whole reply.
7. Return ONLY the rewritten field content as plain markdown text.

After the invariants above, follow these editorial preferences from the project owner:
{{INSTRUCTION}}

If the source text is already clear, professional, and well-structured, you may return it unchanged.`

// Context is the surrounding metadata the handler passes through so
// the model can write at the right register. All fields are optional;
// empty fields are simply omitted from the assembled context block —
// we don't want to feed the model "Project: " with nothing after it.
//
// Kept deliberately small. Adding more fields here is cheap on the
// PAIMOS side but expensive on token cost, and the marginal benefit
// of e.g. "list of all sibling tickets" is poor.
type Context struct {
	IssueKey     string // e.g. "PAI-146"
	IssueType    string // "epic" | "ticket" | "task"
	IssueTitle   string
	ProjectName  string
	ParentEpic   string // formatted as "PAI-100 — Title", optional
	FieldName    string // "description" | "acceptance_criteria" | "notes" | …
}

// BuildSystemPrompt returns the full system message: the fixed wrapper
// with the admin instruction layered in. Treats an empty admin
// instruction as "use the product default", because saving an empty
// string in settings tends to mean "I want the default" and not
// "I want no editorial guidance at all".
func BuildSystemPrompt(adminInstruction string) string {
	instruction := strings.TrimSpace(adminInstruction)
	if instruction == "" {
		// The handler also seeds this from DefaultOptimizeInstruction,
		// but we don't import that here to avoid a cycle. An empty
		// admin instruction at this point means the caller really did
		// pass empty — fall back to a single benign sentence so the
		// wrapper still parses cleanly.
		instruction = "Optimize for clear, professional, project-appropriate wording."
	}
	return strings.Replace(fixedSystemWrapper, "{{INSTRUCTION}}", instruction, 1)
}

// BuildUserPrompt assembles the user-facing message: a small context
// block followed by the source text inside an explicit fenced block.
// The fence is purely structural — it tells the model "this is the
// payload, not more instructions" — and we strip it from the model's
// response in the handler if it leaks (some models echo the fence
// when asked not to).
//
// Field-name aware copy: the wrapper says "the contents of a single
// multiline field", but the user prompt names which field and reminds
// the model to keep that field's conventions (acceptance_criteria
// stays a checklist, etc.). The reminders are short — long context
// blocks waste tokens.
func BuildUserPrompt(text string, ctx Context) string {
	var b strings.Builder

	b.WriteString("Optimize the following field.\n\n")

	if ctx.FieldName != "" {
		b.WriteString(fmt.Sprintf("- Field: %s\n", ctx.FieldName))
		// Per-field reminders. Kept inline rather than templated so a
		// new field type just needs one case here, not a registry.
		switch ctx.FieldName {
		case "acceptance_criteria":
			b.WriteString("  - Keep checklist items as \"- [ ]\" or \"- [x]\"; do not collapse them into prose.\n")
		case "description":
			b.WriteString("  - Treat any embedded headings, lists, or code blocks as structural; do not flatten.\n")
		case "notes":
			b.WriteString("  - Notes are informal; keep the author's voice and any informal markers intact.\n")
		}
	}
	if ctx.IssueKey != "" {
		b.WriteString(fmt.Sprintf("- Issue: %s", ctx.IssueKey))
		if ctx.IssueType != "" {
			b.WriteString(fmt.Sprintf(" (%s)", ctx.IssueType))
		}
		if ctx.IssueTitle != "" {
			b.WriteString(fmt.Sprintf(" — %s", ctx.IssueTitle))
		}
		b.WriteString("\n")
	}
	if ctx.ProjectName != "" {
		b.WriteString(fmt.Sprintf("- Project: %s\n", ctx.ProjectName))
	}
	if ctx.ParentEpic != "" {
		b.WriteString(fmt.Sprintf("- Parent epic: %s\n", ctx.ParentEpic))
	}

	b.WriteString("\nReminder: preserve every architecture-significance signal exactly as written. Examples in this codebase that MUST be preserved verbatim: \"architecture change\", \"breaking change\", \"schema change\", \"infra change\", \"new component\", \"new migration\", \"new endpoint\", and any explicit version/migration numbers like \"M74\" or \"v1.7.0\".\n\n")
	b.WriteString("Source text (between the fences):\n")
	b.WriteString("```\n")
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n")
	b.WriteString("\nReturn only the optimized field content (no fences, no preamble).")

	return b.String()
}

// StripFenceEcho removes a single leading/trailing triple-fence pair
// if the model echoed it back despite the instruction. Idempotent for
// already-clean output. Intentionally conservative: we only strip
// ``` fences that wrap the *entire* response, not fences that are
// part of legitimate code blocks inside the rewrite.
func StripFenceEcho(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") || !strings.HasSuffix(t, "```") {
		return s
	}
	// Drop the opening fence line (which may include a language tag).
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	} else {
		return s
	}
	// Drop the trailing fence.
	t = strings.TrimSuffix(t, "```")
	return strings.TrimSpace(t)
}
