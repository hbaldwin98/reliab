---
name: properties
description: Write property-based tests for one area of the code from its stated intent, run them through reliab, and handle what they find. Use when asked to add properties, property tests, or specification tests for a path in a repository with reliability-profile.yaml.
argument-hint: <path>... [intent or source]
---

# Properties from intent, on demand

The person names an area (paths) and may state the intent or point at its
source. A property says what must hold for every input, so it is only as
good as the intent it encodes.

## 1. Gather intent before reading the code

Collect what the area is supposed to do, from the person's words, a ticket,
a doc, a reviewed rule card, or a caller that depends on the behavior.
Write the candidate properties down from these sources first. Only then read
the implementation, to learn its inputs and how to call it. Tests written
from the implementation pin what it does, not what it should do.

If no source says what should hold, ask the person. Do not invent intent.

## 2. Write the properties

- Use the profile's T2 tool (`roles.T2` in `reliability-profile.yaml`). If T2
  is not applicable, stop and say so.
- Put them in the profile's test paths, never in protected paths.
- Each property cites its source in a comment: the ticket, doc, rule card,
  or caller.
- Read the seed and case count from the environment reliab sets
  (`RELIAB_SEED`, `RELIAB_PROPERTY_CASES`, or the profile's `{seed}` and
  `{cases}`) when the tool supports it.

Before running, check each property can fail: it asserts something, swallows
no errors, and its generator reaches the interesting cases (boundaries,
empty and large inputs, the cases the source calls out). Label or count
generated cases when the tool allows it.

## 3. Run them through reliab

Use `properties` if the profile defines it, else the command that runs the
new tests (usually `unit`):

```sh
reliab verify --only properties --scope <path> [--seed <seed>]
```

The run writes `.reliab/reports/on-demand-latest.json`; it never stands in
for the inner or pre-PR report. Then run `reliab report route`.

## 4. Handle each failure

Reproduce it from its seed, shrink it to a minimal case, and classify it:
`reliab findings classify <id> --kind <kind> --reason "<one line>"`.

- `wrong-property`: the source does not say this. Delete the property and
  write down why.
- `harness-defect`: the generator or setup is wrong. Fix it.
- `new-code-defect`: code changed in this work breaks the intent. Fix the
  product code, never the property.
- `legacy-defect-or-quirk`: behavior that predates this work contradicts
  the source. Stop; a person decides.
- `requirement-silent`: the source says nothing about this input (no limit
  stated, say), so neither the code nor the property is wrong by it. Do not
  pick a side; record the question in the reason and stop. A person says
  what is intended. Use it for behavior new on the branch too; it is not a
  legacy quirk.

Never weaken a property or narrow its generator to make it pass.

Classify before you fix: the fix makes the finding leave the next report,
and the PR summary's fixed list shows the kind and reason you recorded.

If the property tool saved the failing case as a file under a protected
path (a regression seed), commit it with the fix: it keeps the failure
fixed. You may not approve it. `reliab gate protected` and the PR summary
list it as a question for a human until a person runs
`reliab approve <path> --by human:<name>`; hooks deny that command to you.
Tell the user it waits for them.

Optionally, to show the properties have teeth, run the mutation skill on the
same area.

## 5. Finish

Run `reliab verify --tier inner` and act on it as the verify skill says.
Report each property and its source, what ran (command, scope, seed, case
count), every failure and how it was classified, anything skipped and why,
and every question for a person.
