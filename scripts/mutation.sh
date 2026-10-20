#!/bin/sh
# reliab's own mutation commands: gremlins on Go packages, then the adapter
# converts its reports into findings.
#   scripts/mutation.sh changed [BASE]   packages changed since BASE; mutants on changed lines
#   scripts/mutation.sh full      every package; all mutants
# RELIAB_SCOPE (set by reliab verify --scope) limits either mode to packages
# with a Go file under its paths; a file in scope mutates its whole package.
# gremlins runs per package because from the module root on Windows it sees
# no coverage (go-gremlins/gremlins#222; see cmd/adapters/gremlins).
set -u
mode=${1:-changed}
# BASE is the run's resolved base (the profile passes {base}), else
# RELIAB_BASE, else origin/main or main.
base=${2:-${RELIAB_BASE:-$(git rev-parse -q --verify origin/main || echo main)}}
case $mode in
# Untracked files count as changed, as reliab changed-lines has them: a new
# package is mutated before it is committed.
changed) files=$(git diff --name-only "$base" -- '*.go' ':!*_test.go'; git ls-files --others --exclude-standard -- '*.go' ':!*_test.go'); filter="-base $base" ;;
full) files=$(git ls-files --cached --others --exclude-standard -- '*.go' ':!*_test.go'); filter= ;;
*) echo "usage: $0 changed|full" >&2; exit 2 ;;
esac

in_scope() {
  [ -z "${RELIAB_SCOPE:-}" ] && return 0
  for s in $RELIAB_SCOPE; do
    case $1 in "$s" | "$s"/*) return 0 ;; esac
    [ "$s" = . ] && return 0
  done
  return 1
}

out=.reliab/out/gremlins
rm -rf "$out" && mkdir -p "$out" || exit 1
top=$(git rev-parse --show-toplevel) || exit 1
set --
for dir in $(for f in $files; do in_scope "$f" && dirname "$f"; done | sort -u); do
  [ -d "$dir" ] || continue
  report="$out/$(printf '%s' "$dir" | tr / _).json"
  # In changed mode gremlins mutates only lines changed since BASE (--diff).
  # It matches git diff's paths, relative to the repository, against paths
  # relative to the package, so it runs from the package directory with no
  # path argument, and git prints paths relative to there (diff.relative);
  # gremlins takes each hunk's added lines as contiguous, so hunks carry no
  # context (diff.context=0). git diff omits untracked files, so a package
  # holding one is mutated whole; the adapter filters either way.
  diff=
  if [ "$mode" = changed ] && [ -z "$(git ls-files --others --exclude-standard -- "$dir/*.go")" ]; then
    diff="-D $base"
  fi
  # Two workers and a 10x timeout: with gremlins' default (one worker per
  # CPU) tests time out under the load and a mutant reads as killed
  # (knowledge/decisions/external-adapters.md). Never --test-cpu: with it
  # every mutant reads as killed.
  # shellcheck disable=SC2086 # diff is empty or two words
  (cd "$dir" && GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=diff.relative GIT_CONFIG_VALUE_0=true     GIT_CONFIG_KEY_1=diff.context GIT_CONFIG_VALUE_1=0     gremlins unleash $diff --workers 2 --timeout-coefficient 10 -o "$top/$report") || exit 1
  # A package with no mutants (only an embed, say) writes no report.
  [ -f "$report" ] || continue
  set -- "$@" "$dir=$report"
done
# shellcheck disable=SC2086 # filter is empty or two words
exec go run ./cmd/adapters/gremlins $filter .reliab/out/mutation.json "$@"
