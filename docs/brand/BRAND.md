# PAIMOS — Brand Guide

> Your Professional & Personal AI Project OS.

This document captures the naming and branding decisions for PAIMOS so they
don't get lost in chat histories. It's written for Future-Me and for
whoever later works on the project.

---

## The Name

**PAIMOS** — pronounced like Greek *Kairos, Kosmos, Pathos* ("pie-moss").
The `-os` ending is deliberate: it places the name in a tool-naming
tradition (Helios, Argos, Kairos) and keeps it from reading as a forced
acronym.

### Origin

The name evolved from **PMO** ("Project Management Online") — the working
title that originally described the project. PAIMOS extends PMO by the
element that now defines the tool: **AI** as an integral part rather than
an add-on. The `O` stays consistent: it's still the "Online" from PMO.

### What PAIMOS is not

Nearby names in the search space that PAIMOS should **not** be confused
with (relevant for disambiguation, FAQ entries, SEO descriptions):

- **Apache Paimon** — a lakehouse format for Flink/Spark. Different name,
  but phonetically adjacent. No conflict, but developers from the
  streaming world may ask "do you mean Paimon?".
- **Paimon** — demon from the Ars Goetia, also a companion in Genshin
  Impact. No connection.
- **Daimos** — Japanese mecha anime series (Toei, 1978) and DAIMOS
  Components GmbH in Fürth, Germany. No connection.
- **PAIMOS SA** — Swiss architecture firm in St-Prex. Different industry
  (Nice Class 42 architecture), no trademark conflict.

---

## The Acronym

At first glance, PAIMOS starts with one `P`. The logo reveals there are
actually **three**:

```
||P AI M  O  S
│ │ │  │  │  │
│ │ │  │  │  └─ System · Services · (Suite)
│ │ │  │  └──── Online · (Operating)
│ │ │  └─────── Management · Managed
│ │ └────────── AI
│ │
│ └──────────── Project
│
Professional · Personal (the two hidden P-strokes)
```

### Fixed anchors, flexible slots

The acronym is deliberately built so that **the first half is fixed and
the second half is parameterized.**

- **`P · P · P`** (the three strokes, Professional · Personal · Project)
  and **`AI`** are **untouchable.** They define the product and the mark.
  If any of these four wavered, the logo and positioning would wobble too.
- **`M`, `O`, `S`** are **interpretation slots.** Each has a small set of
  valid fillings that signal a different facet of the same product —
  without changing the name, the logo, or the abbreviation. The slot set
  is closed (not open-ended), so the acronym never becomes a free-for-all.

| Slot | Valid fillings | What it signals |
|---|---|---|
| **M** | Management · Managed | Active voice (tool manages work) vs. passive voice (work is AI-managed). Audience choice. |
| **O** | Online · Operating · Open | Product maturity and license stance. `Online` = default delivery form. `Operating` = earned platform claim (Phase 2+). `Open` = FOSS stance, usually paired with `Source` in S. |
| **S** | System · Services · Source | Monolith framing (one tool) vs. plural framing (a set of capabilities) vs. FOSS framing. Dev, business, or community register. |


This is what makes the name evolve *dezent*: no rebrand, no logo change,
just a slot swap on the About page as the product or the audience
changes.

### Readings (what actually appears on the website)

PAIMOS ships with **two co-equal readings** in Phase 1. Both are valid,
both are documented, and the audience picks whichever fits them. Neither
is primary.

**Today's reading — *for developers, FOSS users, the default*:**
> **P**rofessional · **P**ersonal · **P**roject / **AI**-**M**anagement
> / **O**nline **S**ystem

**Services reading — *for teams where agents ship alongside humans*:**
> **P**rofessional · **P**ersonal · **P**roject / **AI**-**M**anaged
> / **O**nline **S**ervices

Same name, same mark, same AI-first commitment — two entry narratives.
One lands with engineers who want a FOSS tool for their boards; the
other lands with engineering leads at companies who think in services
and managed capabilities. The About page shows both side by side with
equal visual weight.

