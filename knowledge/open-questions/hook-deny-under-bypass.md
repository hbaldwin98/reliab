---
type: OpenQuestion
title: "Does a hook deny hold when permissions are bypassed"
description: "Closed without a spike: the design no longer depends on the answers; the Windows launcher question was settled by a local check."
status: deprecated
tags: [hooks, spike]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Does a hook deny hold when permissions are bypassed

**Closed 2026-10-17.** The three questions and how each was resolved:

1. *Does a PreToolUse `permissionDecision: "deny"` hold when the user bypasses
   permissions?* Not answered and not needed: CI gates are authoritative and
   in-session enforcement is best-effort
   ([enforcement-hooks-self-contained](/decisions/enforcement-hooks-self-contained.md)).
2. *When exactly does an `if` filter fall back to running the hook?* Not
   needed: enforcement hooks no longer use `if` filters (same decision).
3. *Does the plugin `bin/` sh launcher run on Windows?* Checked locally on
   Windows 11 on 2026-10-17 with a directory prepended to `PATH`: from Git Bash
   the extensionless sh script runs; from PowerShell the sh script is not
   runnable and a sibling `.cmd` file is resolved and runs instead. The plugin
   therefore ships both
   ([binary-distribution](/decisions/binary-distribution.md)).
