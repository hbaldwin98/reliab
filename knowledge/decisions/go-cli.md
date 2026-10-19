---
type: Decision
title: "The CLI is written in Go"
description: "Single static binaries per OS, fast hook startup; latest stable Go (1.27.2) with cobra and minimal dependencies."
status: stable
tags: [cli, tooling]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# The CLI is written in Go

## Context

The CLI must be a single binary that runs on Windows, macOS, and Linux, starts
fast enough to answer hooks, and installs pinned in any CI without a runtime.
A .NET tool would tie a stack-neutral tool to one stack; Node or Python need a
runtime on every CI agent; Rust builds slower.

## Decision

- Go, latest stable at the time of writing: `go 1.27` with
  `toolchain go1.27.2` in `go.mod` (older local Go auto-downloads it).
- cobra for the subcommand tree.
- Minimal dependencies: a YAML library, a JSON Schema validator, the standard
  library for the rest.
