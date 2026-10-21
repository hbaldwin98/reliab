---
type: Decision
title: "reliab's own repo dogfoods and releases with goreleaser"
description: "This repo's GitHub Actions workflow runs reliab on itself and publishes cross-platform binaries and checksums on tags."
status: stable
tags: [release, ci]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# reliab's own repo dogfoods and releases with goreleaser

## Decision

reliab's own repository (on GitHub) has a GitHub Actions workflow that runs
reliab on itself and, on a `vX.Y.Z` tag, uses goreleaser to publish binaries
for Windows, macOS, and Linux (amd64 and arm64) plus checksums, and stamps the
plugin `version` ([versioning-lockstep](/decisions/versioning-lockstep.md)).
This is reliab's own CI, not a template for users
([ci-agnostic](/decisions/ci-agnostic.md)). Its checks and the release gate
are in [own-ci-checks](/decisions/own-ci-checks.md).
