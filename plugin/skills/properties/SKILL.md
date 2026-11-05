---
name: properties
description: Write property-based tests for one area of the code from its stated intent, run them through reliab, and handle what they find. Use when asked to add properties, property tests, or specification tests for a path in a repository with reliab.yaml.
argument-hint: <path>... [intent or source]
---

# Properties from intent, on demand

The person names an area (paths) and may state the intent or point at its
source. A property says what must hold for every input, so it is only as
good as the intent it encodes.

## 1. Gather intent before reading the code

Collect what the area is supposed to do, from the person's words, a ticket,
a doc, or a caller that depends on the behavior. Write the candidate
properties down from these sources first. Only then read the
implementation, to learn its inputs and how to call it. Tests written from
the implementation pin what it does, not what it should do.

If no source says what should hold, ask the person. Do not invent intent.

## 2. Write the properties

- Use the property tool the repository already uses (rapid, FsCheck,
  fast-check, Hypothesis, proptest). If it has none, ask before adding one.
- Put them beside the area's existing tests, so the command that runs those
  tests runs them too.
- Each property cites its source in a comment: the ticket, doc, or caller.

Before running, check each property can fail: it asserts something, swallows
no errors, and its generator reaches the interesting cases (boundaries,
empty and large inputs, the cases the source calls out). Label or count
generated cases when the tool allows it.

## 3. Run them through reliab

```sh
reliab run change --target <path> --plan
reliab run change --target <path>
```

The plan shows which command runs the new tests. If none does, say so. The
full log is `.reliab/run/<command>.log`.

## 4. Handle each failure

Reproduce it from its seed: the run prints it, and `reliab run <context> --seed <n>`
reruns with the same random inputs (commands get it as `RELIAB_SEED`; pass it
to the property tool, e.g. `-rapid.seed=$RELIAB_SEED`). Shrink it to a minimal case, and decide what it
is:

- **The property is wrong:** the source does not say this. Delete it and
  write down why.
- **The generator or setup is wrong (fix-test):** fix it.
- **Code changed in this work breaks the intent (fix-code):** fix the
  product code, never the property.
- **Behavior that predates this work contradicts the source (ask-human):**
  stop; a person decides.
- **The source says nothing about this input (ask-human):** neither the
  code nor the property is wrong by it. Do not pick a side; write the
  question down and stop. A person says what is intended.

Never weaken a property or narrow its generator to make it pass.

If the tool saved the failing case as a file (a regression seed), commit it
with the fix: it keeps the failure fixed. Never delete one to make a run
pass.

Optionally, to show the properties have teeth, run the mutation skill on the
same area.

## 5. Finish

Run `reliab run change`. Report each property and its source, what ran
(command line, target, seed, case count), every failure and what you
decided it was, anything skipped and why, and every question for a person.
