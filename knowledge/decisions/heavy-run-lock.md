---
type: Decision
title: "One heavy run per machine: a lock a second heavy run waits on"
description: "A full prepr or nightly run, or any run with a mutation command, takes a machine-wide lock in reliab's user cache directory before its budget starts. A second heavy run waits, saying once who holds the lock (pid, tier, repository, start); the operating system frees the lock when its holder exits; a run never waits on its own process or on the run whose command started it; light runs take no lock."
status: draft
tags: [verify, tiers, mutation, performance]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-20T20:00:00-07:00
---

# One heavy run per machine: a lock a second heavy run waits on

## Context

Mutation and pre-PR runs are the expensive ones. Several agents on one
machine, each in its own worktree, started them at the same time: the runs
shared the CPUs, tests timed out, budgets ran out, and a mutant whose tests
timed out read as killed, so scores moved with the load
([external-adapters](/decisions/external-adapters.md),
[mutation-threshold](/decisions/mutation-threshold.md)). Running them one
after another finishes sooner than running them together, and gives the
same counts each time.

## Decision

- **Which runs are heavy:** a full `prepr` or `nightly` tier, and any run
  whose commands include a `mutation_*` command (an inner tier that runs
  one, `verify --only mutation_full`). An inner run without one and an
  on-demand run of light commands take no lock and touch no file for it:
  the edit loop never waits.
- **The lock** is a file, `heavy.lock` in `reliab` under the user cache
  directory (`os.UserCacheDir`), locked with the operating system's file
  lock (`flock`; `LockFileEx` on Windows) and holding a record of its
  holder: pid, repository, tier, and the time it took the lock (not the
  time it began waiting, so a run that queued does not look hung to the
  next waiter). The operating system
  frees the lock when its holder exits, killed or crashed included, so a
  dead holder is taken over at the next try with nothing to clear by hand
  and no pid that a new process could reuse.
- **A second heavy run waits**, trying every half second, and prints once
  per holder who holds it ("waiting for another heavy run to finish (pid
  4242, prepr in /src/repo, since ...)"), then how long it waited once it
  has the lock. It never waits silently. Ctrl-C or the caller's
  cancellation ends the wait. There is no wait limit: queueing behind a
  run beats thrashing beside it, and the line says whom to stop.
- **The wait is not part of the budget.** The budget starts once the lock
  is held, so a queued prepr does not run out of time it never had.
- **A run never waits on itself.** Two heavy runs in one process share the
  lock, which is held until the last of them ends. A heavy run passes the
  holder's pid to its commands as `RELIAB_HEAVY_LOCK`, so a `reliab
  verify` that a profile command starts goes ahead under its parent's lock
  rather than deadlocking.
- **Tests take no lock** unless they name a lock directory
  (`Options.LockDir`): a Go test binary (`testing.Testing`) skips it, so
  packages that test verify in parallel, or a prepr's own `go test`, do not
  queue behind each other or behind a real run.

## Consequences

- Five worktrees asking for prepr at once run one after another; each
  prints whom it waits for.
- A heavy run on a machine with a hung (not dead) heavy run waits until a
  person stops it; the waiting line names the pid to stop.
- The lock is per machine, not per repository: two unrelated repositories'
  mutation runs also queue, which is the point, since they share the CPUs.
