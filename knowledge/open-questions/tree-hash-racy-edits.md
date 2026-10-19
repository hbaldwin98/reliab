---
type: OpenQuestion
title: "Can the tree hash miss a same-size edit"
description: "Closed: yes, it could. The temporary index now keeps the real index's mtime, so git's racy-entry check sees the edit."
status: deprecated
tags: [freshness, gitx, legacy]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-18T17:45:00-07:00
---

# Can the tree hash miss a same-size edit

Seen on 2026-10-18 during step 3's acceptance: the reviewer subagent's
first clean-worktree `reliab verify --tier prepr` reported a finding from
`internal/gitx` `TestTreeHash` (the `"an edit did not change the hash"`
check), while gremlins loaded the machine. The test then passed 30 times
alone, and a second re-run was clean.

The test rewrites `a.txt` from `a` to `b`, same size, right after
committing it. [TreeHash](/decisions/report-freshness.md) seeds a temporary
index from the real one and runs `git add --all`. If git trusts the copied
index's stat data for that file (same size, same mtime second, and the
copy's own newer mtime hides git's racy-entry check), it keeps the old blob
and the hash does not change.

If that is the cause, an agent edit of the same size within a second of a
commit or `git add` could leave a report looking fresh, and the stop hook
and push gate would accept it. Unconfirmed: nobody has reproduced it on
purpose yet. The code predates step 3, so it is classified as a
legacy defect or quirk for a person to decide: fix it (for example
`git update-index --really-refresh` on the temporary index, or not seeding
it), or accept the window.

## Answer (2026-10-18)

Yes. Reproduced on purpose by `TestTreeHashSeesASameSizeEditInTheIndexSecond`
in `internal/gitx`: with the file and the index stamped in the same second,
a same-size edit left the hash unchanged every time. Copying the index gave
the copy a new mtime, so git judged the entry clean from its stat data and
never re-read it. `TreeHash` now gives the copy the real index's mtime, so
git re-reads entries modified in that second, as it would on the real index
([report-freshness](/decisions/report-freshness.md)).
