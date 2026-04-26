# PAIMOS — Agent Interface Guide

> **Who this is for.** Agents (Claude Code / Claude Desktop / custom scripts) and the humans who run them. If you want to read issues, transition status, or scaffold work programmatically, start here instead of reading the REST docs end-to-end.

PAIMOS offers three agent-facing surfaces, in descending order of ergonomic payoff:

1. **`paimos` CLI** — the recommended default. Deals with auth, multi-line inputs, key resolution, error shapes, and shell-safety so you don't have to.
2. **`paimos-mcp` facade** — JSON-RPC over stdio for MCP clients (Claude Desktop). Wraps a curated subset of the CLI as tools.
3. **REST API** — the ground truth. Everything else is a layer on top. See [`api-minimal.md`](api-minimal.md).

Most day-to-day agent work should go through the CLI.

> **Extending PAIMOS with a CRM sync provider** (HubSpot, Pipedrive, …)?
> See [`CRM_PROVIDERS.md`](CRM_PROVIDERS.md) for the in-process Go
> plugin interface — `crm.Provider` + the registry — and a worked example.

---

## 1. Get set up

### Install

```sh
go install github.com/markus-barta/paimos/backend/cmd/paimos@latest
go install github.com/markus-barta/paimos/backend/cmd/paimos-mcp@latest
```

Both binaries end up in `$GOBIN` (default `$HOME/go/bin`). Add it to `PATH` if you haven't.

### Log in

```sh
paimos auth login
# Instance URL [https://pm.barta.cm]: <enter or paste>
# API key (input hidden): <paste>
# ✓ logged in as mba at https://pm.barta.cm
#   saved to /Users/you/.paimos/config.yaml as instance "default"
#   default_instance = "default"
```

Config file is `~/.paimos/config.yaml`, mode `0600`. Multi-instance is supported:

```sh
paimos auth login --name ppm       --url https://pm.barta.cm
paimos auth login --name bytepoets --url https://pm.bytepoets.com
```

Use `--instance <name>` on any command to switch, or rely on `default_instance`.

### Verify with `doctor`

```sh
paimos doctor
#   ✓ config   ok — ppm (https://pm.barta.cm)
#   ✓ health   ok
#   ✓ auth     ok — user=mba
#   ✓ schema   ok — version=1.1.0
```

CI-safe; exit 0 all green, 1 warnings only, 2 any failure.

### Enable shell completions

```sh
# fish
paimos completion fish > ~/.config/fish/completions/paimos.fish

# zsh
paimos completion zsh > "${fpath[1]}/_paimos"

# bash
paimos completion bash > /etc/bash_completion.d/paimos
```

After `paimos schema` runs once per instance, flag values like `--status done` get tab-completion from the cached enum list.

---

## 2. Core patterns

### Issue refs accept **either** key or numeric id

Every CLI command and every `/api/issues/{id}/*` REST endpoint accepts `PAI-83` or `462` (PAI-86). Prefer keys — they're stable across instance re-imports and human-readable in logs.

```sh
paimos issue get PAI-85          # key
paimos issue get 465             # numeric — equivalent
```

### Multi-line fields are file-first

Inline `--foo "…"` is single-line only **by design**. For markdown (description, acceptance-criteria, notes, close-notes, comments): use `--foo-file path` or `--foo-file -` (stdin).

```sh
# Good
paimos issue create --project PAI --title "Fix paginator" \
  --type ticket --priority high \
  --description-file /tmp/desc.md \
  --ac-file /tmp/ac.md

# Also good — editor-driven
$EDITOR /tmp/desc.md
paimos issue create ... --description-file /tmp/desc.md

# Agent-driven with no temp files — pipe via stdin
cat <<EOF | paimos issue create --project PAI --title "Refactor X" --description-file -
# What
Long multi-line markdown here.
EOF
```

Mixing `--description` + `--description-file` on the same command is a hard error (exit 2). No silent preference.

### Always pass `--dry-run` first when scripting

Every mutation supports `--dry-run`: the CLI prints the resolved request payload (method, path, body) as JSON and exits 0 without calling the API.

