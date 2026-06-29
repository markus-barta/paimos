# Pickup → moved to the ppm knowledge plane

The session-pickup / continuation state now lives as a **runbook** in ppm, not in this
repo. This file is just a pointer so the canonical handoff doesn't drift in two places.

**Read it:**

```sh
paimos knowledge get runbook pickup --project PAI       # add --json for raw
```

Web UI: **pm.barta.cm** → project **PAI** → Knowledge → _Session Pickup / Continuation State_
(`runbook`, slug `pickup`, #2418).

**Update it** at the end of a working session:

```sh
paimos knowledge update runbook pickup --project PAI --body-file -   # pipe new markdown
```

(don't re-add state to this file).
