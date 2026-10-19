# Concepts

* [Evidence stack](evidence-stack.md) - Layers of evidence, each answering one question with its own independent oracle.
* [Finding kind](finding-kind.md) - The triage classification every failure receives, which decides who resolves it.
* [Report](report.md) - The single team-owned JSON document every tier writes and the agent, hooks, reviewer, and CI read.
* [Stack profile](stack-profile.md) - The one checked-in file (reliability-profile.yaml) that binds the method to one codebase.
* [Starting condition](starting-condition.md) - Greenfield, maintained, or legacy; declared in the profile, it decides which evidence layer leads.
* [Tier](tier.md) - A time-boxed run of verify; inner loop, pre-PR, or nightly, each with its own budget and layer mix.
