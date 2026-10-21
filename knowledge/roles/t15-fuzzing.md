---
type: Role
title: "T15: Coverage-guided fuzzing"
description: "Mutate inputs to code that parses or decodes, guided by coverage, within a time box; save each crashing input as a reproducer. reliab's own role, optional."
tags: [tool-role]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-21T12:00:00-07:00
---

# T15: Coverage-guided fuzzing

**Must:** run fuzz targets for a time box it is given, keep a corpus between
runs, and save each failing input where a re-run replays it.

**Example fillings (illustrative, unverified):** Go's `go test -fuzz`,
cargo-fuzz (libFuzzer), Atheris, Jazzer, SharpFuzz.

The paper's roles stop at T14 and fuzz only an API from its spec (T7).
reliab adds T15 for code below the API that takes untrusted bytes: parsers,
decoders, protocol readers ([fuzz-and-e2e](/decisions/fuzz-and-e2e.md)). A
profile may leave it out; when it lists it, it fills it or marks it
`applicable: false` with a reason, as with every role.
