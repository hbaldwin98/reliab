---
type: Decision
title: "Binary delivery: a launcher that fetches the pinned release"
description: "plugin/bin/reliab is a POSIX sh launcher that downloads and caches the versioned binary; install scripts do the same for any CI."
status: stable
tags: [release, plugin]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Binary delivery: a launcher that fetches the pinned release

## Context

Plugin `bin/` is committed files only: no install step, no per-OS selection.
Hooks run under Git Bash on Windows (PowerShell if Git Bash is missing), so a
POSIX sh launcher works. `${CLAUDE_PLUGIN_DATA}` is visible to hooks but not
to commands the agent runs through the Bash tool. Committing per-OS binaries
would add tens of megabytes to git history per release; `go install` would
require Go on every machine.

## Decision

- `plugin/bin/reliab` is a small sh launcher. It looks for
  `~/.cache/reliab/<version>/reliab[.exe]`; if missing, it downloads that
  version's release asset, verifies the checksum, and caches it. The
  session-start hook triggers the download early.
- `plugin/bin/reliab.cmd` sits beside it for the PowerShell tool on Windows,
  which cannot run the extensionless sh script but resolves the `.cmd`
  (checked 2026-10-17; see
  [hook-deny-under-bypass](/open-questions/hook-deny-under-bypass.md)). Both
  launchers share the same cache.
- Download source: this repo's GitHub Releases over plain HTTPS with `curl`
  or `wget`; the repository is public, so no credentials are needed. `gh` is
  optional: when it is installed and authenticated, and
  `RELIAB_DOWNLOAD_URL` is unset, the installer uses `gh release download`
  instead. `RELIAB_DOWNLOAD_URL` points at a mirror, with `RELIAB_TOKEN`
  sent as a bearer token when the mirror needs one. It may also be a local directory or a
  `file://` URL holding the assets (added 2026-10-19): PowerShell 7's
  `Invoke-WebRequest` refuses `file://`, so a mirror on a share, and testing
  `install.ps1` against a snapshot, had no way in.
- `install.sh` and `install.ps1` use the same logic for CI, reading the version
  from `.reliab-version`. Any CI can mirror the assets anywhere.
  `scripts/install.sh` and `plugin/bin/reliab-install` are the same file
  (a test keeps them identical); the launcher calls the latter.
- Release assets are `reliab_<X.Y.Z>_<os>_<arch>.tar.gz` for every OS
  (Windows included, so Git Bash's tar can extract them) plus
  `checksums.txt`. Each archive holds `reliab`, every `reliab-adapter-*`,
  the `plugin/` directory (so `claude --plugin-dir` can load a release
  without the marketplace), and the README; the installers take only the
  binaries. The binary is stamped `v{{ .Version }}`, the version the archive
  is named for, so the pin `profile init` writes names an asset the
  installers can fetch, snapshots included; `reliab version` prints it. A
  test keeps the goreleaser config, `cmd/adapters`, and the installers in
  step. (Until v0.1.0 the repository was private, plain HTTPS downloads did
  not work even with a token, and `gh` was the only working path.)
- `RELIAB_BIN` makes the launcher run a local build instead (development).
  The launcher exports `RELIAB_PLUGIN_VERSION`, which session-start compares
  with `.reliab-version`.
- `reliab doctor` checks prerequisites (the shell, Git Bash
  on Windows).
