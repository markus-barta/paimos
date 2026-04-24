# PAI-122 — Local AI claim: wording rollback

The 2026-04-24 security audit (`audit.md`) flagged the website's
"Local AI-ready integrates with Ollama, LM Studio, vLLM, llama.cpp"
copy as not yet supportable from the audited codebase. The audit gave
two acceptance paths: implement the integration, or roll back the
wording. This ticket takes the rollback path so the public claim
matrix matches what is actually shipped.

## Action

Update `paimos.com` `/03 / specs` to replace the current bullet:

> **Local AI-ready** integrates with Ollama, LM Studio, vLLM, llama.cpp

with this:

> **Local AI roadmap** — PAIMOS is designed to run alongside locally
> hosted inference. Direct integrations with Ollama, LM Studio, vLLM
> and llama.cpp are on the roadmap; the current build does not ship
> any of them.

`paimos.com` lives in a separate repo, so this change happens at
website-deploy time. Once the bullet has been updated, mark `PAI-122`
done and move the row in `docs/claim-matrix.md` (PAI-123) to "matches
shipped".

## Why not implement it now

A real local-AI integration spans:

- a transport / token-streaming layer
- model selection + prompt-template mapping
- per-project / per-user runtime selection
- an evaluation harness
- documentation for each runtime

That is several weeks of focused product work and is outside the
8.5/10 readiness target this epic is aimed at. Re-implementing it
later is straightforward; lying about it today is not.
