---
type: Source
title: "Evidence, Not Assurance: An Evidence Stack and Agent Contract for AI-Assisted Reliability Testing of Services"
description: The method paper reliab implements; summarised here for its method only.
tags: [paper, method]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Evidence, Not Assurance (technical report, 17 October 2026)

The PDF is **not** committed to this repository; see
[context-agnostic](/decisions/context-agnostic.md). This concept summarises
the method only. Concepts cite it by section number.

## Method in one paragraph

AI coding agents write tests that assert what code does rather than what it
should do, and repair loops turn bugs into regression tests. The method gives a
service independent oracles that fail independently (the
[evidence stack](/concepts/evidence-stack.md), §3), a
[starting condition](/concepts/starting-condition.md) that decides which layer
leads (§3.1), fourteen [tool roles](/roles/) that separate the method from any
stack (§4), a verification protocol that checks each role filling on the real
codebase because package metadata is an unreliable oracle for runtime support
(§4.2), an agent contract of three [tiers](/concepts/tier.md), one report
schema, nine [hard rules](/rules/) and a triage table (§6), and a plugin
anatomy that makes the contract enforceable (§7).

## Sections cited by this bundle

- §3 Starting conditions and the evidence stack (Tables 1–2, Figure 1)
- §4 Tool roles and the verification protocol (Table 3)
- §5 Evidence from prior deployments; §5.10 summary of design rules (Table 4)
- §6 The agent contract: tiers (Table 5), loop (Figure 11), hard rules R1–R9
  (§6.1), report contract (§6.2)
- §7 Plugin anatomy: the CLI (§7.1), skills (§7.2, Table 6), enforcement
  (§7.3, Table 7), agents and workflows (§7.4), evals (§7.5), plugin split
  (§7.6)
- §8 Agent-free CI safety nets (Table 8)
- §9 Platform moves: decoupling principles P1–P8 and the phase skeleton
- §11 Rollout: adoption stages S0–S6 (Table 14) and what to measure
- §12 Limitations and open questions
- Appendix A: profile skeleton, AGENTS.md template, hook configuration,
  repository settings, reviewer subagent

## Claims from the paper reliab relies on, and their status

The paper's Claude Code mechanics (hook `if` filters, PreToolUse deny,
`stop_hook_active` and the 8-block cap, plugin `bin/`, subagent worktree
isolation, skill invocation control, plugin evals) were checked against the
Claude Code docs on 2026-10-17 and confirmed. Gaps found in that check are
recorded in [protected-path-enforcement](/decisions/protected-path-enforcement.md)
and [hook-deny-under-bypass](/open-questions/hook-deny-under-bypass.md).

The paper describes its plugin as "a design, not yet a measurement" (§12).
reliab treats it as design intent: see
[paper-as-design-intent](/decisions/paper-as-design-intent.md).