### A potential third reading (Phase 2)

If platform features materialize (plugins, agent orchestration, public
API, marketplace), a third reading becomes defensible:

**Platform reading — *for teams building on top of PAIMOS*:**
> **P**rofessional · **P**ersonal · **P**roject / **AI**-**M**anagement
> / **O**perating **S**ystem

The `Operating System` filling is the strongest claim in the whole
matrix — it implies platform, extensibility, ecosystem. Don't use it
until the product earns it. Trigger criteria (at least two must hold):

- A plugin or extensions system exists and is used by third parties
- Multiple AI agents / workflows can be orchestrated together
- A public API enables integration with other tools
- A marketplace or template store is live

Until then: the Platform reading stays documented internally (here) but
is **not shown on the website**. Today's + Services reading are enough.

---

## Visual Identity

### The mark

```
  ╷╷┌─┐
  ││├─┘ A I M O S
  ╵╵╵
```

Three parallel vertical strokes, the rightmost one carrying the P-bowl.
The three strokes encode the three P's of the acronym: **P**rofessional,
**P**ersonal, **P**roject. Those who see it feel smart — those who don't
still get hooked by a clean visual (Easter-egg principle, like the FedEx
arrow or the Amazon smile).

### Proportions

- All three strokes have the same thickness and length
- The P-bowl on the third stroke reaches roughly halfway down the stroke
  height (classic P shape)
- Spacing between strokes: about `1× stroke width`
- Spacing between the P-bowl and `A`: about `0.3× letter height` — this
  is the critical kerning point. Too tight reads as "BAIMOS"; too loose
  breaks the unit.

### Color

**Phase 1: monochrome with graduated opacity on the wordmark.** The mark
uses `currentColor` / the primary text color of its context throughout.
This means:

- It works automatically in light and dark mode
- No two logo variants need to be maintained in the repo
- The project stays visually restrained — FOSS-appropriate

**Opacity stepping on the leading strokes (wordmark only).** In the full
wordmark `|||PAIMOS`, the two strokes before the `P` are set at reduced
opacity, creating a crescendo into the letterforms:

| Element | Opacity |
|---|---|
| Stroke 1 (leftmost) | `0.35` |
| Stroke 2 (middle) | `0.60` |
| Stroke 3 (the `P` itself) + `AIMOS` | `1.00` |

The effect: the three P's are visibly *ranked in presence* — the eye
lands on the `P`, then notices the two ghosted strokes leading into it.
The Triple-P-concept becomes legible without explanation, instead of
being a hidden puzzle only insiders solve.

**Important:** opacity stepping applies **only to the full wordmark**.
The mark-alone variant (favicon, app icon, GitHub avatar) keeps all
three strokes at full opacity — at small sizes, any transparency reads
as rendering error, not intent.

**Do not introduce a brand color** while Phase 1 is active. If one gets
added later, it should distinguish clearly from Apache Paimon (which has
no strong color branding) and from the dominant PM tools: Linear
(blue/purple), Notion (black/white), Asana (coral), Jira (blue).

### Logo variants #FIXME

Maintain these in the repo under `docs/brand/`:

| File | Purpose |
|---|---|
| `wordmark.svg` | Full logo: `\|\|\|P AIMOS` — for README header, website |
| `mark.svg` | Just the three strokes + P-bowl — for favicon, app icon, GitHub avatar |
| `wordmark-on-dark.svg` | Not needed while `currentColor` is used |

The mark-alone variant is the real design win: it scales down to 16×16 px
and stays recognizable. You don't get that from a plain "PM" or "AI"
wordmark setup.

### Typography

Wordmark: a geometric sans with balanced stroke weights, so the drawn `P`
construction doesn't feel foreign next to `AIMOS`. Strong candidates:
**Inter**, **Söhne**, **Neue Haas Grotesk**, **Geist**. Avoid fluid serifs
or typewriter-flavored grotesques.

