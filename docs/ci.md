# reliab in CI

reliab writes no CI configuration. Call it from whatever CI the repository
uses; its exit code is the gate.

## Install

Install the version pinned in `.reliab-version`, so CI runs what everyone
else runs:

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

The CI machine also needs every tool the commands in `reliab.yaml` run.
Run `reliab doctor` first: it lists what is missing.

## Commands

| When | Command |
|---|---|
| Every push or PR | `reliab run pr` |
| On a schedule | `reliab run audit` |

`pr` looks at the files changed since the base (`base` in `reliab.yaml`,
default `origin/main`; override with `--base`). A shallow clone must fetch
the base ref first, or nothing can be compared with it.

`--parallel n` caps how many commands run at once; it defaults to the
number of CPUs. Keep `.reliab/run/` as a build artifact: it holds each
command's log.

## Exit codes

`0` everything passed, `1` something failed (a failing command, a finding,
a timeout, or a command blocked by one that failed), `2` a config or usage
error.
