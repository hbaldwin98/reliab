---
type: Decision
title: "Adapters emit unclassified findings; the agent classifies"
description: "Every adapter finding starts unclassified; reliab findings classify records the kind; hooks block on unclassified."
status: stable
tags: [report, hooks, departure]
generated:
  by: claude-code/claude-opus-5-5
  at: 2026-10-17T14:45:25-07:00
---

# Adapters emit unclassified findings; the agent classifies

## Context

A format adapter can see that a test failed but not why. The paper has the
agent classify failures ([R4](/rules/r4-reproduce-shrink-classify.md)), but the
findings bookkeeping that would hold the classification is deferred from v0.

## Decision

- Adapters always emit findings with `kind: unclassified`, a reliab addition
  to the paper's [finding kinds](/concepts/finding-kind.md).
- `reliab findings classify <id> --kind <kind> --reason <text>` is in v0 and
  writes the classification into the report and its history copy
  ([report-storage](/decisions/report-storage.md)). A finding fixed before
  it was classified is in no latest report; `classify` then records it on
  the newest history report that holds it (added 2026-10-18), so the PR
  summary's fixed list shows its kind and reason.
- Hooks treat `unclassified` and `new-code-defect` as blocking.
  `characterization-diff`, `legacy-defect-or-quirk`, `equivalent-mutant`,
  `requirement-silent`, and `environment` block until a human acknowledges
  them.
- `equivalent-mutant` (added 2026-10-18) records R7's one-line reason for a
  surviving mutant no test can kill. It is a human kind: the agent stops, and
  a person acknowledges it or excludes the mutant in the mutation tool's
  config, which is a protected path. Only findings from `mutation_*`
  commands can take it.
- `requirement-silent` (added 2026-10-18) is for a surviving mutant whose
  behavior is real but stated nowhere. Killing it would pin incidental
  behavior: a change-detector test that raises the score and catches no
  real bug. The agent records the question instead and stops; a person says
  whether the behavior is intended, and then a test citing that answer pins
  it, or the code changes. This is the paper's case-study survivor left as a
  question because the ticket was silent. Findings from `mutation_*`
  commands can take it, and since 2026-10-18 so can findings from the
  property and fuzz commands (`properties`, `model_tests`, `api_fuzz`): a
  generated input the cited source says nothing about (no minimum quantity
  stated, say) is the same question. Found onboarding a .NET repository,
  where such a finding could only be filed `legacy-defect-or-quirk`, whose
  question ("keep this pre-existing behavior?") is wrong for behavior new on
  the branch. `reliab report route` gives those commands' unclassified
  findings advice that names the choice. Since 2026-10-19 so can findings
  from the fault commands (`faults_db`, `faults_http`): what the system
  should do when a store or a dependency fails is often stated nowhere, and
  a fault test may have assumed a side. Found on a consumer repository,
  where such findings were filed `legacy-defect-or-quirk` with the
  question in the reason. It stays a human kind; `equivalent-mutant` stays
  mutation-only.
- `environment` (added 2026-10-18) is for a failure the machine caused,
  not the code: out of consoles, memory, or disk, the network, a service
  the tests need. The agent may use it only after re-running the failing
  command once, and quotes the machine's error in the reason; a failure it
  cannot explain is not `environment`. It is a human kind: a person fixes
  the machine. **It does not carry to the next run**, unlike every other
  classification: once the machine is fixed, a failure there is judged
  afresh, so a stale "it was the machine" cannot hide a real defect. Found
  on 2026-10-18, when Windows ran out of consoles during a mutation run and
  agents filed the resulting pty failures as legacy quirks.
- **A command-level finding carries only while its failure is the same**
  (added 2026-10-19). A finding with no test, `F-<command>`, is made from a
  failing exit code (or a tool that failed without reporting a test), so it
  has one id however the command fails. verify stores a `fingerprint` with
  it: a hash of its message and observed output with run-to-run noise
  removed (line endings and trailing spaces, the repository root and the
  temp directory, other absolute paths, timestamps, durations, memory
  addresses). Its classification and acknowledgement carry to the next run
  only when the fingerprint matches; otherwise it comes back open and
  unclassified, with `reopened` saying it changed since it was classified
  or acknowledged, and `reliab report route` prints that. An earlier report
  with no fingerprint (before report 0.9.0) cannot show the failure is the
  same, so nothing carries from it, and `reopened` says so. Findings with
  their own test keep their classification by id, as before. Found in the
  adapter acceptance runs: a linter wired by exit code reported every
  problem as `F-analyzers`, so a person's acknowledgement of one
  pre-existing problem covered every later one.
- `below-threshold` (added 2026-10-18) is a mutation score short of the
  profile's threshold or a baseline scope's score. verify derives it from
  the counts and the survivors; it is never classified, routes the agent to
  its survivors while they need it, then stops for a person, who
  acknowledges it or says what to test
  ([mutation-threshold](/decisions/mutation-threshold.md)).
- **A mutant-killing test cites its intent (R3 applies).** Its comment names
  where the mutated behavior is stated: a doc, a ticket, a rule, or a caller
  that depends on it. The implementation is never the source.