Body text on the website and in the product: **Inter** is plenty — free,
widely licensed, renders cleanly across platforms.

---

## Voice & Positioning

### The central tension

PAIMOS serves two audiences that overlap more than the PM-tool market
admits:

- **Professional — teams shipping software with AI agents.**
  The dominant user: engineering teams doing agentic development.
  Managing agents (Claude Code, Cursor, Devin, in-house agents) alongside
  humans is becoming part of the PM workflow itself. Tasks aren't just
  "assign to Alex" anymore — they're "assign to Alex, have Agent-A draft
  the PR, review in the same board." Existing tools (Jira, Linear,
  Asana) treat AI as an integration. PAIMOS treats it as a first-class
  participant. That's what the `AI` in the name is about.

- **Personal — solo engineers with side projects.**
  The secondary but equally served user: the same developer running a
  Kanban for their weekend project. Same tool, no enterprise bloat,
  no process tax.

Both audiences want the same thing: a PM system that understands code,
agents, and humans as peers. Most tools pick one audience and make the
other feel wrong. PAIMOS is designed so the side-project board feels as
good as the team board, and the team board doesn't feel like it was
built for non-technical managers.

### Target segment within companies

Where PAIMOS fits sharpest inside a company: **engineering teams doing
agentic software development.** Concretely — teams that already run
Claude Code, Cursor agents, internal agent frameworks, or similar, and
whose existing PM tool can't represent what the agents are doing. For
those teams PAIMOS isn't competing with Jira; it's solving a problem
Jira was never designed to solve.

### Which reading to lead with

