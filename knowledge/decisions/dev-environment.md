---
type: Decision
title: "Developing reliab in its own repo: dev reviewer, cheap warning hooks, a per-checkout build"
description: "reliab's own .claude/ settings give every session the reviewer as a project agent (a byte copy of the plugin's), a PreToolUse hook that warns, never blocks, on shell writes to .go and .md files, a SessionStart hook that builds the checkout's reliab onto PATH, and PYTHONUTF8=1. Prepr and the review come before a branch merges into main, by a PR or locally, with the change committed first. Repocheck tests keep the copies in step."
status: draft
tags: [process, dev, hooks, reviewer, windows]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-20T19:00:00-07:00
---

# Developing reliab in its own repo: dev reviewer, cheap warning hooks, a per-checkout build

## Context

Agents develop reliab inside this repository, often several at once in git
worktrees, on Windows. A retrospective of that work found:

- The rules said to have the `reliab:reviewer` agent review a change, but
  sessions here often had no such agent: the project settings enable the
  plugin from the marketplace, and a session where it is not installed
  loads neither its agents nor its hooks.
- Agents merged branches into main locally, without a PR, so "prepr before
  opening a PR" never fired; and they invoked the reviewer on uncommitted
  work, which it refuses.
- Shell writes (python, perl, `sed -i`, `cat >` and heredocs) bypassed the
  editor tools and left CRLF, control bytes, and mangled encoding in `.go`
  and `.md` files.
- `reliab` is not on PATH in a worktree, and the released binary is not the
  code under change, so agents ran `go run ./cmd/reliab` each time or the
  wrong build.
- Python on Windows writes in the system code page.
- Hand-synced copies drifted: AGENTS.md's rules and templates/agents.md,
  the adapters listed in the docs.

The owner's standing priority weighs every choice: reliab must be fast in
its workflow, and its guardrails must not be infuriating. A false positive
in a hook is as serious as a bypass.

## Decision

- **Prepr and the review come before a branch merges into main**, by a PR
  or a local merge, in AGENTS.md and the template profile init writes. The
  author commits first; the reviewer's description says it reviews
  committed work only.
- **The reviewer is also a project agent here**:
  `.claude/agents/reviewer.md` is a byte copy of
  `plugin/agents/reviewer.md`, and a repocheck test fails when they differ.
  It is the review, not the verdict, that this makes reliably available: a
  project agent's `agent_type` is its bare name (`reviewer`), and the
  pre-bash guard allows `reliab report verdict` only to `reliab:reviewer`
  ([reviewer-verdict](/decisions/reviewer-verdict.md)). Where the plugin is
  installed its hooks deny the copy the verdict and name `reliab:reviewer`,
  which is then available; where it is not, no reliab hook runs at all.
  Accepting `reviewer` in the guard is left to the owner (it would let any
  project agent of that name record a verdict in every consumer
  repository).
- **A PreToolUse hook warns on shell writes to `.go` and `.md` files**
  (`.claude/hooks/warn-shell-writes.sh`, Bash and PowerShell). It returns
  `additionalContext` with exit 0 and no permission decision, so the
  command runs as before and the agent reads the warning. It reads the
  command with shell builtins and spawns one `grep` only after a cheap
  match: about 70 ms a call on Windows, nearly all of it starting `sh`.
  To keep false positives down it ignores text after `git commit`, `git
  tag`, `gh pr`, and `gh issue`, and a heredoc's body; it can still warn on
  such text elsewhere, which costs a line of context, never a blocked
  command.
- **A SessionStart hook builds the checkout's reliab**
  (`.claude/hooks/dev-session-start.sh`) into `.devbin/` (gitignored) and
  appends `.devbin` to PATH through `CLAUDE_ENV_FILE`. `go build` is the
  staleness check: on a current binary it writes nothing and takes about
  0.3 s, and it sees every input (embedded files, `go.mod`) a hand-written
  check could miss. Without `go` it only sets PATH; a failed build tells the
  agent to use `go run ./cmd/reliab`; it always exits 0.
- **`PYTHONUTF8=1`** in the project settings' `env`.
- **docs/windows.md** lists the Windows gotchas; AGENTS.md points to it,
  outside the reliab block.
- **Repocheck tests** keep AGENTS.md's reliab block equal to
  templates/agents.md, every `cmd/adapters` directory named in the README
  and docs/adapters.md (the release config's builds were already checked),
  and every project hook script present; the hook scripts are tested with
  sample inputs.

## Consequences

- Everyone who opens the repository gets these hooks; they need Git Bash on
  Windows (`sh`). A missing `sh` or script errors without blocking.
- The PowerShell tool may not see the PATH the SessionStart hook sets.
- Hook scripts are not Go inputs, so their tests read the script to make
  `go test`'s cache key on it.
- `reliab profile init` writes the new trigger into consumers' AGENTS.md
  when they re-run it.
