# reliab in CI

reliab writes no CI configuration. Wire these commands into whatever CI the
repository uses; each one's exit code is the gate.

## Install

Install the version pinned in `.reliab-version`, so CI runs what the agents
run:

```sh
curl -fsSL https://raw.githubusercontent.com/hbaldwin98/reliab/main/scripts/install.sh | sh -s -- "$(cat .reliab-version)"
export PATH="$HOME/.reliab/bin:$PATH"
```

On Windows without a POSIX shell, use `scripts/install.ps1` with
`-Version`. Both download from GitHub Releases over HTTPS with no
credentials; an authenticated `gh` is used instead when present. For a
mirror, set `RELIAB_DOWNLOAD_URL` (and `RELIAB_TOKEN` if it needs one); it
may be a local directory or a `file://` URL holding the archive and
`checksums.txt`.

The CI agent also needs every tool the profile's commands run. Run
`reliab doctor` first: it lists what is missing.

## Commands

| When | Command | Fails when |
|---|---|---|
| Every push | `reliab verify --tier prepr` | a finding is open, the config is wrong, or the run is incomplete |
| Every PR | `reliab gate protected --base origin/main` | a protected path changed |
| Every PR, after `verify --tier prepr` | `reliab gate mutation` | a mutation score is below its threshold or baseline, or the report is stale |
| On a schedule | `reliab verify --tier nightly` | as for prepr; no budget |
| After a verify, for the CI's test view | `reliab report export --format junit -o report.xml` (or `--format sarif`) | never; it only writes |

Approvals (`reliab approve`) live in `.reliab/`, which is never committed,
so on CI every change to a protected path fails `gate protected`. Make the
code host require a person's approval of the PR when it fails, from the
approvers the profile's owner named. It needs the base ref fetched: a
shallow clone must fetch `origin/main` first.

## Exit codes

`0` passed and complete, `1` open findings, `2` config error, `3`
incomplete (the budget ran out, or a command needs tests named).
Treat `3` as a failure: an incomplete run is not evidence.

## S0 on a CI agent

Onboarding probes each tool locally. The S0 gate also needs the probes to
pass on a CI agent: run `reliab doctor`, `reliab verify --tier inner`, and
`reliab verify --tier prepr` there once, then set `verified_on` in the
profile.