The two readings (Today's / Services) are co-equal on the About page,
but in conversation, pitch, or copy outside of that page, pick
deliberately:

- **Talking to developers, solo devs, FOSS folks:** lead with
  **Today's reading**. Language: *system, tool, self-hosted,
  FOSS, board, CLI.*
- **Talking to engineering leads, CTOs, companies adopting agentic
  engineering:** lead with the **Services reading**. Language:
  *managed, services, capabilities, orchestration, platform.*

Same product. Same mark. Different first sentence. The other reading
is always one click away on the About page, so no audience feels
excluded — they just encounter the framing that speaks to them first.

### Tone

- **Direct.** No marketing fluff, no "revolutionary" adjectives.
- **Dev-native.** The reader is an engineer, not a CEO. CLI examples
  before screenshots, keyboard shortcuts before click paths.
- **Honest.** "Online System" instead of "revolutionary platform".
  Missing features get marked "not yet", not hidden.
- **English for code, commits, issues, and docs.** German allowed for
  local communication (e.g. DACH community channels).

### What PAIMOS does **not** want to be

- Not an "enterprise-grade project portfolio management solution"
- Not a clone of Jira, Asana, or Monday
- Not a pure AI tool that falls apart without the LLMs
- Not a desktop-only tool
- Not a tool that treats AI as a chatbot bolted onto the side

---

## Do / Don't

### Do

- Sentence case everywhere — including headings on the website
- Show the triple-P resolution **once** prominently (About page) and
  let it rest otherwise. Easter eggs lose their charm if explained on
  every page
- On the About page, present Today's reading and Services reading
  **side by side with equal visual weight** — neither is primary
- Visually highlight what both readings share (`P · P · P` and `AI`)
  and let only the `M · O · S` slots differ in emphasis — the
  slot-based structure should be visible without needing explanation
- Homepage claim: *"Professional and personal AI project management, online and open source."* — acronym-complete (P · P · P · AI · M · O · S with `O` carrying both "online" and "open" and `S` resolving to  "source"), avoids the "OS" landmine, and puts the FOSS commitment in the first sentence. Rendered on the site with the first word animating through P-P-P permutations (Professional ↔ Personal ↔ Project) to  echo the Triple-P mark.
- Use the three strokes as icon / favicon / avatar from day one. It's
  the strongest visual asset
- Put `mark.svg` prominently in the README header
- When talking to engineering teams, lead with the **agentic
  engineering** angle and the **Services reading**
- When talking to solo devs, lead with the **FOSS + no enterprise
  bloat** angle and **Today's reading**

### Don't

- Don't use any full reading as a tagline. "Professional and Personal
  Project- and AI-Management Online System" (or any variant) is too
  clunky to read aloud — the short form *paimos* stays the marketing
  asset, the long form is documented, not advertised
- Don't rank the readings. No "primary reading / alternative reading",
  no "developer edition / business edition". Both are the product
- Don't invent new slot fillings ad-hoc. The `M · O · S` slot set is
  closed (see table). If a genuinely better filling emerges, update
  this document first, then the website
- Don't show the **Platform reading** (`Operating System`) on the
  website until the Phase 2 trigger criteria are met. It's an earned
  claim, not a marketing upgrade
- Don't introduce a brand color while Phase 1 is live
- Don't force a "PM 3.0" or "AI-native" marketing frame
- Don't overload the logo with effects (gradients, glow, 3D). The
  strength is restraint
- Don't constantly compare PAIMOS to Apache Paimon. Phonetic proximity
  is an SEO fact, not a positioning
- Don't pitch the tool as "AI replaces your PM." It's humans and
  agents collaborating, not humans getting replaced

---

## Legal / IP Status

**As of April 2026.** No registered trademark PAIMOS found in:

- EUIPO / eSearch+ (EU Union trademark) — no hits in indexed results
- DPMAregister (Germany) — no hits in indexed results
- TMview (worldwide aggregated) — no hits in indexed results

**Important:** these statements rest on web searches, not on direct
database queries. Before any actual trademark application, a formal
search must be done — ideally via a trademark attorney, or at minimum
manually through TMview's fuzzy search.

### Nice Classes (for eventual registration)

- **Class 9** — downloadable software
- **Class 42** — SaaS, software development services

Both together cover the product range.

### Handles and resources to secure

Priority descending; as of this document's date all found free:

- [✔︎] GitHub Org `paimos`
- [ ] npm package `paimos` (placeholder release is enough)
- [ ] PyPI package `paimos` (placeholder release is enough)
- [✔︎] Domain `paimos.com` (primary)
- [ ] Mastodon / Bluesky handle `@paimos`
- [ ] LinkedIn page (only once business phase starts)
- [✔︎] NO Instagram handle

### Re-run trademark checks periodically

Every ~6 months, do a TMview check for PAIMOS or near-names (PAIMON,
PAYMOS, PAIMO, PAIMAS) in Class 9/42. Early detection is cheaper than
opposition proceedings.

---

## Phasing Plan

| Phase | Readings shown on website | Claim | Brand color | Trademark |
|---|---|---|---|---|
| **1 — FOSS** | Today's + Services (co-equal) | *Professional and personal AI project management, online and open source.* | monochrome | none |
| **2 — Platform** | Today's + Services + Platform (Operating System earned) | *The OS for how you ship work* (or similar) | 1 primary color | DE word mark, classes 9+42 |
| **3 — Commercial** | Today's + Services + Platform | (product-specific) | full system | EU Union mark |

Every transition is **additive**, not destructive: the logo stays, the
domain stays, the name stays, existing readings stay. New readings are
added once they are earned — never at the cost of the previous ones.

---

## References

- Origin of the name: conversation from April 2026, evolved from PMO
  through PAIMO to PAIMOS
- Visual concept: triple-P monogram, derived from the acronym resolution
- Phonetic model: Greek `-os` ending (Kairos, Kosmos, Pathos)
- Positioning references: Linear (dev focus), Notion (personal/pro
  duality), Obsidian (FOSS spirit), Claude Code / Cursor (agentic
  engineering workflows PAIMOS aims to manage)
- Readings concept: informed by tools that carry multiple legitimate
  entry narratives without rebranding (Notion's "OS for your work /
  second brain / wiki"; Linear's "tool for modern software teams /
  issue tracker / project planner")
