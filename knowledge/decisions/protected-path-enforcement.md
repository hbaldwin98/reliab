---
type: Decision
title: "Protected paths: four layers of enforcement"
description: "Deny rules, a Bash heuristic hook, sandbox denyWrite where supported, and the gate command; the native-Windows gap is documented."
status: stable
tags: [enforcement, hooks]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Protected paths: four layers of enforcement

> Amended 2026-10-21 by [light-guardrails](/decisions/light-guardrails.md): pre-bash no longer inspects shell writes, and a push is reminded, not denied.

## Context

Checked against the Claude Code docs on 2026-10-17: `Edit(...)` deny rules
cover Edit and Write and Bash redirects and `tee`, but not `sed -i`, `cp`, `mv`,
or `sh -c`. Only the sandbox's `filesystem.denyWrite` blocks those, and the
sandbox does not run on native Windows (it does on macOS, Linux, and WSL2).

## Decision

1. `reliab profile init` writes an `Edit(...)` deny rule for every protected
   path and for `.reliab/**`. `Edit` rules are what Claude Code checks for
   every file-editing tool (Edit, Write, NotebookEdit) and Bash redirects;
   `Write(path)` rules are accepted but never consulted (docs checked
   2026-10-17). Globs starting `**/` match at any depth; others get a leading
   `/`, which anchors them at the directory Claude Code starts in. These
   protect [R2](/rules/r2-protected-oracles.md) oracles.
2. A PreToolUse Bash hook (`reliab hook pre-bash`) denies `sed -i`, `cp`, `mv`,
   `rm`, `git checkout`, `git restore`, and similar commands naming a protected
   path. Heuristic. Since 2026-10-20 it reads each line once as its shell
   does, so quoting, escapes, continuations, and `bash -c` do not hide a
   write ([command-effects](/decisions/command-effects.md)).
3. `init` also writes `sandbox.filesystem.denyWrite` entries (`./<dir>` for
   protected globs of the form `<dir>/**`) but never enables the sandbox:
   enabling it sandboxes every command and starts network access from an
   empty allow-list, which is the user's decision. The entries take effect
   only where the user turns the sandbox on.
4. `reliab gate protected --base <ref>` is the agent-free backstop
   ([ci-agnostic](/decisions/ci-agnostic.md)). Since 2026-10-18 it accepts
   a change whose content matches a person's local `reliab approve`, and
   lists it apart ([protected-path-approval](/decisions/protected-path-approval.md));
   on CI there are no approvals, so it fails as before.
5. **`protected_commands`** (added 2026-10-18): regular expressions for
   shell commands that approve or rewrite an oracle without naming its path.
   The pre-bash hook denies them, and session start lists them with the
   rule. Found in step 2's acceptance on a consumer repository: insta
   approves a snapshot with `cargo insta accept` or `INSTA_UPDATE=always
   cargo test`, which name no protected file, so the path heuristic could
   not see them, unlike a tool whose approval is copying a file. The profile declares them, so the
   core stays tool-neutral; a pattern that does not compile is a config
   error.
6. **`protected_edits`** (added 2026-10-18): regular expressions for text an
   agent may not add to any file. The pre-edit hook denies an Edit, Write,
   MultiEdit, or NotebookEdit whose new text has more matches than the text
   it replaces (for Write, the file as it is), so editing near an existing
   match is allowed. Found checking a .NET service: the Verify library
   accepts snapshots when a test calls `AutoVerify()`, so an edit to test
   code silently rewrote a protected `.verified.` file during the next test
   run, and no path or command rule could see it. A hook can only see the
   text an agent writes: a test can still opt into such behavior at runtime
   by other means (configuration, an environment the test reads, a helper
   already in the code), so `reliab gate protected` stays the backstop.
7. **A directory holding an oracle** (added 2026-10-18): a command that
   rewrites or removes a whole directory (`rm`, `rmdir`, `git checkout`,
   `restore`, `rm`, `clean`, `reset`, `Remove-Item`) is denied when an
   argument of its own simple command is a directory holding a protected
   file, such as `git checkout main -- tests/Snapshots` with
   `**/*.verified.*` protected. Other write verbs only write into a
   directory they name, so `mv notes.txt tests/Snapshots/` is allowed.

## Consequences

On native Windows only layers 1, 2, and 4 apply. The README says so.