```sh
paimos issue update PAI-83 --status done --close-note-file /tmp/close.md --dry-run
# {
#   "method": "PUT", "path": "/api/issues/PAI-83",
#   "body": {"status": "done"},
#   "close_note_will_comment": "contents of close.md…"
# }
```

### Prefer `--json` for anything that pipes into another tool

```sh
paimos --json issue list --project PAI --status backlog --limit 5 \
  | jq '.issues[] | {key: .issue_key, title}'
```

### Errors exit non-zero and never dump HTML

- Exit **0** success
- Exit **1** API/runtime error — in `--json`, payload is `{"error": "…", "code": 4xx}` (a WAF-returned HTML body is mapped to `"non-JSON response (proxy/WAF?)"`, never echoed)
- Exit **2** usage error (bad flags, missing config, mutually-exclusive options)

---

## 3. End-to-end transcript — a realistic session

This is a lightly edited transcript of a real `paimos` session ticking off one child of this epic. Shell commands are exactly what ran; only output was trimmed for brevity.

```sh
# 1. Start from the ticket.
$ paimos issue get PAI-89
PAI-89  D. Relations: add follows_from / blocks / related
  type:     ticket
  status:   backlog
  priority: low
  …

# 2. Branch + make changes (normal git flow).
$ git checkout -b feat/pai-89-new-relation-types
$ $EDITOR backend/db/db.go  # add migration M67
$ $EDITOR backend/handlers/issues.go  # allowlist update
$ go test ./...
# ... all green

# 3. Commit + open PR (gh, not paimos — complementary tools).
$ git commit -m "feat(relations): follows_from, blocks, related (PAI-89)"
$ gh pr create --title "..." --body "..."
# https://github.com/markus-barta/paimos/pull/15

# 4. CI runs; PR merges; docker image publishes; deploy.
$ ssh user@deploy-host "cd ~/docker && just deploy"

# 5. Smoke check the new behaviour before closing.
$ paimos --json issue get PAI-85 \
  | jq '.description' | head -c 80
# "Sibling to **PAI-29** (context-in) and **PAI-30** …"

# 6. Close the ticket with a close-note — single command.
$ cat > /tmp/pai89-close.md <<'EOF'
Shipped in v1.2.8 — merge 0f0235f.

Three new directional relation types live: follows_from, blocks, related.
Inverse display without a second row via new `direction` field on
GET /api/issues/{id}/relations. Tested live: PAI-84 follows_from PAI-40
— both sides render correctly.
EOF

$ paimos issue update PAI-89 --status done --close-note-file /tmp/pai89-close.md
✓ updated PAI-89 with close note
```

**No temp JSON files. No shell-quoted markdown. No retry-on-404 phantom tickets.** That's the whole point of the epic.

---

## 4. Bulk work

### Bulk status transitions: `batch-update`

Drop a JSONL file, run once. Handles 100 items per transaction; larger files chunk automatically.

```sh
$ cat > /tmp/ops.jsonl <<'EOF'
{"ref": "PAI-101", "fields": {"status": "accepted"}}
{"ref": "PAI-102", "fields": {"status": "accepted"}}
{"ref": "PAI-103", "fields": {"status": "accepted"}}
EOF

$ paimos issue batch-update --from-file /tmp/ops.jsonl
chunk 1: 3 items updated
3 items updated across 1 chunk(s); 0 failed
```

### Scaffolding: `apply`

Declarative YAML plan — create + relate in one command. Named refs let children reference a same-plan parent.

```yaml
# plan.yaml
project: PAI
create:
  - name: epic
    type: epic
    title: Q2 refactor
  - name: extract-auth
    type: ticket
    title: Extract auth module
    parent: epic       # → server-side parent_ref: "#0"
  - name: migrate-sessions
    type: ticket
    title: Migrate session store
    parent: epic
relations:
  - source: epic
    type: related
    target: PAI-40
```

```sh
$ paimos apply --from-file plan.yaml
✓ created 3 issues
✓ added 1 relations
```

**Not idempotent in v1** — running twice duplicates. After scaffolding, use `ensure-status` / `batch-update` for subsequent changes:

```sh
$ paimos issue ensure-status PAI-101 done
✓ PAI-101 already done       # no-op, exit 0
```

