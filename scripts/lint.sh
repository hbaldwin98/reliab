#!/bin/sh
# reliab's own lint gate: the CI lint job runs this, and it runs the same in
# Git Bash, Linux, and macOS. Not a template for users (reliab is CI-agnostic).
#   scripts/lint.sh [BASE]
# Every check runs, cheapest first; the script fails if any of them failed.
#   gofmt         gofmt -l lists no file
#   whitespace    git diff --check from BASE's merge base (skipped without BASE)
#   bytes         no tracked text file holds a control byte other than tab and
#                 newline. CR counts: .gitattributes stores every text file
#                 with LF (*.ps1 and *.cmd get CRLF only in the working tree)
#   golangci-lint the version pinned in tools/go.mod, built with this
#                 module's toolchain (profile correction for T14)
set -u
cd "$(dirname "$0")/.."
base=${1:-}
failed=

fail() {
  echo "lint: $1" >&2
  failed="$failed $2"
}

out=$(gofmt -l .)
[ -z "$out" ] || fail "gofmt would change:
$out" gofmt

if [ -z "$base" ]; then
  echo "lint: whitespace skipped (no base)" >&2
elif ! mb=$(git merge-base "$base" HEAD 2>/dev/null); then
  echo "lint: whitespace skipped ($base has no merge base with HEAD)" >&2
elif ! out=$(git diff --check "$mb" 2>&1); then
  fail "whitespace errors since $base:
$out" whitespace
fi

# -I skips binary files (a NUL byte); LC_ALL=C makes the range bytewise.
out=$(LC_ALL=C git grep --cached -I -n -e "$(printf '[\001-\010\013-\037\177]')" | cat -v)
[ -z "$out" ] || fail "control bytes (cat -v; ^M is CR):
$out" bytes

go tool -modfile=tools/go.mod golangci-lint run ./... || fail "golangci-lint failed" golangci-lint

if [ -n "$failed" ]; then
  echo "lint: failed:$failed" >&2
  exit 1
fi
echo "lint: ok" >&2
