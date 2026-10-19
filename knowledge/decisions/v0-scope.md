---
type: Decision
title: "v0 scope and definition of done"
description: "v0 is the core CLI plus a minimal plugin; done is a real edit-to-PR loop on a real consumer repository with hooks enforcing the rules."
status: stable
tags: [scope]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# v0 scope and definition of done

## Decision

**CLI in v0**

- `reliab profile validate`
- `reliab profile init`: profile skeleton, deny rules, AGENTS.md testing-rules
  section, marketplace and plugin settings. No CI files, no CODEOWNERS.
- `reliab verify --tier inner|prepr`
- adapters: JUnit XML, TRX
- `reliab report route`
- `reliab report export --format sarif|junit`
- `reliab findings classify <id> --kind <kind> --reason <text>`
- `reliab gate protected --base <ref>`
- `reliab hook pre-edit|pre-bash|stop|pre-push|session-start`
- `reliab doctor`

**Plugin in v0:** `hooks/hooks.json`, a `verify` skill, a model-only
`evidence-stack` knowledge skill, and the `bin/reliab` launcher.

**Deferred:** corpus and findings-queue bookkeeping, `canary`, spike probes,
the reviewer subagent, the nightly tier and triage workflow, plugin evals, and
the `write-property`, `spike`, `init`, `kill-mutants`, `characterize`,
`migrate` skills.

**Done:** a real change goes edit to PR on a real consumer repository with
both tiers, the hooks deny and block as designed, and reliab runs on its own
repo.