---

## 5. Schema discovery

The server publishes a single source of truth at `GET /api/schema`. The CLI caches it (`~/.paimos/schema-<instance>.json`) and uses it for tab-completions.

```sh
$ paimos schema
instance: ppm (https://pm.barta.cm)
version:  1.1.0
enum priority: low, medium, high
enum relation: groups, sprint, depends_on, impacts, follows_from, blocks, related
enum status:   new, backlog, in-progress, qa, done, delivered, accepted, invoiced, cancelled
enum type:     epic, cost_unit, release, sprint, ticket, task

$ paimos schema --refresh       # re-download + report if version moved
```

The schema includes **recommended** status transitions — enforced client-side as hints, the backend still accepts any→any so you can fix mistakes without ceremony.

---

## 6. MCP integration

### Claude Desktop

Add `paimos-mcp` to your MCP servers config:

```json
{
  "mcpServers": {
    "paimos": {
      "command": "/Users/you/go/bin/paimos-mcp",
      "env": { "PAIMOS_INSTANCE": "ppm" }
    }
  }
}
```

Tools available in the current allowlist:

| Tool                  | Notes |
|----------------------|-------|
| `paimos_schema`       | Call before choosing enum values |
| `paimos_retrieve`     | mixed project-context retrieval with fusion metadata |
| `paimos_graph`        | typed entity-graph traversal |
| `paimos_blast_radius` | grouped impact traversal for one issue |
| `paimos_search`       | global PAIMOS search endpoint |
| `paimos_issue_get`    | by key or id |
| `paimos_issue_list`   | `project_key`, `status`, etc. |
| `paimos_issue_create` | title + project_key required |
| `paimos_issue_update` | partial by ref |
| `paimos_relation_add` | all 7 types |

**Deliberately not exposed**: `batch-update`, `apply`. MCP context grows fast; agents that need bulk should shell out to the `paimos` CLI instead.

---

## 7. Failure modes (explicit)

PAIMOS deliberately makes the "what happens when X breaks" contract explicit so agents can trust it.

- **Soft-deleted issues**: status/trash-aware endpoints return 404. Key resolution (`ResolveIssueRef`) still finds them so `restore` / `purge` can target trashed tickets by key.
- **Missing issue**: 404 `{"error": "not found"}`. Never a misleading 200 with `null` body.
- **Malformed ref**: 400 `{"error": "invalid id"}`. Distinct from 404 so agents can distinguish "garbage input" from "nothing matched".
- **Server-side validation failure in a batch**: the whole batch rolls back, response is 400 with `{"errors": [{index, ref?, error}], "rolled_back": true}`. No half-commits.
- **Session audit**: off by default in v1 (see [`CHANGELOG 1.4.0`](CHANGELOG.md)). Enable with `PAIMOS_AUDIT_SESSIONS=true` on the backend. When on, every mutation is tagged with the CLI's auto-generated UUIDv7 `X-PAIMOS-Session-Id` so you can replay "what did my agent do?" via `GET /api/sessions/:id/activity`.
- **Schema version drift**: the server bumps `SchemaVersion` on any enum / transition / field change. `paimos schema --refresh` reports whether the server moved vs. your local cache.

---

## 8. When to drop down to REST

Most of the time you won't need to. Reach for the REST API directly when:

- You're writing code in a language where Go's `paimos` CLI isn't convenient (even then, shelling out is often faster than pulling in an HTTP client).
- You need an endpoint the CLI doesn't wrap yet (e.g. time entries, attachments, acceptance reports — see [`api-minimal.md`](api-minimal.md)).

If a pattern feels awkward via CLI but natural via REST — **file a ticket**. The whole point of the PAI-85 epic is that agent-facing ergonomics belong in the CLI.

---

## See also

- [`api-minimal.md`](api-minimal.md) — REST reference, canonical.
- [`DEVELOPER_GUIDE.md`](DEVELOPER_GUIDE.md) — PAIMOS dev setup.
- [`DATA_MODEL.md`](DATA_MODEL.md) — schema + entity relationships.
- [`CHANGELOG.md`](CHANGELOG.md) — recent-features / version history.
