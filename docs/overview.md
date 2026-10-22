# reliab in this repository

reliab is a testing CLI and a Claude Code plugin. It runs the repository's
own tests and tools in tiers, each within a time budget, and turns each
tool's output into findings in one report. The tools, commands, and tiers
are in `reliability-profile.yaml`; reliab knows nothing about the stack
beyond what that file says. It never replaces a test framework: it runs the
one the repository has.

## The evidence stack

No single technique proves code correct, so reliab runs independent layers,
each with its own oracle. Agreement is strong evidence; disagreement is a
finding.

| Layer | Question | Profile commands |
|---|---|---|
| 0 Static analysis | Is it obviously wrong? | `analyzers` |
| 1 Characterization, regression corpus | Did existing behavior change? | `snapshots`, `characterize` |
| 2 Specification | Does it do what was intended? | `properties`, `fuzz`, `api_fuzz` |
| 3 Mutation | Would the tests catch a bug here? | `mutation_changed`, `mutation_full` |
| 4 Failure behavior | Does it fail safely? | `faults_http`, `faults_db`, `store_tests` |
| 5 Differential replay | Does new code match old on real inputs? | (a planned rewrite only) |

`build`, `unit`, `integration_tests`, `e2e`, and `coverage` run beside
them. Which commands exist here, and which roles are not applicable and
why, is in the profile.

## What to run when

| Moment | Run | Plugin |
|---|---|---|
| Adopting reliab (stage S0) | `reliab profile init`, then the onboarding steps | `/reliab:onboard` |
| A change is done | `reliab verify --tier inner` (about 5 minutes) | `verify` skill |
| Before a branch merges into main | `reliab verify --tier prepr` (about 15 minutes), commit, then the reviewer | `verify` skill, `reliab:reviewer` agent |
| Writing a PR description | `reliab report summary` | `/reliab:summary` |
| After any run | `reliab report route`, then classify each finding | `verify` skill |
| How strong are the tests for an area? | `reliab verify --only mutation_full --scope <path>` | `/reliab:mutation` |
| Tests from stated intent | `reliab verify --only properties --scope <path>` | `/reliab:properties` |
| Harden a parser or decoder | `reliab verify --only fuzz --scope <path>` | `/reliab:fuzz` |
| A refactor or migration | `reliab characterize capture`, change, `compare`, `discard` | `/reliab:characterize` |
| A tool reliab cannot read | an adapter that writes reliab's findings file | `/reliab:adapter`, `reliab docs adapters` |
| A symptom whose cause the tests' output does not show | `reliab debug run` with breakpoints and dumps (needs the profile's `debug` section) | `/reliab:diagnose`, `reliab docs debug` |
| On a schedule | `reliab verify --tier nightly` (no budget) | |
| Something is misconfigured | `reliab doctor` | |

`verify --only` writes an on-demand report; it never stands in for the
inner or pre-PR report.

## After S0

Onboarding stops at the S0 gate. What leads next depends on the profile's
`starting_condition`:

- **maintained**: a mutation baseline over the existing suite. Run
  `reliab verify --only mutation_full --scope <area> --tier nightly` per
  area, then `reliab baseline propose --scope '<area>/**'`; a person runs
  `reliab baseline write` and commits the file. From then on, mutation
  scores must not fall below it. Survivors on changed lines drive new tests.
- **legacy**: characterization first. Pin what each area does today
  (snapshots, or `characterize` around each change); questions of legacy
  versus intended behavior go to a product owner.
- **greenfield**: properties and mutation from the first endpoint.

Then wire CI: `reliab docs ci`.

## Findings

Every failure, surviving mutant, or crash is a finding with a kind. The
agent resolves `new-code-defect` (fix the product code), `harness-defect`
(fix the test harness), and `wrong-property` (delete it, say why). A person
decides `legacy-defect-or-quirk`, `characterization-diff`,
`equivalent-mutant`, `requirement-silent`, `environment`, and
`below-threshold`. Classify with
`reliab findings classify <id> --kind <kind> --reason "..."`.

## A person's, not an agent's

- Changes to protected paths (`protected_paths` in the profile, including
  the profile itself): a person runs `reliab approve <path> --by human:<name>`.
- `reliab findings ack`, `reliab baseline write`.
- `reliab report verdict` is the `reliab:reviewer` agent's alone.

The hooks remind and never block; the CI gates are authoritative.

## Exit codes

`0` passed and complete, `1` open findings, `2` config error (run
`reliab doctor`), `3` incomplete (the budget ran out, or a command needs
tests named with `--tests` or `--mutation-tests`).
