#!/bin/sh
# reliab's own release gate: the release workflow runs this before it builds
# anything, so a tag publishes only code the ci workflow passed on (README
# "Release"). Not a template for users (reliab is CI-agnostic).
#   scripts/release-gate.sh [SHA]   default: HEAD
# Passes when a ci run on SHA succeeded, or when SHA is the release's version
# bump (it changes plugin/.claude-plugin/plugin.json and at most
# knowledge/log.md, the release's log line) on a commit a ci run succeeded on:
# the release order tags the bump before main is pushed, so ci has not run on
# the tag's own commit yet.
# Needs gh with a token (GH_TOKEN) that can read Actions, and the parent
# commit in the clone; GITHUB_REPOSITORY names the repository (default: gh's
# current repository).
set -u
cd "$(dirname "$0")/.."
sha=$(git rev-parse --verify "${1:-HEAD}^{commit}") || exit 2
repo=${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)} || exit 2

green() {
  n=$(gh api "repos/$repo/actions/workflows/ci.yml/runs?head_sha=$1&status=success&per_page=1" --jq .total_count) || exit 2
  [ "${n:-0}" -gt 0 ]
}

if green "$sha"; then
  echo "release-gate: ci passed on $sha" >&2
  exit 0
fi
if parent=$(git rev-parse -q --verify "$sha^"); then
  changed=$(git diff --name-only "$parent" "$sha") || exit 2
  other=$(printf '%s\n' "$changed" | grep -v -x -e plugin/.claude-plugin/plugin.json -e knowledge/log.md)
  if [ -z "$other" ] && printf '%s\n' "$changed" | grep -q -x plugin/.claude-plugin/plugin.json; then
    if green "$parent"; then
      echo "release-gate: $sha bumps the version on $parent, which ci passed on" >&2
      exit 0
    fi
    echo "release-gate: $sha bumps the version, but no successful ci run on its parent $parent either." >&2
  elif [ -n "$other" ]; then
    echo "release-gate: $sha changes more than plugin.json and knowledge/log.md, so it is not a version bump ci can skip:" >&2
    printf '%s\n' "$other" | sed 's/^/  /' >&2
  fi
fi
echo "release-gate: no successful ci run on $sha." >&2
echo "release-gate: push the commit to main, wait for ci to pass on ubuntu, macOS, and Windows, then tag it." >&2
exit 1
