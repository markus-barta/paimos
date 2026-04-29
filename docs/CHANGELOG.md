# Changelog

All notable changes to PAIMOS are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and PAIMOS adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.4.0] — 2026-04-29

### Added

- [PAI-272](https://pm.barta.cm/projects/6/issues/PAI-272) — **CustomerDetailView redesign.** Identity slab on the left of the hero (monogram tile with `--bp-blue` corner-mark accent + name + industry chip + provider link) and a stat / sync rail on the right (€/h + €/LP cards over a ghost Sync row + ⋯ overflow menu collapsing Edit / Delete). Asymmetric body grid splits primary content (Contacts → Projects → Documents) from a sticky About / Notes / Sync provenance side rail (8/4 ≥1024px, 7/5 768–1023px, single-column <768px). Empty states share a dashed-box vocabulary with centered Lucide icon + one-line copy + CTA link. The ⋯ menu teleports to `<body>` to escape the existing `overflow:hidden` clipping pattern (mirrors PAI-246/265). `dev_viewer` correctly hides Sync, Edit/Delete, the "New project" CTA, and the contact-row hover pencil.
- [PAI-273](https://pm.barta.cm/projects/6/issues/PAI-273) — **Contact (Ansprechpartner) entity + customer metadata expansion.** New `contacts` table holds many contacts per customer (one `is_primary` at a time, enforced in transactional code); CRUD endpoints — `GET|POST /customers/:id/contacts`, `GET|PUT|DELETE /contacts/:id`, atomic `POST /contacts/:id/promote-primary`. `customers` extends with `website` / `domain` / `vat_id` / `employee_count` / `annual_revenue_cents` / `description` / `phone` / billing_address quartet / visit_address pair — all nullable, no destructive migration. M87 backfills a primary contact for every customer with non-empty legacy `contact_name` / `contact_email`. Read-compat: `GET /customers/:id` continues to expose `contact_name` / `contact_email`, populated from the primary contact when present. Write-compat: `PUT /customers/:id` carrying legacy fields routes the writes back into the primary contact (creating one when missing) — v1 callers keep working unmodified for one release.
- [PAI-273](https://pm.barta.cm/projects/6/issues/PAI-273) — **HubSpot extended sync.** Companies fetch now requests the full property set (`domain, website, numberofemployees, annualrevenue, description, phone, address, address2, city, state, zip, country`) and maps onto the new customer columns during Import + Sync. Associated contacts are pulled via `/crm/v3/objects/companies/{id}/associations/contacts` followed by `/crm/v3/objects/contacts/batch/read` (paginated in 100-id chunks) and upserted into the `contacts` table keyed on `(external_provider, external_id)` — re-syncs are idempotent. The contacts call soft-fails: a slow or rate-limited contacts endpoint never fails the whole company fetch. The first associated contact takes the primary slot only when the customer has no primary on the PAIMOS side (admins set primaries; we only fill the slot when empty).
- **CustomerDetailView wires the slots PAI-272 left open.** The Contacts card renders a real multi-contact list with the primary-contact pill badge, Ansprechpartner-Funktion (role) label per row, hover-revealed promote-primary (★) / edit / delete actions, and an Add-contact modal. The About card surfaces the new metadata as icon-led data-driven rows (`link` website, `phone`, `hash` VAT, `users` employees, `trending-up` revenue) — each row hides when empty, so sparsely-populated customers stay clean and additive fields don't re-flow the layout. Description renders as a paragraph below the row list with a separator.

### Changed

- DocumentsSection's empty state restyled to the dashed-box pattern (centered icon, one-line copy, "click to upload" CTA when writable) so all three customer-detail empty states share a vocabulary.

## [2.3.0] — 2026-04-28

### Added

- [PAI-261](https://pm.barta.cm/projects/6/issues/PAI-261) — **Encryption-at-rest for user-entered secrets.** New `backend/secretvault` package: AES-256-GCM with per-domain HKDF-SHA256 subkeys, versioned envelope (v1 = `0x01 || nonce(12) || cipher || tag`), v0-fallback read path so existing CRM provider ciphertexts keep decrypting untouched. CRM provider configs (`crm:provider_configs` domain) and the OpenRouter `api_key` (`ai:openrouter` domain, M86 adds `ai_settings.api_key_encrypted` BLOB) both consume the package. AI api_key migration is **lazy** — pre-existing plaintext rows decrypt via fallback until the next admin save, which encrypts under the new domain subkey and clears the plaintext column. Cross-domain ciphertext replay (e.g. a leaked CRM blob played against the AI store) fails AEAD verification at decrypt time. Master-key sourcing is unchanged (`PAIMOS_SECRET_KEY` env > `$DATA_DIR/.secret-key` disk file); operators move from Tier 1 (default, key on volume) to Tier 2 (key in secret manager, env-only) by reusing the same key bytes — no rotation needed for the move.
- [PAI-261](https://pm.barta.cm/projects/6/issues/PAI-261) — **`paimos secrets rotate --new-key <b64> [--dry-run]` operator subcommand.** Decrypts every secret-bearing row under the current `PAIMOS_SECRET_KEY` and re-encrypts under the new key in a single SQLite transaction across both consumer tables. Partial failure (any row that fails to decrypt or re-encrypt) rolls back cleanly — the service keeps working on the OLD key, no recovery needed. `--dry-run` decrypts every row to confirm rotation can proceed, reports counts, writes nothing. Operator workflow on success is `service stop → rotate → update env → service start` and the CLI prints the recipe. Today, swapping `PAIMOS_SECRET_KEY` without first running rotate corrupts every existing ciphertext — that gap is now closed.

### Changed

- [`HARDENING.md` § 3.6](HARDENING.md#36--secrets-management) rewritten with explicit **Tier 1 / Tier 2** framing: T1 = master key auto-generated to `$DATA_DIR/.secret-key` (default, suitable for dev / single-node, does NOT defend against stolen backup tarballs); T2 = master key from secret manager via `PAIMOS_SECRET_KEY` env (recommended for production, backup tarballs are useless without the env-supplied key). Plus a T1→T2 migration recipe that re-uses the same key bytes — no rotation needed, just a location move.
- [`THREAT_MODEL.md` § 5](THREAT_MODEL.md) — the "physical attacker with disk access" clause now acknowledges the field-level encryption that PAI-261 introduces, with explicit "T2 protects against backup theft, T1 doesn't" framing so an operator reading the model gets the honest picture rather than the obsolete "PAIMOS doesn't encrypt at rest" wording.

## [2.2.1] — 2026-04-28

### Added — TODO fill in before committing

- chore(dev): self-bootstrap go via direnv when shell isn't hooked (PAI-267)
- fix(layout): drop view-body flex:1 so tall views don't bleed into AppFooter (PAI-262)
- feat(devseed): rich fixture data for ACME / BUGZ / LOGS (PAI-269)
- docs: DEV_LOGIN.md — token, security model, user matrix (PAI-271)
- feat(dev): build-tag-gated dev-login + minimal-fixture seed (PAI-267 phase 1)
- fix(deploy): use containerized tar for bind storage backups

## [2.2.0] — 2026-04-28

### Added

- [PAI-266](https://pm.barta.cm/projects/6/issues/PAI-266) — Customer search with CRM fan-out. Typing into the existing customer search field now triggers a 300ms-debounced query against the local DB; when local matches are zero and the field has 2+ characters, the search fans out in parallel to every enabled + configured CRM provider that implements the new optional `crm.Searcher` interface. Remote results render as a dropdown beneath the search input, grouped per provider with its logo, with per-group loading + error states inline (one broken integration cannot kill the dropdown). Clicking "Import" opens an inline confirm step and reuses the existing `/customers/import` endpoint. Hits already imported locally — joined on `(provider_id, external_id)` — render muted with an "Open in PAIMOS" link to the existing customer detail page. The legacy URL/ID paste flow is demoted to an "Advanced" affordance in the empty state. **HubSpot** is the first reference implementation, against `POST /crm/v3/objects/companies/search` — same `crm.objects.companies.read` scope as `ImportRef`, no new permissions for operators to grant. New endpoint: `GET /api/integrations/crm/search?q=&limit=` (admin-only). Backend tests cover happy path, 401/403/500/network failure modes, parallel fan-out, per-provider error isolation, and `already_imported` dedup.

### Changed

- HubSpot integration help-text is now blunter about which token format works in practice: only Private App tokens (`pat-na1-…`) have authenticated reliably; Personal Access Keys and Service Account keys are accepted by the form but have failed against HubSpot in our testing. Required scope is documented as the single `crm.objects.companies.read` checkbox — PAIMOS does not write to HubSpot or read contacts, deals, schemas, or sensitive-classified fields.

## [2.1.25] — 2026-04-28

### Changed

- [PAI-263](https://pm.barta.cm/projects/6/issues/PAI-263) — `AppFooter` hoisted into `AppLayout` as a single source of truth. The 15 top-level views that each imported and rendered `<AppFooter />` (`SettingsView`, `ProjectsView`, `CustomersView`, `CustomerDetailView`, `DashboardView`, `IssuesView`, `IssueDetailView`, `SprintsView`, `SprintBoardView`, `ReportingView`, `IntegrationsView`, `ImportView`, `LieferberichtView`, `UsersView`, `DevelopmentView`) no longer do. AppLayout now wraps `<slot />` in a `flex:1; flex-direction:column; min-height:0` `view-body` div and renders `<AppFooter />` after it, so every authenticated view gets the footer for free at the bottom of `.main-content`. Side benefit: `ProjectDetailView` and the loaded state of `IssueDetailView` — the only authenticated views that previously rendered no footer at all — now get one. `AccrualsPrintView` opts out via `route.meta.hideAppFooter` since it ships its own colophon.

### Fixed

- [PAI-264](https://pm.barta.cm/projects/6/issues/PAI-264) — `scripts/release.sh` no longer deadlocks when called from a non-TTY shell. Three new opt-outs short-circuit the CHANGELOG-editor step and commit the auto-generated draft as-is: `--no-edit` flag, `RELEASE_NO_EDIT=1` env var, and dummy `$EDITOR` detection (empty / `true` / `:` / `cat` / `tee`). Plus an idempotent-recovery branch: if a prior run already bumped `VERSION` and prepended the `CHANGELOG.md` entry but bailed before committing (the failure mode hit cutting v2.1.23 and v2.1.24 by hand), a re-run with the same mode now picks up where it left off instead of failing the `working tree clean` gate. Bounded strictly — only `VERSION` + `docs/CHANGELOG.md` may differ, and they must already match the targeted bump. Interactive behaviour is unchanged: with a real `$EDITOR`, the editor still opens for review.

## [2.1.24] — 2026-04-28

### Fixed

- [PAI-265](https://pm.barta.cm/projects/6/issues/PAI-265) — Project Detail: the **⋯ More project actions** dropdown (Export CSV / Import CSV / Edit project) appeared to do nothing on click. State was actually toggling correctly — the panel was just rendered-and-clipped. Both trigger and panel lived inside `<Teleport to="#app-header-right">`, and `.ah-right-slot` carries `overflow:hidden` (load-bearing for long customer-pill / tag-chip ellipsis behaviour, can't be removed). The absolutely-positioned `.pd-overflow-menu` was therefore clipped to the slot's box and invisible. Fix: split the teleport — trigger stays in `#app-header-right`, panel teleports separately to `<body>` with `position:fixed`, anchored to the trigger's `getBoundingClientRect()` on open and recomputed on `window` `resize` + `scroll` (capture phase, so a `.main-content` scroll also re-anchors). Outside-click handler now checks both `triggerRef` and `panelRef` since the panel is no longer a descendant of the wrapper.

## [2.1.23] — 2026-04-28

### Fixed

- [PAI-262](https://pm.barta.cm/projects/6/issues/PAI-262) — `AppFooter` no longer floats into mid-page when the active view is shorter than the viewport. `AppLayout.vue`'s `.main-content` is a `flex-direction: column` scroller, but `AppFooter.vue` was a plain block with `margin-top: 2rem`, so on short views (Integrations Jira tab pre-preview, Reporting, Users, Settings, etc.) the footer rendered flush against the last content block — visually bleeding into the page mid-screen instead of pinning to the bottom. Swapping the static `2rem` for `margin-top: auto` lets the flex item consume leftover column space and pin to the bottom; long-content views are unaffected (auto collapses when no slack remains, and the `1.25rem` `padding-top` still provides separation from the content above the `border-top`). One-line CSS change in `AppFooter.vue:30`. Follow-up [PAI-263](https://pm.barta.cm/projects/6/issues/PAI-263) tracks hoisting `<AppFooter />` out of the 14 view templates and into `AppLayout.vue` as a single source of truth.

## [2.1.22] — 2026-04-28

### Added

- [PAI-260](https://pm.barta.cm/projects/6/issues/PAI-260) — `paimos issue tag add` / `paimos issue tag rm` subcommands. Closes a long-standing gap where `bon26/PMO.md` documented these as the canonical lane-management recipe but the CLI never shipped them, forcing agents to fall back to a racy read-modify-write of `tag_ids` over raw `PUT /api/issues/{id}`. The new verbs sit parallel to `issue relation add/rm`, accept either `--tag <key>` (resolved against `/api/tags`) or `--tag-id <int>` (mutually exclusive), and are idempotent server-side via `INSERT OR IGNORE` / no-op `DELETE`. `--json` mode mirrors `issue update`. `<ref>` accepts both an issue key and a numeric DB id. Unknown tag-key / tag-id surfaces a 404 here rather than a silent no-op against the upstream endpoint. PMO.md needs no edit — its existing recipe (`paimos issue tag-add 2445 --tag dev`) keeps working with the new spelling once `tag-add` is rewritten as `tag add` in client repos that adopt the new verb.

## [2.1.21] — 2026-04-28

### Fixed

- [PAI-259](https://pm.barta.cm/projects/6/issues/PAI-259) — CRM Settings: first-time save now correctly persists the secret. `SettingsCRMTab.vue:120` previously gated all secret writes on `replacing[p.id]?.[f.key]`, which is only set when the admin clicks **Replace** on an already-set secret. On first-time setup `f.has_value` was false, the password input rendered, the admin typed the token, but the value never landed in the patch — the backend then rejected the save with "access token must not be empty" against a clearly non-empty input. The save logic now also sends the secret on first-time setup (`!f.has_value && draftValue !== ''`).

### Added

- [PAI-259](https://pm.barta.cm/projects/6/issues/PAI-259) — CRM Settings: **eye / eye-off toggle** on every secret input so admins can verify what they pasted before saving. Toggle re-masks automatically on Save, Cancel-replace, or when the panel is closed.
- [PAI-259](https://pm.barta.cm/projects/6/issues/PAI-259) — CRM Settings: **Test integration** button + inline log panel. New optional `crm.ConnectionTester` interface lets each provider opt in to a structured smoke test; HubSpot's implementation hits `/crm/v3/objects/companies?limit=1` (same scope as the real import flow, so OK ⇒ genuinely usable). New endpoint `POST /api/integrations/crm/{id}/test` (admin-only) returns `{ok, message, lines}`. The frontend keeps the last 20 attempts per provider in a scrollable card; pass / fail is colour-coded; the test never accepts or echoes the secret on the wire — it round-trips through the same persisted config that powers real imports.

## [2.1.20] — 2026-04-28

### Fixed

- [PAI-258](https://pm.barta.cm/projects/6/issues/PAI-258) — HubSpot CRM integration accepts the new **Personal Access Key** format. The `ValidateConfig` check in `backend/handlers/crm/hubspot/provider.go` previously gated on `strings.HasPrefix(token, "pat-")`, which rejected HubSpot's newer opaque-base64 keys (e.g. `CiRldTEtN…`) even though they authenticate the exact same Bearer-token endpoints. Replaced the prefix gate with a permissive sanity check (non-empty, no whitespace, ≥20 chars, surfaces the "you pasted `Bearer …` by mistake" case explicitly), and updated the Settings UI hint + placeholder to advertise both flavours. Added `provider_test.go` to pin the invariant — the next time HubSpot changes the format, this test fails with a clear signal rather than silently breaking integrations.

## [2.1.19] — 2026-04-28

### Fixed

- [PAI-255](https://pm.barta.cm/projects/6/issues/PAI-255) — Hovering a row in the global-search dropdown no longer scrolls the page and hides `AppHeader`. Root cause: the `activeIndex` watcher in `SearchPalette.vue` called `el.scrollIntoView({ block: 'nearest' })`, which walks the DOM looking for any scrollable ancestor; when the active row was already mostly visible the browser scrolled the page (not the palette), pushing the docked header off-screen. Replaced with a manual `palette.scrollTop` adjustment scoped strictly to the palette's own scroll container, so hover/keyboard navigation can never affect the page scroll.
- [PAI-256](https://pm.barta.cm/projects/6/issues/PAI-256) — `AppHeader` "updated N ago" prefix no longer butts against the issue-key chip on the right. Vue's default template-whitespace rule strips the source-level space between `<span class="ah-meta-prefix">` and `<RouterLink class="ah-meta-link">` when they're on different lines, so a `margin-right` on `.ah-meta-prefix` (0.35rem) restores the visual gap without breaking the Tier 2 `display: none` rule that hides the prefix entirely on narrow `.main` widths.

## [2.1.18] — 2026-04-28

### Added

- [PAI-254](https://pm.barta.cm/projects/6/issues/PAI-254) — Backend handlers for the `structure_*` AI actions exposed by the Project Manifest tab editor (frontend shipped in 2.1.16, backend was the deferred follow-up). Five action keys are now registered with real handlers + admin-overridable system prompts in Settings → AI prompts: `structure_manifest`, `structure_guardrails`, `structure_glossary`, plus two new ones for the new tabs — `structure_dev` and `structure_ops`. Each takes free-form prose and returns a JSON candidate scoped to the right manifest slice; the diff overlay UX is unchanged. Field allow-list extended with `manifest_json` / `guardrails_json` / `glossary_json` / `dev_json` / `ops_json`. `ProjectManifestTabs.vue` gains the **Dev** (`_dev`, terminal icon) and **Ops** (`_ops`, server icon) tabs after Glossary, mirroring the Guardrails/Glossary save + AI-structure flow exactly. The new slices are positioned as future LLM-command sources.

### Changed

- Project-management workflow: paimos product tickets now live in PAIMOS itself (https://pm.barta.cm, project `PAI`, id 6) — the local `+pm/backlog` and `+pm/done` markdown directories are deprecated and the five entries that lived under `+pm/done/` were migrated to PAI-249 through PAI-253. The `+pm/` directory is retained for long-form product framing only (PRD, FIELD_MATRIX). Documented in `+pm/README.md`, `docs/DEVELOPER_GUIDE.md`, and the repo `README.md`.

## [2.1.17] — 2026-04-27

### Changed

- `+pm/P35.62b5b4e` — Settings → Users action column collapses the legacy "Projects" + "Access" buttons into a single matrix dialog. Every grant now flows through `/api/users/{id}/memberships` (none / viewer / editor + reset-to-default per project) and produces one audit-trail entry — the legacy `/users/{id}/projects` UI flow is gone. The dialog adapts to role: a segmented "Explicit grants" / "All projects" filter (default ON for externals, OFF for staff) keeps the ~30-row "None" noise out of the way for sparse externals, an "Add project" composed bar at the top makes assignment discoverable, a hairline "Defaults — {role-default}" divider separates explicit overrides from defaulted rows, and the Editor pill is hidden everywhere (per-row + Add bar) for external users since they can only be Viewer. Backend `/users/{id}/projects` endpoints stay alive for now (still exercised by `backend/handlers/portal_test.go`); endpoint deprecation is a follow-up.

## [2.1.16] — 2026-04-27

### Added

- `+pm/P40.854a7db` — Manifest editor in `ProjectContextSection` is now a three-tab editor (Manifest / Guardrails / Glossary) with a per-tab "Structure with AI" button. Guardrails persist under `manifest.data._guardrails`, Glossary under `manifest.data._glossary` (reserved keys); the Manifest tab edits everything else. Save merges all three slices through the existing `/projects/{id}/manifest` endpoint — no backend changes for persistence. The AI button routes through `useAiOptimize.runRewriteAction()` + `AiSurfaceFeedback` so accept/reject UX matches every other AI rewrite. Per-tab drafts survive tab switching; tab dots indicate which slices have content. The PAI-178 sentinel contract on `ProjectContextSection` is preserved — `hasManifest` now means "the manifest area is populated" (any of the three slices). Backend follow-up (separate ticket): three new action handlers (`structure_manifest`, `structure_guardrails`, `structure_glossary`) under `backend/ai/`, registered in the dispatcher + `/api/ai/actions` catalog. Until those land, the AI buttons render but error out at request time with the standard red-pill UX.

## [2.1.15] — 2026-04-27

### Changed

- `+pm/P30.1567bd8` — AppHeader now degrades gracefully when its container shrinks (pinned side panel, narrow viewport, etc.). Header height is locked at 52px and the right cluster never wraps to a second row. `.main` is a container-query root (`container-type: inline-size`); `.app-header` collapses through four `@container` tiers at 1100 / 920 / 760 / 600px of `.main` width: T1 drops the project subtitle and tag chips and shrinks the search; T2 ellipsis-truncates the title at 14ch, strips the "updated Xh ago" prefix from the meta (keeping the issue-key link), and reduces the customer pill to icon-only; T3 collapses the search to a 36px icon-button (it expands inline on focus) and drops the "Undo" label; T4 hides the title entirely. A soft right-edge fade mask on `.ah-left` softens breadcrumb truncation. Mobile viewport (< 900px) is unchanged — the multi-row stack is preserved with `height: auto`. `ProjectDetailView` wraps the timestamp prefix in a new `.ah-meta-prefix` span so T2 can hide it without nuking the issue-key link.

## [2.1.14] — 2026-04-27

### Fixed

- `+pm/P30.c6aad81` — AppHeader now shrinks alongside the main content when the issue side panel is **pinned**. The panel is `position: fixed`, so it never pushes layout; the previous shrink-when-pinned logic only ran on the inner `.issue-list-root` and the top `AppHeader` stayed full-width and got covered by the panel. A new `useSidePanelPinned` singleton lifts the pinned + visible state out of `IssueList`, and `AppLayout` consumes it (together with `useSidePanelWidth`) to apply `padding-right` on `.main`. Both the header and the main content now reflow as one column.

## [2.1.13] — 2026-04-27

### Changed

- PAI-246 — App-header right cluster polish: Undo button now inherits `.btn-sm` like the per-view Edit button next to it, so the two read at the same size and weight (the global `.btn` padding was previously winning over the missing `.btn-sm` rule in `AppHeader.vue`).
- PAI-246 — Project Detail header collapses the Export CSV / Import CSV / Edit project trio behind a single `⋯` menu. The menu items show icon + label, respect the existing admin/edit permission gates, and close on outside click, Esc, and selection.
- PAI-246 — Within that menu, Export CSV uses an up/out arrow and Import CSV uses a down/in arrow, following the data-flow convention (data *leaves* on export, *enters* on import); the labels in the menu disambiguate any icon-only ambiguity that convention introduces vs. the more common user-device convention.

## [2.1.12] — 2026-04-27

### Changed

- PAI-245 — App-header refinements: the global Undo control moved from the center cluster to the far right of the header, immediately next to the per-view Edit button, and is now styled as a ghost button matching its neighbours instead of a rounded pill. Right-slot status badges (`active` / `archived`) and the `updated …` meta line now read with the same muted weight as the ghost buttons next to them, so the right cluster no longer outweighs Edit visually.

### Fixed

- PAI-245 — Search-input placeholder no longer overlaps the magnifying-glass icon. The `:not()` chain on the global form-control rule (added in 2.1.8) was bumping its specificity to (0,5,1) and overriding component-scoped padding; the negations are now wrapped in `:where()` so the rule keeps single-element specificity.

## [2.1.11] — 2026-04-27

### Fixed

- PAI-220 — `Settings → AI → Usage today` table headers (Prompt + completion / Calls / Cap) now share right-alignment with their numeric values. The previous `.ai-usage-table thead th` rule's specificity was beating `.ai-usage-num`, leaving headers left-aligned while values rendered right-aligned. Numeric headers also use `tabular-nums` so digits line up cleanly.

## [2.1.10] — 2026-04-27

### Fixed

- PAI-242 — Sidebar search field is now cleared on logout instead of bleeding the previous user's last query into the next session via the persisted `paimos:search:lastQuery` localStorage key.
- PAI-243 — `MetaSelect` (status / priority / type / assignee dropdowns) now flips above the trigger when there isn't enough room below the viewport, and the option list's `max-height` is clamped to the available space so options never disappear off the bottom of the screen.
- PAI-244 — Timer `start()` now refreshes running state from the server before checking, so the "other timers running — switch / both / cancel" prompt is no longer raised against stale local cache when another tab/session has already stopped the timer. Same-browser tabs additionally sync via a `BroadcastChannel('paimos:timer')` so stopping/starting in one tab updates peer tabs immediately.

## [2.1.9] — 2026-04-27

### Fixed

- AI result modals (`AC checklist`, AI suggestions, sub-tasks, UI spec) now layer above the issue side panel and project workspace dock instead of being clipped by them. The `AppModal` overlay z-index moved from 100 to 1000, putting it above every right-edge sidebar while staying below confirm dialogs.

## [2.1.8] — 2026-04-27

### Fixed

- AI suggestions modal (`suggest_enhancement`, `generate_subtasks`) no longer collapses each suggestion into a one-word-per-line column on the right edge; the row checkbox stays compact and the suggestion title, impact pill, target pill and body render on a normal flex row.
- Global `input,select,textarea` form-control style is now scoped to text-input types, so checkboxes, radios, file/range/color inputs are no longer forced to `width: 100%` with text-input padding/border.

## [2.1.4] — 2026-04-26

### Fixed

- Issue-list freshness banners now sit with proper top spacing instead of crowding the toolbar cluster.
- AI result details now render in a wide, readable modal layout instead of collapsing into a narrow strip.
- Header/title geometry now preserves left-side project context cleanly beside the pinned sidebar.
- Footer project workspace status now resets and rehydrates correctly when switching between projects.

## [2.1.5] — 2026-04-26

### Fixed

- Saved views now restore flat/tree mode reliably, including older and fallback views that previously lacked an explicit `treeView` value.

## [2.1.7] — 2026-04-26

### Changed

- Undo activity now opens as a right-edge sidebar (shared width with the issue panel and project workspace dock) instead of a popup card; only one sidebar is visible at a time.
- App header keeps a single explicit Undo button — the duplicate rewind icon inside the search field is gone.
- AppFooter logo is right-aligned and links to paimos.com; on the project detail view the footer no longer doubles up with the workspace rail.

## [2.1.6] — 2026-04-26

### Changed

- Project detail now anchors the Docs / Coop / Context rail at the true bottom edge, with a compact brand footer above it so the issue list gets more vertical room.

## [2.1.3] — 2026-04-26

### Changed

- Project Detail now uses a footer-level workspace rail for Context, Docs, and Coop instead of toolbar/sidebar project workspace controls.
- Project workspace panels now open as one docked footer-adjacent workbench with shared status presentation.

## [2.1.2] — 2026-04-26

### Added

- Project Detail now exposes Project Context as a bottom-docked workbench with a compact toolbar status chip instead of a floating right-side panel.
- Issue and global issue lists now support opt-in freshness banners backed by weak ETags and conditional polling.
- A reusable locale-aware number-formatting helper now drives the AI usage panel and other follow-on surfaces.

### Changed

- The global undo affordance is now visible in the app header instead of being hidden inside the search input.
- The AI provider configuration surface moved from `Settings -> AI` to `Integrations -> AI`, while `AI prompts` stays in Settings.
- The app header and shell now wrap more gracefully on narrow widths and align correctly against the pinned sidebar.

## [2.1.1] — 2026-04-26

### Fixed

- Issue side-panel AI result and decision UI now appears directly under the header action cluster instead of lower in the form area.
- AI surface inventory regression test updated to the corrected side-panel host mount.

## [2.1.0] — 2026-04-26

### Added

- Durable undo and redo foundation with `mutation_log`, conflict resolution, per-issue activity, and admin stack-depth controls.
- AI UX completion across shared result strips, surface feedback, paper trail views, and durable AI-applied undo.
- Undo documentation and configuration references, including runtime system settings and retention guidance.

### Changed

- Issue and side-panel AI flows now participate in the durable request-correlated undo path.
- Recent activity surfaces now unify AI and non-AI issue mutations on top of `mutation_log`.

## [2.0.4] — 2026-04-26

### Added — Project context substrate and hybrid retrieval

- Merge branch 'feat/pai-29-pai-30-substrate'
- docs(security): operator hardening guide + reference architecture (PAI-133)
- Complete hybrid retrieval and symbol graph flow
- docs(security): threat model + named security invariants per domain (PAI-125)
- Add deterministic embedding retrieval substrate
- Add lexical project-context retrieval index
- Build project-context tooling substrate
- docs(security): solo-maintainer continuity plan + tabletop (PAI-144)
- docs(security): incident response runbook + tabletop exercise (PAI-131)
- fix(settings): move CRM tab from Settings to Integrations (PAI-179)
- docs(2.0): audit + decision report and planning-hierarchy review (PAI-189 close-out)

## [2.0.3] — 2026-04-26

### Fixed

- **AI action dispatcher route was not mounted after the catalog
  refactor in v2.0.2.** The action menu loaded but `POST
  /api/ai/action` returned 404, breaking every AI surface end-to-
  end. Re-mounted the route on the chi router; verified against
  the catalog flow + catalog test (PAI-189).

## [2.0.2] — 2026-04-26

### Changed — Wave 2/3/4 service-extraction refactor (PAI-189)

The 2.0 architectural consolidation continued through three more
merge waves on top of v2.0.1, all behavior-preserving. No REST
contract changes.

- **Backend** — issue-detail sidecar concerns (anchors, attachments,
  comments, history, relations, time entries, group members, epic
  completion) lifted from the issue handler into per-domain seams,
  reducing the issue-handler's coupling to a single responsibility
  per call site.
- **Project context + AI catalog seams** — `project_context_service.go`
  and `ai_action_catalog_service.go` extracted; handlers delegate
  rather than orchestrate.
- **Detail-view mutations** — view-level mutation orchestration
  moved out of the chunky `IssueDetailView` Vue file into typed
  service modules under `frontend/src/services/issue*.ts`, each
  with its own Vitest suite.
- **AI response parsing** — hardened against malformed provider
  responses; the parser now refuses unknown shapes loudly rather
  than silently degrading.
- **Schema audit utility** — `backend/db/schema_audit.go` ships
  small introspection helpers used by the new schema regression
  tests; catches accidental schema drift between development and
  production migrations.

Audit & decision record: see [`docs/2.0_AUDIT.md`](2.0_AUDIT.md).

## [2.0.1] — 2026-04-26

### Changed

- Hardened test reporting so deployments now expose honest missing, partial, and ready states and support admin-side report bundle ingestion.
- Added security regression coverage, authorization fuzz checks, and stronger CI scanning for backend invariants and release trust surfaces.
- Mounted and validated the AI action catalog flow so empty menu states now reflect real assignment state instead of missing API wiring.
- Extracted backend service seams for project context, AI action catalog, and development reporting to reduce handler coupling without changing behavior.
- Refactored frontend settings, project detail, and issue detail orchestration into typed registries and helper modules for clearer state ownership.
- Added schema regression coverage and deterministic release plumbing to improve upgrade safety and release provenance.

## [2.0.0] — 2026-04-26

The Project Context layer for code-aware agents — built up incrementally
across the v1 series under PAI-29 (context-in) and PAI-30 (context-out)
— promotes from internal/experimental to a v1-stable agent contract.
The schema (`project_repos`, `project_manifests`, `issue_anchors`,
`entity_relations`) has been live for a while; this release finalizes
the handlers, completes the typed entity-graph traversal, and publicly
documents the surface in `AGENT_INTEGRATION.md` and `api-minimal.md`.
That public agent contract is what the major bump marks.

### Added — Project Context for code-aware agents (PAI-29 / PAI-30)

Coding agents (Claude Code, custom build/triage bots, anything that
operates on the repo as well as the issue tracker) now have a
first-class read-and-write surface for **structured project facts**
that go beyond markdown. The intent: an agent shouldn't have to grep
six issues to figure out which repo to clone, which command to run,
or where in the source tree the issue it's working on actually lives.

**Six new endpoints**, all per-project / per-issue with the usual
view-or-edit gating:

- `GET /api/projects/{id}/repos` · `POST` · `PUT /{repoId}` · `DELETE`
  — declare the linked repositories (URL, default branch, label,
  sort order). The list is what `paimos-mcp` and ad-hoc agent skills
  look at to decide where to clone.
- `GET /api/projects/{id}/manifest` · `PUT` — structured project
  truth: stack, commands, services, owners, NFRs, ADR refs.
  Recommended v1 keys: `repos`, `commands`, `stack`, `services`,
  `owners`, `nfrs`, `adrs`. Free-form JSON beyond that — admins can
  shape it for their own agents without a schema migration.
- `POST /api/projects/{id}/anchors` — bulk-ingest issue→file/line
  locations from a repo-side scanner. Each anchor carries
  `schema_version`, `repo_revision`, and `generated_at` so a deep
  link in a ticket can be trusted to either resolve at the recorded
  revision or fail loudly.
- `GET /api/issues/{id}/anchors` — read-side: every recorded
  file/line for one issue, across every linked repo, with the
  per-anchor revision/schema metadata.
- `GET /api/projects/{id}/graph?root=issue:42&depth=2` — typed
  entity-graph traversal. Returns the relations rooted at the given
  node (issue, repo, anchor, project) up to `depth` hops. Backed by
  `entity_relations` rows that are populated incrementally as
  issues, anchors, and repos move through the system.
- `POST /api/projects/{id}/retrieve` `{q, k}` — mixed-context
  retrieval. Combines issue full-text hits, manifest matches,
  anchor matches, and one hop of graph-neighbor expansion into a
  single ranked result list. Designed to be the one call an agent
  makes when it needs context for a question.

This release adds the typed graph traversal (`fetchEntityGraph`,
`expandContextNeighbors`) and the relation-maintenance helpers
(`upsertIssueEntityRelation`, `deleteAnchorEntityRelationsByRepo`,
etc.) that keep the `entity_relations` table consistent as anchors
and repos churn.

No schema migration in this release — `project_repos`,
`project_manifests`, `issue_anchors`, and `entity_relations` were
added in earlier v1 milestones; v2.0.0 is the contract-promotion
release for the surface that sits on top of them.

### Added — `just doc-sync` (release follow-up workflow)

A new `scripts/release-doc-sync.sh` files a single PAIMOS ticket per
release with a four-surface checklist — README, `docs/`, the
`../paimos-site` repo, brand/screenshots — plus a diff summary since
the previous tag and a snapshot of `paimos-site`'s git state. Run as
`just doc-sync` after `just release`; the release script now prints
the reminder as part of its closing "Next:" output so the step is
hard to miss. `docs/DEPLOY.md` is updated to "the four commands"
with the standard **release → deploy → doc-sync** sequence
documented.

The intent is to close the long-running drift gap between code and
user-facing surfaces: ship the code, deploy it, and within the same
session decide (and record on a ticket) which of README / internal
docs / public site / screenshots actually need a refresh.

### Compatibility

No breaking REST changes. Every v1.x endpoint continues to work
unchanged. The major bump marks the **public agent contract** for
the project-context surface — PAIMOS commits to keeping these
endpoint shapes stable for the v2 series. Agents that integrated
against the experimental shape during v1 should re-verify field
names against `docs/api-minimal.md`; the documented v2 contract is
the canonical reference going forward.

### Brand — Phase 1 → Phase 2 transition

v2.0.0 also marks the brand's Phase 1 → Phase 2 transition (see
[`docs/brand/BRAND.md`](brand/BRAND.md#phasing-plan)). The brand
guide reserved the **Platform reading** of the acronym (the
"OS" in PAIMOS resolving to *Operating System*) until two of
four trigger criteria held. This release cleared two:

- **Multi-workflow orchestration** — the `POST /api/ai/action`
  dispatcher with 11 admin-tunable actions (sub-actions, per-row
  placement, prompt CRUD, dry-run), composed across three control
  planes (`paimos` CLI, `paimos-mcp`, REST + in-app surfaces).
- **Public API for integration** — `/api/openapi.json` (PAI-119),
  the self-describing `/api/schema` (PAI-87), the v2.0 agent-context
  layer (`/projects/:id/{repos,manifest,anchors,graph,retrieve}`,
  `/issues/:id/anchors`), and the `paimos-mcp` MCP facade.

The remaining two trigger criteria — third-party plugin loop and
marketplace/template store — frame the open Phase 2 roadmap. Brand
colour and DE wordmark trademark are Phase 2 *deliverables*, not
Phase 2 *gates*, and stay deferred until they earn their own
commits. Phase 1 (FOSS) stays active alongside Phase 2; transitions
in the brand model are additive, never destructive.

Visible signals: `paimos.com` banner now reads `phase 2 - platform · v2.0.0`,
the homepage rotator cycles through four co-equal readings (FOSS /
Services / System / Platform), and `about.html` exposes the
Platform reading and the trigger-criteria reasoning.

## [1.10.3] — 2026-04-26

### Changed — Settings → AI prompts edit modal (PAI-183)

Full UX rewrite of the edit modal. The first cut had four real
problems: the "Enabled" checkbox sat centered while its label
right-aligned, the dry-run was buried in a `<details>`, the Cancel /
Run / Save buttons were three different shapes and sizes, and the
dry-run "Issue ID" was a raw number input.

The redesign organises the modal into card-shaped sections with
small uppercase tracked-out titles + one-line hints — Identity (for
custom rows) · Placement · Status · Prompt template · Dry-run
console. The Enabled control became a proper iOS-style switch with
the label on the left. Cancel + Save are now visually identical
40px buttons in a sticky footer that pins to the modal viewport
bottom with a subtle backdrop-blur.

The variable chips above the prompt textarea now insert at the
**cursor position** (and restore focus + caret) instead of
appending to the end — feels considerably better when an admin is
composing a prompt and wants to drop a variable mid-sentence.

The dry-run console is now a permanent panel, not a `<details>`,
with the issue picker on the left and a "Run preview" button on
the right (both 40px tall, aligned). Result renders as a meta
strip (model · latency · tokens · "code default" pill) plus a
2-pane grid: rendered prompt | model response, each in monospace
with a 320px scroll cap.

### Added — Smart issue search picker (PAI-183)

New `IssueSearchInput.vue` component replaces the raw number
input in the dry-run console:
- Empty state: input with leading magnifier and placeholder
- 200ms debounced `GET /api/issues?q=…&fields=list&limit=10`
- Results popover renders type icon + key (mono pill) + status dot
  + title per row
- Keyboard nav: ↑/↓/Enter/Esc; outside-click and Esc dismissal
- Selected state: bordered chip with the same row shape and a ✕ to
  clear, focusing the input back on clear so admins can immediately
  type a new query
- Self-contained: v-model is the id (number | null), the component
  manages its own `selectedIssue` ref for chip rendering

### Changed — AI Settings sticky action bar (PAI-182)

The Last saved + Test connection + Save changes footer moved to
the top of the AI Settings tab and is pinned with `position:
sticky; top: 0`. Test result + save banners sit just below the
bar so the response of clicking either button shows up where the
user just clicked, without scrolling.

### Fixed — Test connection accepts the saved key (PAI-180)

The endpoint had been demanding model + API key in the form payload,
which forced admins to re-paste the key just to run a smoke test
(the SPA never echoes the saved key back). Now the backend falls
back to `ai_settings` for any blank form field; the frontend's
`canTest` flips on as soon as the form has a model AND either a
typed key or a previously-saved one.

### Note on commit-message ticket numbers

Commits in v1.10.1 and v1.10.2 use the labels `PAI-178` / `PAI-179`
in their messages. Those labels collide with real existing tickets
(PAI-178 is the parked AI web-tool epic; PAI-179 is the CRM-move
ticket). The canonical backing for the polish work is **PAI-180**
(v1.10.1) and **PAI-181** (v1.10.2). The commit labels remain as
historical record.

## [1.10.2] — 2026-04-26

### Fixed
- **AI menu was empty on every text field after login.** The action
  catalogue is loaded once at module import; the very first call
  fired before login and 401'd, leaving the cache permanently empty
  and every menu showing "No AI actions are configured for this
  surface yet." Two changes: failed loads no longer flip
  `actionsLoaded` (the next caller retries), and the AiActionMenu
  component nudges a refresh on mount when the catalogue is empty.

### Added — Action placement (PAI-179)

Each AI action now has a `placement` field — `text`, `issue`, or
`both` — that controls where it appears in the UI:

- **Text** actions sit inline next to text fields (textareas).
  Examples: Optimize wording, Translate, Tone check, Suggest
  enhancement, Spec-out, UI generation.
- **Issue** actions sit in issue-level menus only — the issue
  header (full view) and the side-panel header. Examples: Find
  parent / sibling, Generate sub-tasks, Estimate effort, Detect
  duplicates.
- **Both** shows everywhere (no built-in defaults to this; reserved
  for custom actions admins want surfaced broadly).

Defaults are set on each action and are admin-overridable per row
in **Settings → AI prompts → Edit**. The list view shows the
effective placement as a pill next to the surface pill.

#### Schema

- Migration **M79** adds `ai_prompts.placement` (TEXT, default
  empty). Empty means "use the registry default" — admins editing
  to "" via the Settings UI get the registry default back.

#### Endpoints

- `GET /api/ai/actions` now includes `placement` per item, with
  admin overrides folded in server-side.
- `GET /api/ai/prompts` exposes both the admin override
  (`placement`) and the registry default (`default_placement`) so
  the editor can surface "(default: text)" next to the radio.
- `PUT /api/ai/prompts/{id}` accepts a new `placement` field —
  validates against `"" | "text" | "issue" | "both"`. Built-in
  rows now allow placement edits (it's a UX choice, not structural).

#### Frontend

- `<AiActionMenu>` accepts a new `placement` prop (`"text"` | `"issue"`).
  The filter combines surface + placement so text-field hosts only
  see text actions and issue-level hosts only see issue actions.
- `IssueDetailView` mounts an issue-level AI menu in the header
  (both view-mode and edit-mode toolbars).
- `IssueSidePanel` mounts an issue-level AI menu next to the
  pin / next / prev / clone buttons.
- Settings → AI prompts edit modal grows a placement radio cluster
  with `Default / Text / Issue / Both`. List rows show a placement
  pill so admins see which actions land where without opening the
  modal.

## [1.10.1] — 2026-04-26

### Changed — AI polish round

- **Test connection now works with the saved key.** The endpoint
  was demanding a model + API key in the form payload, which made
  admins re-paste the key just to run a smoke test (the SPA never
  echoes the saved key back). Now the handler falls back to the
  saved settings for any missing field; pasting in the form still
  overrides them. Friendlier failure copy when nothing is set up
  on either side.
- **Frontier model picks are vendor-diverse.** The Frontier
  category used to cluster on whichever vendor was trending that
  week; now the picker explicitly takes the top frontier-priced
  model from each of Anthropic, OpenAI, xAI, and Google in that
  order, with vendor pills on the cards so the row is scannable
  at a glance. Trending order still drives the choice WITHIN each
  vendor's bucket.
- **Model picker grid pinned to 4 cards per row** at the new
  1200px tab width; 3/2/1 column responsive overrides kick in on
  narrower viewports.
- **Settings → AI tab widened from 920px → 1200px** to fit the
  4-up grid without squeezing cards below the readable minimum.
- **Project Context moved into the toolbar toggle cluster** next
  to Docs and Coop, instead of rendering full-width above the
  issue tabs. The section now slides in from the right like the
  other aux panels; the toggle button shows a small "i" badge
  when at least one repo or any manifest content exists.

### Added — Real prompts in the prompt editor

- **Built-in action prompts now seed into `ai_prompts.prompt_template`
  on first list call**, so admins see the actual default in the
  Settings → AI prompts editor instead of an empty textarea. Backfill
  for instances that seeded under v1.10.0 (which left the column
  empty) runs idempotently on the next list.
- **Each action handler now reads its system prompt from the
  ai_prompts row**, falling back to the code-defined constant when
  the row is missing or empty. Net effect: edits in Settings →
  AI prompts actually take effect at request time.
- **Reset writes the current code default into the row** instead
  of clearing it. The editor stays useful after reset and
  benefits from any future default changes shipped in code.
- **All 11 built-in prompts rewritten / sharpened** with explicit
  invariants, output schemas, named-entity preservation rules,
  and per-action style guidance (verb-first sentences for optimize,
  testable single conditions for spec-out, vendor-honest
  confidence tiers for find-parent, etc.). The prompts live in
  `backend/handlers/ai_action_prompts.go` as the single source of
  truth; the handlers are now thin and admin-tunable.

## [1.10.0] — 2026-04-25

### Added — AI action suite (PAI-159 → PAI-177)

The single-purpose AI optimize button (PAI-146) is now a **multi-action
dropdown menu** with 9 actions on the issue editor surface and 2 on the
customer surface. Behind it sits a unified action dispatcher, an
admin-editable prompt store, and live model recommendations from
OpenRouter — all configurable through the Settings → AI tabs.

#### New endpoints

- `POST /api/ai/test` (admin) — fixed-prompt smoke test that grades
  via a literal `OK` / `FAIL` whole-word marker. 15 s timeout, 50 token
  budget. Audited under `audit: ai_test ...`. **PAI-159**
- `GET /api/ai/models` (admin) — server-cached top-3 OpenRouter
  models in 6 categories: Frontier, Value, Fastest, Cheapest, Open
  weights, Free. 1 h cache; `?force=1` busts it. Falls back to the
  last-known-good snapshot (`stale: true`) and finally to a curated
  static list when OpenRouter is unreachable. **PAI-160**
- `GET /api/ai/usage` (admin) — per-user daily token totals, request
  counts, effective cap, and admin-override flag. Surfaced in the
  Settings → AI usage panel. **PAI-161**
- `POST /api/ai/action` — unified dispatcher with action registry
  (`ai_action_<key>.go` per action). Replaces the legacy
  `/api/ai/optimize`. Per-action handlers receive a populated
  `aiActionContext` so they focus on prompt + provider + response.
  **PAI-163**
- `GET /api/ai/actions` — catalogue endpoint so the menu renders from
  server data and stays in sync as actions ship. Each entry carries
  `implemented: bool`. **PAI-163**
- `GET/POST/PUT/DELETE /api/ai/prompts` + `/{id}/reset` +
  `/{id}/dry-run` (admin) — full CRUD for the new `ai_prompts`
  table. Built-in rows lazily seed from the action registry; custom
  rows added by admins appear in the menu via the catalogue.
  **PAI-175 / PAI-177**

#### New schema

- M77 `ai_usage(user_id, day, prompt_tokens, completion_tokens,
  request_count)` + `users.ai_cap_override_tokens`. **PAI-161**
- M78 `ai_prompts(id, key UNIQUE, label, surface, parent_action,
  sub_action, prompt_template, enabled, is_builtin,
  default_template_hash, created_at, updated_at)`. **PAI-175**

#### New actions (issue surface)

| Action                   | Sub-actions                                                  | Result UX |
| ------------------------ | ------------------------------------------------------------ | --------- |
| **Optimize wording**     | —                                                            | diff overlay (default click) |
| **Suggest enhancement**  | security · performance · ux · dx · flow · risks              | checklist modal, append to AC/notes |
| **Spec-out**             | —                                                            | categorized checklist (4 categories) |
| **Find parent/sibling**  | —                                                            | top-3 candidate cards |
| **Translate**            | de_en · en_de                                                | diff overlay |
| **Generate sub-tasks**   | —                                                            | editable checklist, batch-create children |
| **Estimate effort**      | —                                                            | popover with h + LP + reasoning |
| **Detect duplicates**    | —                                                            | top-5 cards with similarity tag |
| **UI generation**        | —                                                            | markdown preview, append/replace |

#### New actions (customer surface)

- **Optimize wording** (existing, ported)
- **Tone check (de-sales)** — strips persuasive / sales-y phrasing
  while preserving every named entity, quote, and markdown structure
  verbatim. Available on `customer_notes`,
  `cooperation_sla_details`, `cooperation_notes`. **PAI-173**

#### Settings → AI

- **Test connection** button next to Save — pings the *unsaved* form
  values so admins can verify a (provider, model, key) triple
  before persisting. **PAI-159**
- **Live model recommendations** replace the static 5-card list. Six
  category sections, each with up to 3 cards showing name, slug,
  context window, $/Mtok pricing, and tags. Manual **Refresh** button
  bypasses the 1 h cache. Manual model-id input stays always-visible
  (per the answer to "should manual override be hidden?" — no). **PAI-160**
- **Usage today** panel: org tokens, request count, default cap, UTC
  day pill, plus a per-user table that highlights over-cap rows in
  red. **PAI-161**

#### Settings → AI prompts (new tab)

- List view grouped by built-in / custom. Each built-in row exposes
  Edit + Reset (when overridden); custom rows expose Edit + Delete.
- Edit modal with monospace template editor, per-surface variable
  picker (clicking a variable inserts `{{.Var}}`), enabled toggle,
  and dry-run launcher. **PAI-176**
- Dry-run preview renders the template against a real issue using
  Go `text/template`, calls the LLM once, and shows the rendered
  prompt + response side-by-side without mutating any state.
  **PAI-177**

#### Audit

The audit prefix moved from `audit: ai_optimize ...` to
`audit: ai_action action=<key> sub_action=<sub>? ...`. Operators with
grep patterns on `ai_optimize` need a one-line update. PAI-153
invariant unchanged: NO body content ever appears in audit lines.
A separate `audit: ai_test ...` covers the test-connection ping.

#### Compatibility notes

- The legacy `POST /api/ai/optimize` endpoint is **removed**. The
  frontend `useAiOptimize` composable now POSTs to `/api/ai/action`
  with `action="optimize"` (issue surface) or `action="optimize_customer"`
  (customer surface).
- Frontend `<AiOptimizeButton>` is **deleted**. All 13 host surfaces
  use the new `<AiActionMenu surface="issue|customer">`.

## [1.9.1] — 2026-04-25

### Fixed
- **AI error banner stuck on screen with empty message.** The 1.8.2 fix
  to `errMsg()` was correct, but the banner's `v-if="aiOptimize.lastError"`
  was checking the Vue `Ref` object itself — which is always truthy —
  instead of its unwrapped value. Vue auto-unwraps refs in templates
  only when they're top-level `<script setup>` bindings or live on a
  `reactive()` proxy; nested access on the plain object returned by
  `useAiOptimize()` skipped that. Symptom: a permanent red banner
  reading "AI optimization failed:" with no detail, even on cold
  page-loads where no optimize call had ever happened. The interpolation
  `{{ ... }}` rendered empty correctly (Vue's `toDisplayString` does
  unwrap refs), which masked the v-if defect. Fixed by destructuring
  `lastError` (and `clearError`) into top-level bindings in
  `AiOptimizeBanner.vue` and `IssueDetailView.vue`. Documented the
  gotcha in `useAiOptimize.ts` so future consumers don't repeat it.

## [1.9.0] — 2026-04-25

### Added — AI optimize on project / customer / cooperation fields

The PAI-146 button now appears wherever a user enters longer-form
prose, not just on issue editors. Each new field gets its own
per-field prompt reminder so the model writes at the right register
for the audience and format. The fixed PAIMOS wrapper and the
admin-editable instruction stay shared (safety + tone are global);
only the per-field reminder differs.

New surfaces:

- **Project description** — both the new-project modal and the
  edit-project modal. Reminder: stakeholder audience, preserve
  scope / out-of-scope markers / deadlines / contractual language;
  do not add scope.
- **Customer notes** — both the create-customer modal and the
  edit-customer modal. Reminder: CRM tone, preserve PII and verbatim
  quotes, do NOT invent titles / dates / decisions.
- **Cooperation → SLA details** (PAI-61). Reminder: preserve every
  number verbatim (uptime %, response-time targets, hours-of-coverage,
  escalation steps); "4 hours" stays "4 hours", not "a few hours".
- **Cooperation → Cooperation notes** (PAI-61). Reminder: preserve
  named systems, contractual lines, ownership boundaries, and
  exceptions exactly as written.

Backend allow-list extended (`project_description`, `customer_notes`,
`cooperation_sla_details`, `cooperation_notes`); each new field is
audited under its own name in the `audit: ai_optimize` line so
operators can attribute usage per surface.

No schema or env changes.

## [1.8.2] — 2026-04-25

### Fixed
- **AI optimize button now appears on every multiline issue editor** —
  the v1.8.0 rollout only wired it into the issue-detail edit form.
  Added to **CreateIssueModal** (the per-project + global "New issue"
  flow) and **IssueSidePanel** (slide-out edit panel) so the action
  is reachable wherever a user enters description / acceptance
  criteria / notes text. For new issues `issue_id=0` is sent, which
  the backend already handled by skipping context lookup.
- **Empty error banner.** `errMsg()` could pass an empty/whitespace
  string through to the UI, producing "AI optimization failed:" with
  no message. Replaced with a guard that always returns a non-empty
  string and, for `ApiError` with an empty payload, surfaces the HTTP
  status as `request failed (HTTP <status>)` so admins have something
  to grep. New `<AiOptimizeBanner>` component used by all three host
  surfaces so the banner experience is consistent.

## [1.8.1] — 2026-04-25

### Fixed
- **Settings → AI** tab redesigned. The first 1.8.0 cut shipped with a
  broken toggle row (checkbox, label, and hint distributed across the
  page width) and form inputs stretched to the full tab area instead
  of capping. Replaced with a card-stack layout: hero strip with
  status pill (Ready / Configured · Off / Disabled / Needs
  configuration), enable as a real switch, provider cards with
  PAI-122 placeholders, model preset grid with category tags
  (Fast / Quality / Open weights / Cheap), and a `<details>` disclosure
  listing the six wrapper invariants admins cannot override. UI only —
  no backend or payload-shape changes.

## [1.8.0] — 2026-04-25

### Added — PAI-146 epic: LLM text optimization for multiline fields

An inline AI-assisted writing affordance for the multiline fields
authors reach for the most. Off by default; opt-in per deployment.

- **PAI-147** — Reusable `<AiOptimizeButton>` (ghost-style "AI" pill)
  appears on description, acceptance criteria, and notes editors in
  the issue detail view. Disabled with a tooltip when the feature is
  not configured.
- **PAI-148** — Diff-preview overlay (`<AiOptimizeOverlay>`) compares
  the current and optimized text side-by-side, anchored on unchanged
  lines and tinted on changed ones. Accept / reject / retry, plus
  Esc-closes-as-reject. Works on desktop and mobile (≥720px is two
  columns; below stacks vertically). Inline LCS line diff — no new
  npm dep.
- **PAI-149** — Admin **Settings → AI** tab. Persisted in M74
  `ai_settings` (singleton row): provider, model, API key,
  optimization instruction, enabled flag. API key uses the same
  "currently set / replace" pattern as the CRM secret fields so an
  unrelated edit can't silently clear it.
- **PAI-150** — Fixed PAIMOS-owned system wrapper carries the
  invariants the product enforces: preserve technical meaning,
  preserve markdown structure, preserve architecture-significance
  phrasing (architecture change / breaking change / schema change /
  infra change / new component, plus version-and-migration tokens
  like `M74` / `v1.7.0`), do not translate, do not add scope. Admin
  instruction layers inside via `{{INSTRUCTION}}`. Per-call context
  block carries issue key/type/title, project name, parent epic,
  and field-aware reminders (acceptance_criteria stays a checklist,
  notes keeps the author's voice).
- **PAI-151** — Provider abstraction (`backend/ai/Provider`) keeps
  every vendor-specific concern off the editor flow, the prompt
  wrapper, and the audit pipeline. PAI-122 (local backends) plugs in
  by registering a new `Provider` — no other change required.
- **PAI-152** — OpenRouter provider behind the abstraction. Talks the
  OpenAI-compatible `/chat/completions` endpoint (no SDK, no extra
  deps), maps 401/403 → `ErrProviderUnconfigured`, 429/5xx →
  `ErrProviderUnavailable`, other 4xx surfaces the upstream message
  verbatim ("model not found: foo/bar"). 256 KiB body cap, 60 s
  per-call timeout owned by the handler.
- **PAI-153** — Structured `audit: ai_optimize …` line per call with
  user_id, field, issue_id, model, outcome (`ok` / `fail` / `denied`),
  latency_ms, prompt_tokens, completion_tokens. Prompt and response
  bodies are NOT logged. A regression test
  (`backend/handlers/ai_optimize_audit_test.go`) enforces this.
- **PAI-154** — Rollout to the three target fields in the issue detail
  editor and operator documentation under
  `docs/CONFIGURATION.md` ("AI text optimization (PAI-146)").

**Operator quick-start:** Settings → AI → toggle Enabled → paste an
OpenRouter API key from `openrouter.ai/keys` → pick a model
(`anthropic/claude-3.5-haiku` is a reasonable default) → Save. The
"AI" pill on multiline editors lights up immediately.

## [1.7.0] — 2026-04-25

### Added — PAI-109 epic: Enterprise Security & 03-Specs Readiness (8.5/10 target)

Twelve of fourteen children land in this release; the remaining two
(PAI-110 active-content upload hardening, PAI-122 paimos.com wording
rollback) are tracked separately. The audit at `audit.md` is the
companion document.

**Shipped security defects:**

- **PAI-111** — `GET /api/documents/{id}/download` enforces scope-aware
  authorization. Project-scoped documents require project view access;
  customer-scoped require admin OR view access to a project belonging
  to that customer. 404 on deny so id enumeration cannot probe
  existence.
- **PAI-112** — `PATCH /api/attachments/link` requires `uploaded_by =
  current_user` for non-admin callers, closing the cross-user
  pending-attachment hijack window.
- **PAI-113** — Per-session CSRF defenses for cookie-authenticated
  browser flows. New `csrf_token` column on `sessions` (M72), bound at
  login/TOTP-verify and exposed via a non-HttpOnly `csrf_token` cookie.
  `auth.CSRFMiddleware` enforces same-origin (`Origin`/`Referer` host
  matches `Host`) plus `X-CSRF-Token` match for every session-cookie
  mutation. API-key callers and pre-existing sessions (created before
  M72) are handled via a lazy-upgrade path that issues a token on the
  first authenticated request after deploy.
- **PAI-114** — Global response-header middleware applies `nosniff`,
  `X-Frame-Options=SAMEORIGIN`, `Referrer-Policy`, `Permissions-Policy`,
  conditional HSTS (when `COOKIE_SECURE=true`), and a
  Content-Security-Policy in **Report-Only** mode that posts violations
  to `/api/csp-report`. Non-breaking: in-app PDF and dev-report iframes
  continue to render.
- **PAI-115** — Password-reset link logging requires explicit
  `PAIMOS_DEV_MODE=true` opt-in. Without it, the handler refuses to
  send when SMTP is unconfigured and surfaces the misconfiguration
  rather than silently routing magic links into log aggregators.

**Compliance / audit:**

- **PAI-116** — `PAIMOS_AUDIT_SESSIONS` defaults to **on** (operators
  opt out with `=false` / `=0`). New `incident_log` table (M73) plus
  admin-only CRUD and JSON/CSV export at `/api/incidents/export` for
  SIEM ingestion. Status transitions auto-stamp `resolved_at`.
- **PAI-117** — GDPR ops pack. Background retention sweeper (24h loop)
  with env-tunable windows for sessions, password reset tokens, access
  audit, session activity, closed incidents, and pending TOTP. New
  admin endpoints: `GET /api/users/{id}/gdpr-export` (full per-subject
  JSON dump), `POST /api/users/{id}/gdpr-erase` (anonymisation rather
  than cascade-delete, preserves historical project data), and `GET
  /api/gdpr/retention` (introspect the active policy).
- **PAI-118** — Bricolage Grotesque, JetBrains Mono and DM Sans now
  bundle via `@fontsource` and ship as content-hashed `/assets/*.woff2`
  files. All `fonts.googleapis.com` / `fonts.gstatic.com` runtime
  requests are gone. CSP-Report-Only is now strictly self-only.

**API contract / SSO:**

- **PAI-119** — `/api/openapi.json` publishes a real OpenAPI 3.1
  contract, embedded at build time so the document always matches the
  binary it ships with. Coverage focuses on the canonical surface;
  internal admin one-offs are intentionally omitted.
- **PAI-120** — Single-provider OpenID Connect SSO end-to-end with
  PKCE (`S256`) and JIT user provisioning. New routes:
  `GET /api/auth/oidc/{status,login,callback}`. The login page renders
  an "SSO" button only when `enabled=true` is reported by `/status`.
  JIT matches existing users by case-insensitive email; new users land
  as role=member with seeded project access. Configuration via
  `OIDC_*` env vars, documented in `docs/CONFIGURATION.md`.

**Release evidence:**

- **PAI-121** — CI tag-push generates Go and npm CycloneDX SBOMs,
  signs the published image keylessly via cosign + GitHub OIDC, and
  attaches each SBOM as a cosign attestation against the image
  digest. `scripts/sbom.sh` (`just sbom`) for local generation;
  `docs/RELEASE.md` documents the verification commands.

**Governance:**

- **PAI-123** — `docs/claim-matrix.md` ties every paimos.com
  `/03-specs` claim to its in-repo evidence and follow-on tickets.
  `scripts/check-claims.sh` is wired into `scripts/release.sh` and
  refuses to cut a release if any `aspirational` row lacks a follow-on
  ticket reference. `--yolo` bypass is supported with the reason
  recorded in the release commit message.

### Configuration changes

New environment variables documented in `docs/CONFIGURATION.md`:

- `PAIMOS_DEV_MODE` — gate password-reset link logging.
- `PAIMOS_AUDIT_SESSIONS` — default flipped to `true`; opt out with
  `false` / `0`.
- `PAIMOS_RETENTION_DAYS_*` — per-class retention windows.
- `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`,
  `OIDC_REDIRECT_URL`, `OIDC_SCOPES`, `OIDC_BUTTON_LABEL`,
  `OIDC_POST_LOGIN_REDIRECT`.
- `HSTS_INCLUDE_SUBDOMAINS` — opt-in HSTS subdomain inclusion.

### Migrations

- **M72** — `ALTER TABLE sessions ADD COLUMN csrf_token TEXT NOT NULL
  DEFAULT ''`. Existing sessions are upgraded lazily on first use; no
  forced re-login.
- **M73** — `incident_log` with status/severity CHECK constraints,
  plus indexes on `status` and `detected_at`.

### Notes

- The `id_token` signature is intentionally **not** verified locally;
  trust comes from the TLS userinfo round trip back to the IdP. Adding
  JWKS-based verification is straightforward when a deployment
  requires it.
- CSP runs in Report-Only mode in this release. After a clean
  violation feed, a follow-up release will flip it to enforce.
- PAI-110 (active-content upload hardening) is **postponed** because
  the cleaning would reject SVG/markdown/text uploads existing UIs
  depend on. It is the only Critical from the audit still open and
  carries into the next phase.

## [1.6.1] — 2026-04-24

### Fixed
- HubSpot provider tile in Settings → CRM and the customer-import
  dropdown were rendering a broken-image placeholder — `LogoURL()`
  pointed at `/assets/crm/hubspot.svg` but the asset wasn't shipped.
  Added a stylised neutral mark under `frontend/public/assets/crm/`
  so the tile reads as intentional. New providers ship their own
  SVG under the same path; see `docs/CRM_PROVIDERS.md`.

## [1.6.0] — 2026-04-24

### Added — PAI-28 epic complete: cooperation metadata + CRM provider docs (PAI-61 / PAI-62 / PAI-107)

This release closes the **PAI-28 customer-management epic**: data
model + plugin layer + HubSpot provider + frontend + cooperation
profile + developer docs all shipped. Bumped to **1.6.0** to mark the
epic as feature-complete.

What landed across 1.5.3 → 1.6.0:
- 1.5.3 — backend foundation (customers, documents in MinIO, plugin
  layer, HubSpot provider)
- 1.5.4 — frontend (sidebar, customer views, admin Integrations CRM
  tab, project polish)
- **1.6.0 (this release)** — cooperation metadata + CRM developer docs

### This release in detail (PAI-61 / PAI-62 / PAI-107)

Closes the PAI-28 epic frontend work plus the deferred plugin-layer
docs.

- **Per-project cooperation metadata** (PAI-61). New
  `project_cooperation` table (1:1 with projects, M71): structured
  fields for engagement type, code ownership, environment
  responsibility, plus an SLA bundle (uptime, response time, backup
  + on-call flags) and two markdown freeform fields. Two endpoints:
  `GET /api/projects/:id/cooperation` returns the row or zero-value
  defaults; `PUT /api/projects/:id/cooperation` upserts (admin-only).
  CHECK constraints validated server-side; informational in v1, no
  behavioural effects elsewhere.
- **Cooperation profile section on project detail** (PAI-62).
  View/edit toggle; structured fields shown as labelled value pills;
  SLA section conditional on `has_sla`; freeform fields rendered
  through the existing `useMarkdown` composable. Empty state shows a
  "Set up profile" button for admins so first-time use isn't a
  hidden form.
- **CRM provider developer docs** (PAI-107). New
  [`docs/CRM_PROVIDERS.md`](CRM_PROVIDERS.md) — interface
  walkthrough, copy/paste skeleton, config schema field types, error
  handling conventions, and a worked Pipedrive example. Linked from
  `docs/AGENT_INTERFACE.md` and `docs/DEVELOPER_GUIDE.md`.

## [1.5.4] — 2026-04-24

### Added — customers + CRM provider UI (PAI-28 frontend; PAI-101 admin)

Frontend for the customer / document / CRM-plugin layer that landed in
v1.5.3. Manual customers and CRM-linked customers render through the
same components — provider affordances are conditional, not a
missing-state stub.

- **Customers in the sidebar** (PAI-57). New nav entry below Projects.
  Card-grid list view at `/customers` with a search box and a
  split "+ New customer" button: primary action is always manual
  create (the no-CRM path); the dropdown only lights up when at
  least one CRM provider is enabled + configured.
- **Customer detail** (PAI-58). Sticky identity header with the
  customer name, optional provider badge, default rates, and a
  state-machine sync button (idle → loading → success / error).
  Two-column contact + notes section. Project cards listed inline
  with effective rates and an inheritance indicator. Documents
  section reuses the same component as the project page.
- **DocumentsSection** — scope-agnostic component used by both
  customer and project detail. The whole section is the drop-target
  (no separate dropzone modal). PDFs preview lazily via inline
  `<iframe>` thumbnails; status pills (active / expired / draft);
  20 MB cap; admin-only writes.
- **Admin CRM tab** (PAI-105). New Settings → CRM tab. Each
  registered provider gets a card with status pill (`Disabled` /
  `Enabled` / `Needs configuration`), an enable toggle, and an
  expandable config form rendered from the provider's
  `ConfigSchema`. Secret fields are never echoed: stored values
  show as `••••• currently set · Replace · Clear`. The toggle
  refuses to enable a misconfigured provider both client- and
  server-side.
- **Project detail polish** (PAI-59 / PAI-60). Customer pill in the
  page header (links through to the customer); customer
  assignment dropdown in the edit modal; rate inputs show an
  inline "Inherits €X from {Customer}" hint when left blank;
  project documents section appended below the issue list.
- **Projects view** (PAI-63). Customer filter dropdown in the
  toolbar (with an "Unassigned" option); each project card shows
  a small linked customer pill when assigned.
- **`useExternalProvider`** composable (PAI-106). Single shared
  fetch of `/api/integrations/crm`; consumers ask for a provider
  by id, get back logo / name / deep-link template — no place in
  the UI hardcodes a CRM name.

Visual language stays consistent with the existing app — DM Sans,
existing color tokens, card hover-lift, monospace numerals, sticky
headers.

## [1.5.3] — 2026-04-24

### Added — customers + documents + CRM provider plugin layer (PAI-28 + PAI-101)

Backend foundation for the customer-management epic, refined for CRM
independence. PAIMOS now owns the customer / document / rate-cascading
data model directly; external CRM sync (HubSpot is the first provider)
plugs in through a small Go interface.

- **Customers** (PAI-53): new `customers` table with CRM-agnostic
  `external_*` columns (NULL = manual customer, fully supported as a
  primary mode). Full CRUD endpoints under `/api/customers`. FTS5
  indexed for search; admin-only writes; pair-validation triggers
  enforce that `external_id` and `external_provider` are both set or
  both null at the DB layer.
- **Project ↔ customer FK + rate cascading** (PAI-54): legacy
  freeform `projects.customer_id` column renamed to `customer_label`;
  new `customer_id` is an INTEGER FK to `customers.id`. List + detail
  responses now carry `effective_rate_hourly`, `effective_rate_lp`,
  and `rate_inherited` so the UI can show inherited-vs-overridden
  without computing it client-side.
- **Documents** (PAI-55): new `documents` table with a `scope`
  (`customer` | `project`) constraint that enforces exactly one of the
  two FKs is set per row. File bytes go to MinIO under the
  `documents/<scope>/<id>/…` key namespace (same bucket as
  attachments — single object store for both). 20 MB cap; PDF / image
  / Office MIME allowlist; status badges (`draft` | `active` |
  `expired`) + validity dates.
- **CRM provider plugin layer** (PAI-101 + children): new
  `backend/handlers/crm` package defining the `Provider` Go
  interface, in-process registry, generic
  `POST /api/customers/import`, `POST /api/customers/:id/sync`, and
  the admin Integrations endpoints (`/api/integrations/crm`).
  Provider configs persist in a new `provider_configs` table with
  AES-GCM-encrypted secrets at rest (key from `PAIMOS_SECRET_KEY` env
  or auto-generated under `$DATA_DIR/.secret-key`). Secrets are never
  echoed in API responses or log lines — only a `has_value` flag per
  field so the admin UI can render "••••• has value".
- **HubSpot provider** (PAI-56): first reference implementation.
  Imports a HubSpot company by URL or bare ID, supports manual
  re-sync (preserves PAIMOS-only fields), builds deep-link URLs back
  into HubSpot. Wired in `main.go` via blank import — adding a new
  provider is one line there + one new subpackage.

Frontend types updated for the renamed `customer_label` column;
sidebar / customer list / customer detail views (PAI-57 / PAI-58) and
the admin CRM Integrations UI (PAI-105) ship in a follow-up.

## [1.5.2] — 2026-04-24

### Changed — appearance-state ownership (PAI-84)

Carved out of PAI-40. Three categories of appearance state previously
had ~3 owners each, with `useBranding` and `useTableAppearance` racing
to set the same `--table-row-*` CSS vars on first paint.

- New `useTypeColors` composable: sole owner of `--type-{epic,ticket,task}`.
  `SettingsAppearanceTab` now consumes it via thin computed get/set
  wrappers instead of holding duplicate refs + watchers.
- `useTableAppearance` is now sole owner of `--table-row-border` and
  `--table-row-alt`. Reset via the settings tab now reverts the CSS
  vars to branding defaults live (was a silent bug — required a page
  reload).
- New `useSidePanelWidth` composable shared by `IssueSidePanel` and
  `IssueList`. `IssueList` no longer reaches past the panel boundary
  to read `LS_SIDEBAR_WIDTH` directly. Double-click reset on the
  resize handle now also updates the IssueList offset (previously
  unsynced).
- `useBranding.applyToDOM` no longer touches type or table-row CSS
  vars — it only supplies the defaults the two composables fall back
  to when no user override is set.

Pure structural cleanup. No user-facing behavior changes.

### Fixed — deploy script resilience

- `scripts/deploy.sh` now tolerates fish-shell quirks in remote SSH
  paths, strips PTY artifacts from captured output, and handles
  root-owned bind mounts without aborting the backup step.

## [1.5.1] — 2026-04-24

### Added — agent interface docs (PAI-96, PAI-85 epic step K)

- New `docs/AGENT_INTERFACE.md`: the single place to learn how to
  drive PAIMOS as an agent. Covers install + `auth login`, core
  patterns (keys-or-ids, file-first multi-line, `--dry-run`,
  `--json`), an end-to-end transcript of a real session (ticket →
  branch → deploy → close-with-note), bulk ops (`batch-update`,
  `apply`), schema discovery, MCP wiring for Claude Desktop, and
  explicit failure-mode guarantees.
- Linked from `DEVELOPER_GUIDE.md` as the top-of-document pointer
  for agent-driving readers.

### Closes the PAI-85 epic

All 12 children (PAI-86 → PAI-97) shipped across 12 PRs in one day:

- **API** (A–D): keys everywhere, `/api/schema`, bulk endpoints,
  new relation types.
- **CLI** (E–I): bootstrap, write commands + close-note,
  batch-update + apply, schema + doctor, shell completions.
- **MCP** (J): 6-tool stdio facade.
- **Docs** (K): this file.
- **Session audit** (L): opt-in UUIDv7 mutation trail.

The shell-quoted-JSON foot-gun, the numeric-id lookups, the
project-ID mismatch between instances, and the status-enum guessing
are all gone from the agent hot-path.

## [1.5.0] — 2026-04-21

### Added — MCP facade (PAI-95, PAI-85 epic step J)

- New `paimos-mcp` binary at `backend/cmd/paimos-mcp/`. Hand-rolled
  JSON-RPC 2.0 over stdio (newline-delimited frames). Spawned by
  Claude Desktop / other MCP clients as a subprocess.
- **6 tools** exposed in v1 (allowlist from the epic pickup doc):
  - `paimos_schema` — fetch `/api/schema` for pre-call validation.
  - `paimos_issue_get` — ref (key or id).
  - `paimos_issue_list` — project_key + status/type/priority filters.
  - `paimos_issue_create` — title + project_key required, full field set.
  - `paimos_issue_update` — partial update by ref.
  - `paimos_relation_add` — all 7 PAI-89 relation types.
- **Deliberately NOT exposed**: `batch-update`, `apply`. MCP context
  grows fast — if an agent needs bulk, it shells out to the
  `paimos` CLI.
- Shares `~/.paimos/config.yaml` with the CLI (same file authored
  by `paimos auth login`). `PAIMOS_CONFIG` / `PAIMOS_INSTANCE` /
  `PAIMOS_SESSION_ID` env vars for overrides.
- Errors from tool bodies surface as `isError=true` MCP results so
  agents see "issue not found" without the JSON-RPC envelope
  eating the message.

#### Example: register with Claude Desktop

```json
{
  "mcpServers": {
    "paimos": {
      "command": "/path/to/paimos-mcp",
      "env": {
        "PAIMOS_INSTANCE": "ppm"
      }
    }
  }
}
```

Live smoke (stdio): initialize → tools/list (6 tools) → 4 tools/call
invocations (schema, issue_get, issue_list, issue_get error path)
all return correct MCP-shaped responses.

## [1.4.1] — 2026-04-21

### Added — CLI batch-update + apply (PAI-92, PAI-85 epic step G)

- `paimos issue batch-update --from-file ops.jsonl` — streams JSONL
  (one `{"ref": …, "fields": {…}}` per line), chunks at 100 (the
  server's batch cap), each chunk is one `PATCH /api/issues`
  transaction. Reports per-chunk progress + a final summary. `-`
  reads stdin.
- `paimos apply --from-file plan.yaml` — declarative scaffolding:
  a single command creates an epic + N children + relations in
  one go. Named refs (`name: epic` on a create item) let later
  rows reference the same-plan item; the CLI translates to the
  server's positional `parent_ref: "#N"` before POSTing the batch.
- Both support `--dry-run` to print the resolved payload(s) without
  sending. **Not idempotent in v1** — running `apply` twice
  duplicates. Use `ensure-status` / `batch-update` for subsequent
  changes to scaffolded work.

#### Example plan

```yaml
project: PAI
create:
  - name: epic
    type: epic
    title: Quarter refactor
  - name: child1
    type: ticket
    title: Extract auth module
    parent: epic
relations:
  - source: epic
    type: related
    target: PAI-85
```

## [1.4.0] — 2026-04-21

### Added — session-scoped mutation audit (PAI-97, PAI-85 epic step L)

- New table `session_activity` (migration M68).
- `SessionAuditMiddleware` records mutation requests (POST / PUT /
  PATCH / DELETE) with method, path, status code, user id, and the
  value of the `X-PAIMOS-Session-Id` header. Reads are skipped so
  UI browsing noise doesn't bloat the table.
- **Off by default in v1.** Enable via `PAIMOS_AUDIT_SESSIONS=true`
  env var (reads per-request so flipping it at runtime works).
  Rationale: single-user instances don't need the data yet; flip
  the default once multi-agent use proves it valuable.
- Missing/malformed header is non-fatal: mutation succeeds, row
  written with `session_id = NULL` (operators can still spot
  untagged traffic).
- `GET /api/sessions/{id}/activity` (admin only): keyset-paginated
  by `id > cursor`, default limit 100, max 1000. Response includes
  `next_cursor` pointing past the last returned row (null on last
  page). Keyset not offset — audit tables grow unboundedly.

### Added — CLI session tagging

- The `paimos` CLI generates a UUIDv7 per invocation and sends it as
  `X-PAIMOS-Session-Id` on every request. `PAIMOS_SESSION_ID` env
  var overrides for multi-step shell flows that need to share one
  session across invocations.

## [1.3.3] — 2026-04-21

### Added — CLI shell completions (PAI-94, PAI-85 epic step I)

- `paimos completion bash|zsh|fish|powershell` — Cobra-native.
  Emits the completion script to stdout; install once per shell.
- **Enum-aware completions** driven by the schema cache:
  - Flags `--status`, `--type`, `--priority` on `issue list` /
    `issue create` / `issue update`.
  - Positional args: `issue ensure-status <ref> <status>` (second
    arg), `relation add <source> <type> <target>` (middle arg).
  - Zero network on tab-press — reads `~/.paimos/schema-<instance>.json`.
    Run `paimos schema` once per instance to populate.

#### Install examples

```sh
# fish
paimos completion fish > ~/.config/fish/completions/paimos.fish

# zsh (persistent)
paimos completion zsh > "${fpath[1]}/_paimos"

# bash
paimos completion bash > /etc/bash_completion.d/paimos   # or ~/.bash_completion
```

## [1.3.2] — 2026-04-21

### Added — CLI schema + doctor (PAI-93, PAI-85 epic step H)

- `paimos schema` — shows enums/transitions/conventions from the
  local cache; fetches transparently on first run. `--refresh`
  forces re-download and reports whether the server-side version
  moved. Cache file: `~/.paimos/schema-<instance>.json` (per-
  instance so multi-env setups don't clobber each other).
- `paimos doctor` — read-only preflight. Checks: config readable,
  `/api/health` reachable, API key valid (`/api/auth/me`), schema
  version current. Exit codes 0/1/2 (ok / warn / fail) per
  convention — CI-safe. `--json` emits the same result array for
  programmatic use.

## [1.3.1] — 2026-04-21

### Added — CLI write commands (PAI-91, PAI-85 epic step F)

- `paimos issue create --project PAI --type ticket --title "..." --description-file/--ac-file/--notes-file`
- `paimos issue update <ref> --status done --close-note-file note.md` —
  when the status is terminal (done/delivered/accepted/invoiced/cancelled),
  appends a **Close note** comment so the "why" is captured alongside
  the status change. Errors out if `--close-note` is passed without a
  terminal `--status`.
- `paimos issue ensure-status <ref> <status>` — idempotent: GETs current
  state, PUTs only if different; second run prints "already X" with
  exit 0. JSON mode reports `{changed: bool, previous, status}`.
- `paimos issue comment <ref> --body-file comment.md`.
- `paimos relation add <source> <type> <target>` — accepts all 7
  relation types (dogfoods PAI-89).

### Conventions

- **Multiline inputs are always file-first.** Inline `--foo "…"` is
  single-line by design. `--foo-file <path>` reads UTF-8 (or `-` for
  stdin). Mutually exclusive — combining inline + file errors out
  rather than silently preferring one.
- **`--dry-run`** on create/update prints the resolved request payload
  (method, path, body) as JSON to stdout and exits 0 without calling
  the API. Makes it safe to script without fear.
- Usage errors → stderr + exit 2. API errors → exit 1 (message in
  chosen format — pretty or `--json`). Nothing ever dumps HTML.

## [1.3.0] — 2026-04-21

### Added

- **`paimos` CLI — bootstrap** — PAI-90 (PAI-85 epic, E). New binary
  at `backend/cmd/paimos/`. Cobra + Viper-lite + `golang.org/x/term`
  (hidden API-key input) + `gopkg.in/yaml.v3` (config). Read-only
  surface for v1:
  - `paimos auth login` — interactive; prompts for URL + API key,
    verifies against `/api/auth/me`, writes `~/.paimos/config.yaml`
    (atomic temp+rename, mode 0600).
  - `paimos auth whoami` — shows active instance + user.
  - `paimos project list` — compact table or `--json`.
  - `paimos issue get <ref>` — ref is key or numeric id; pretty by
    default, `--json` pipes the server payload verbatim.
  - `paimos issue list --project PAI --status backlog --limit 20`.
  - `paimos issue children <ref>`.

  Global flags work on every subcommand: `--instance <name>` (multi-
  instance config), `--json` (machine output), `--config <path>`.
  Missing/invalid config → exit 2 with a pointer to `auth login`.
  API errors → exit 1, `--json` emits `{error, code}` (never HTML
  or unstructured dumps). `paimos` with no subcommand prints usage
  to stderr with exit 2.

### Note: CLI is bundled in the backend Go module (monorepo layout).
  Shared types with the server prevent schema drift. Distribution
  via `go install github.com/markus-barta/paimos/backend/cmd/paimos@latest`
  or a release binary — not via the PAIMOS Docker image.

## [1.2.8] — 2026-04-21

### Added

- **Three new relation types** — PAI-89 (PAI-85 epic, D):
  - `follows_from` (spin-off / carved-out ticket),
  - `blocks` (hard blocker, semantically distinct from `depends_on`),
  - `related` (loose "see also").

  Migration M67 extends the `issue_relations.type` CHECK constraint;
  existing rows unaffected. `GET /api/issues/{id}/relations` now tags
  every row with `direction: "outgoing" | "incoming"` so the UI can
  render inverse labels ("follows up on X" vs "followed up by Y")
  without a second stored row. Issue detail page picks up new form
  options + grouped rendering. `SchemaVersion` bumped to **1.1.0**;
  `/api/schema` `enums.relation` lists all seven types.

## [1.2.7] — 2026-04-21

### Added

- **Bulk issue endpoints** — PAI-88 (PAI-85 epic, C). Three new
  admin-only operations, all atomic under a single SQLite transaction
  with a 100-item hard cap (413 on exceed):
  - `POST /api/projects/{key}/issues/batch` — create N issues at once.
    Accepts project key or numeric id. Supports `parent_ref: "#N"`
    to point a child row at an earlier same-batch item, so the
    canonical "create an epic + all its children in one call" flow
    works without a round-trip-per-child.
  - `PATCH /api/issues` — update N issues at once. Body shape
    `[{ref: "PAI-83"|123, fields: {...}}, ...]`; any row failing
    validation rolls back the whole batch and returns per-row errors
    with `rolled_back: true`.
  - `GET /api/issues?keys=PAI-1,PAI-2,PAI-3` — pick list, response
    order matches request order, missing/inaccessible keys surface
    as `{ref, error: "not found"}` entries (never silently dropped).
- **Side-effect note** — bulk ops deliberately SKIP the auto-promote
  parent-epic / cascade-children / billing-timestamp logic that
  single-issue CreateIssue and UpdateIssue run. Bulk is mechanical;
  the CLI calls single endpoints when it wants the full lifecycle.

## [1.2.6] — 2026-04-21

### Added

- **`GET /api/schema` — self-describing discovery endpoint** — PAI-87
  (PAI-85 epic, B). Public, no auth required. Returns a versioned JSON
  payload with `enums` (status, priority, type, relation), recommended
  `transitions` (any→any still accepted server-side — these are hints
  for clients), `entities` (required/optional field lists per type,
  with `key_shape` for issues), and `conventions` (AC checkbox
  format, key case-sensitivity, multiline-input guidance). Strong
  ETag + `Cache-Control: public, max-age=300`; `X-Schema-Version`
  header mirrors the body's `version` string. The CLI (PAI-90+) and
  MCP (PAI-95) consume this before POSTing so typos like
  `status: "completed"` get caught client-side with a suggestion.
  Regression test hashes the payload — editing the schema without
  bumping `SchemaVersion` fails CI.

## [1.2.5] — 2026-04-21

### Added

- **API: accept issue keys everywhere** — PAI-86 (PAI-85 epic, A). Every
  `/api/issues/{id}/*` route now accepts either the numeric id or an
  issue key like `PAI-83` / `PMO26-639`. Keys resolve server-side via
  `(project.key, issue.issue_number)`; numeric ids keep working
  unchanged. Malformed refs return 400, key-shapes with no match 404.
  Soft-deleted issues still resolve so `restore` / `purge` can target
  them by key. New helper `auth.ResolveIssueRef` + `auth.IsIssueKey`;
  table-driven test in `keyresolve_test.go`.

## [1.2.4] — 2026-04-21

### Changed

- **Issue-status constants centralized + `IssueStatus` type completed**
  — PAI-44 (scope c). New `frontend/src/constants/status.ts` exports
  `STATUSES` (all 9 in canonical workflow order),
  `ACCRUALS_DEFAULT_STATUSES` (done / delivered / accepted / invoiced),
  and `ACCRUALS_EXTRA_STATUSES` (new / backlog / in-progress /
  cancelled — `qa` deliberately excluded). Four call sites migrated:
  `IntegrationsView.vue`, `ImportView.vue`, `AccrualsPrintView.vue`,
  `ProjectsView.vue` — the previously-duplicated literal arrays are
  gone.

### Fixed

- **`IssueStatus` type now reflects the full 9-status workflow**
  (`types.ts:73`). Previously omitted `qa` and `delivered`, which the
  backend already emits and the frontend handles at runtime — the
  type was silently wider than declared.

### Not changed (deliberately)

- Single-value equality checks like `if (s === 'done')` were left
  alone: TypeScript's `IssueStatus` union already gives compile-time
  safety there, so importing a const would be noise.
- `SprintBoardView`'s narrower "completed" arrays and the
  `useInlineEdit` / `IssueEditSidebar` "terminal" sets (which
  disagree on whether `delivered` counts as terminal) were not
  migrated. That's a semantic question for product review, not a
  centralization question. Flagged in the PAI-44 close note.
- `PT_FACTOR = 8` (`useTimeUnit.ts:38`) and the two `10 * time.Second`
  literals in `backend/handlers/integrations.go` — mentioned in
  PAI-44's description but not in its acceptance criteria; leaving
  to a separate ticket if ever warranted.

## [1.2.3] — 2026-04-21

### Changed

- **State-management pattern documented + localStorage keys
  centralized** — PAI-40. `docs/DEVELOPER_GUIDE.md` now names all
  three state tiers (Pinia store / module-scope composable singleton /
  component-local `ref`) with criteria for when each is appropriate,
  replacing the previous two-tier summary that didn't mention
  singleton composables despite their heavy use. New single source of
  truth `frontend/src/constants/storage.ts` exports every
  localStorage key the app touches — 22 static keys + 4 dynamic
  factory functions, migrated across 16 files. Legacy outliers
  (`sidebar-color-bg`, `sidebar-color-pattern`, `issue-display-type-*`,
  `paimos_time_unit`) keep their non-standard names to avoid wiping
  existing user preferences on upgrade; all documented in the module
  header. Pure refactor — no behavior change. Triple-ownership of
  type-color / table-row-color / sidebar-width state carved out to
  PAI-84 for separate treatment.

## [1.2.2] — 2026-04-21

### Fixed

- **"Session expired" banner stuck on after login** — PAI-83. Any 401
  from a non-auth endpoint flipped the global `sessionExpired` ref to
  `true`, but nothing ever cleared it. A user logging back in after a
  stale session saw the dashboard correctly populated — but the
  "Session expired. Your content may be out of date — please sign in
  again." banner persisted at the top. Fix: clear `sessionExpired` in
  `auth.setUser()` (which both password and TOTP login paths converge
  on) and in `auth.login()`. Added a regression test in
  `src/stores/auth.test.ts`.

## [1.2.1] — 2026-04-20

### Fixed

- **Issue-list scroll containment** — PAI-16. Two related scroll bugs
  in the issue list are gone:
  - In **tree view**, `AppFooter` no longer bleeds into the last rows.
    `<IssueTreeView>` now lives in a `.issue-tree-wrap` scroll container
    mirroring the flat table's `.issue-table-wrap` pattern; the
    `.tree-active` `overflow: visible` opt-out rules on
    `.issue-list-root` / `.issue-list-main` are dropped.
  - With the **side panel open in unpinned (floating) mode**, the list
    behind the panel stays scrollable via wheel / trackpad. Previously
    the transparent full-viewport `.sp-backdrop` intercepted wheel
    events and — being `position: fixed` — terminated the scroll chain
    at the viewport (`<html>` is `overflow: visible`). Now a
    `@wheel.passive` handler on the backdrop forwards the scroll to
    the element visually beneath it, preserving the existing
    click-to-close behaviour.

## [1.2.0] — 2026-04-20

### Added

- **Soft-delete (Trash) for issues** — PMO26-639. `DELETE /api/issues/{id}`
  now moves the issue to a Trash instead of hard-deleting. Child tasks
  cascade into the Trash too; `issue_relations` (groups / sprint /
  depends_on / impacts) stay intact so a later Restore re-attaches them
  automatically.
- `POST /api/issues/{id}/restore` — admin-only; clears `deleted_at` on
  one issue. Children stay in Trash — restore is deliberately explicit.
- `DELETE /api/issues/{id}/purge` — admin-only; hard-deletes a trashed
  issue (cascades comments, history, tags, time_entries, attachments,
  issue_relations). Two-step flow: must be in Trash first.
- `GET /api/issues/trash` — admin-only list of soft-deleted issues.
- **Settings → Trash** now lists deleted issues alongside deleted users
  and projects, with Restore and "Delete forever" buttons.
- `issues.deleted_at` (TEXT NULL) and `issues.deleted_by` (INTEGER NULL)
  columns with an `idx_issues_deleted_at` index (migration 66).
- 9 handler tests covering delete / restore / purge / cascade / relation
  survival / cross-project leak protection.

### Changed

- Every user-facing issue query (list, tree, recent, search, sprint,
  reports, aggregation, CSV export, cross-project list, distinct
  cost_unit / release, FTS, portal) now filters `deleted_at IS NULL`.
  Trashed issues only appear via the Trash endpoint.
- `GET /api/issues/{id}` and `PUT /api/issues/{id}` return `404` for
  trashed issues — restore first, then edit.
- Issue delete confirmation dialogs now say "Move to trash" and mention
  that child tasks are moved too and the action is recoverable from
  Settings → Trash.

### Notes

- Protection applies to **new** deletions only. Any issue hard-deleted
  before this release remains unrecoverable.

## [1.1.1] — 2026-04-20

This release bundles the per-project access-control feature with the
follow-on audit fixes and Docker/CI repairs. No `v1.1.0` tag was cut —
the feature landed on `main` and shipped together with the hotfixes
under `v1.1.1`.

### Added

- **Per-project access control.** Every user now has a per-project
  access level (`viewer`, `editor`, or `none`) stored in a new
  `project_members` table. Admins bypass. Members default to editor on
  every non-deleted project. External users get no access until granted
  explicitly. Access changes are written to a new `access_audit` log.
- **Settings → Permissions** tab renders the capability matrix
  (viewer vs. editor vs. admin) straight from the backend, so the
  documentation stays in lockstep with the code.
- **Settings → Users → Access** button opens a per-project matrix
  editor (`viewer` / `editor` / `none`) for any user, backed by the new
  `GET/PUT/DELETE /api/users/{id}/memberships` endpoints.
- `GET /api/permissions/matrix` — public to logged-in users for UI
  rendering.
- `GET /api/access-audit` — admin-only audit trail of grant / update /
  revoke events.
- Backend helpers: `auth.CanViewProject`, `auth.CanEditProject`,
  `auth.AccessibleProjectIDs`, `auth.WithAccessCache`. Frontend store
  exposes `canView()` and `canEdit()` helpers plus a hydrated
  `accessibleProjects` map.
- Per-project chi middlewares: `RequireProjectView`,
  `RequireProjectEdit`, `RequireIssueAccess`, `RequireAttachmentAccess`,
  `RequireTimeEntryAccess`, `RequireCommentAccess` (and their `Edit`
  counterparts). 404 on no-view; 403 on view-only; bypasses for
  orphan sprints.

### Changed

- `/api/auth/login`, `/api/auth/me`, and `/api/auth/totp/verify` now return a
  `{ "user": {...}, "access": {...} }` envelope. The `access` field
  hydrates the frontend's per-project permission cache in a single
  round-trip.
- `CreateUser` (internal roles) and `CreateProject` auto-seed editor
  rows in `project_members` so internal users never lose visibility on
  projects they didn't pre-exist.
- Cross-cutting list endpoints (`/api/projects`, `/api/issues`,
  `/api/issues/recent`, `/api/cost-units`, `/api/releases`,
  `/api/search`, `/api/users/me/recent-projects`) are now filtered by
  the caller's accessible project set.
- `ListIssueRelations` redacts the target title/key (`"RESTRICTED"`)
  when the relation's target lives in a project the caller can't view.

### Removed

- `user_project_access` table. Migration 65 drops it after a
  safety re-insert into `project_members`.

### Fixed

- 10 critical audit findings around access control enforcement
  (PAI-6..PAI-15) — see commit `899dd09`.
- Test suite green after audit fixes; migration test suite sped up.
- Dockerfile: copy `VERSION` and `docs/` into the SPA build stage so
  `__APP_VERSION__` and in-app docs links resolve in the built image.
- CI: sync `VERSION` from the git ref before the docker build so
  tagged images (`v1.1.1` → `1.1.1`) and main-branch images
  (`<base>-dev+<sha>`) have accurate version strings.

## [1.0.0] — 2026-04-19

### Initial release

PAIMOS v1.0.0 is the first public release. It provides a self-hosted
project management system with first-class support for tracking both
humans and AI agents as participants.

**Core features**

- Hierarchical issues (epic → ticket → task) with parent-child relations
- Rich issue metadata: status, priority, assignee, cost unit, release,
  dependencies, impacts, tags, attachments, comments, history
- Sprints, accruals, cost units, releases
- Per-user time tracking with inline timers and billing lifecycle
- Full-text search with partial issue-key matching
- Custom views (saved filter + column sets), per-user view ordering
- Admin panel: users, projects, tags, integrations
- External-user portal with read-only projects + acceptance workflow
- PDF delivery reports (Lieferbericht) with configurable column layout

**Security**

- Session cookies (HttpOnly, SameSite=Lax, optional Secure)
- TOTP 2FA with QR setup, reset, disable
- API keys (`paimos_` prefix, sha256-hashed storage)
- Magic-link password reset with 60-minute token TTL
- Rate-limited auth endpoints (login / forgot / reset / totp-verify)

**Integrations**

- Jira import (project list, field mapping, issue relations)
- Mite time-tracking import (DE/AT; user mapping via note field)
- CSV import/export (per-project and cross-project)

**Branding & configuration**

- **Live-editable branding** via admin-only **Settings → Branding** tab:
  edit product name, tagline, website, page title, full colour palette,
  upload custom logo + favicon — all applied live with no restart.
- Admin API: `PUT /api/branding`, `POST /api/branding/logo`,
  `POST /api/branding/favicon`. Public `GET /brand/<filename>` for
  pre-auth login-page assets (SVG served with restrictive CSP).
- Runtime branding config at `$DATA_DIR/branding.json`, plus
  `branding-<slug>.json` for multi-brand installs.
- Operator-configurable identity via `BRAND_*` env vars (product name,
  company, website, TOTP issuer, email from, API key prefix, DB filename,
  MinIO bucket — see `docs/CONFIGURATION.md`).
- Optional MinIO-backed attachments (graceful-disable when unset).
- Optional SMTP for password-reset emails (dev mode logs links to stdout).

### Provenance

PAIMOS is forked from an internal prototype and published under AGPL-3.0.
All upstream branding, CI, and deployment infrastructure have been removed.
Git history starts fresh with this release.
