#!/bin/sh
# reliab's own CI check that the public repository stays company-agnostic
# (knowledge/decisions/context-agnostic.md, public-repo.md). Not a template
# for users (reliab is CI-agnostic).
#   scripts/check-names.sh [RANGE]
# RANGE is a git revision range such as BASE..HEAD (default: HEAD alone).
#   authors every commit in RANGE, merges aside, has a GitHub noreply author
#           email (ID+login@users.noreply.github.com or login@...)
#   names   no tracked file's path or text, and no commit message in RANGE,
#           holds a name from NAME_DENYLIST (case-insensitive, whole words)
# The names never live in the repository. The owner keeps them in the
# repository secret NAME_DENYLIST (comma- or newline-separated), which the CI
# workflow passes in; with the variable empty the names check is skipped, not
# failed (for example on a pull request from a fork, which gets no secrets).
# A match prints where it is, never the name, since a public repository's CI
# logs are public: a path that holds a name prints as its line number in
# git ls-files.
set -u
cd "$(dirname "$0")/.."
range=${1:-HEAD}
failed=
nl='
'

case $range in
*..*)
  # A push that creates a branch reports an all-zero before; an unknown base
  # leaves only the head to check.
  left=${range%%..*}
  if ! git rev-parse -q --verify "${left:-HEAD}^{commit}" >/dev/null; then
    echo "check-names: $left is not a commit; checking ${range##*.} alone" >&2
    range="${range##*.} -1"
  fi
  ;;
*) range="$range -1" ;;
esac

# shellcheck disable=SC2086 # range holds a revision and maybe -1
emails=$(git log --no-merges --format='%h %ae' $range) || exit 2
bad=$(printf '%s\n' "$emails" | grep -v -E '^[0-9a-f]+ ([0-9]+\+)?[A-Za-z0-9-]+@users\.noreply\.github\.com$' | grep .)
if [ -n "$bad" ]; then
  echo "check-names: author emails that are not GitHub noreply addresses:" >&2
  printf '%s\n' "$bad" | cut -d' ' -f1 >&2
  failed="$failed authors"
fi

# match prints the label of each stdin line holding a name: whole words, any
# case. A line starting with \001 sets the label for the lines after it;
# without one the label is the line number. awk rather than grep, since some
# greps (Git for Windows' 3.0) abort on -i -w -F.
match() {
  awk 'BEGIN { n = split(tolower(ENVIRON["CHECK_NAMES"]), w, "\n") }
    /^\001/ { label = substr($0, 2); next }
    {
      l = tolower($0)
      for (i = 1; i <= n; i++) {
        if (w[i] == "") continue
        for (o = 0; (p = index(substr(l, o + 1), w[i])) > 0; o += p) {
          b = substr(l, o + p - 1, 1); a = substr(l, o + p + length(w[i]), 1)
          if (b !~ /[a-z0-9_]/ && a !~ /[a-z0-9_]/) { print (label != "" ? label : NR); next }
        }
      }
    }' | uniq
}

# One -e per name for git grep, and a newline list for match.
set --
CHECK_NAMES=
oldifs=$IFS
IFS=",$nl"
for n in ${NAME_DENYLIST:-}; do
  n=$(printf '%s' "$n" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
  [ -n "$n" ] && set -- "$@" -e "$n" && CHECK_NAMES="$CHECK_NAMES$n$nl"
done
IFS=$oldifs
export CHECK_NAMES

if [ $# -eq 0 ]; then
  echo "check-names: names skipped (NAME_DENYLIST is empty)" >&2
else
  lines=$(git ls-files | match)
  # The text of a file whose path holds a name is not listed by path.
  # shellcheck disable=SC2046,SC2086
  CHECK_SKIP=$(git ls-files | awk -v ls=" $(echo $lines) " 'index(ls, " " NR " ")')
  export CHECK_SKIP
  hits=$(git grep --cached -I -i -w -F -n "$@" | cut -d: -f1,2 |
    awk 'BEGIN { n = split(ENVIRON["CHECK_SKIP"], s, "\n"); for (i = 1; i <= n; i++) skip[s[i]] = 1 }
      { f = $0; sub(/:[0-9]+$/, "", f); if (!(f in skip)) print }')
  for l in $lines; do hits="$hits${hits:+$nl}path at git ls-files line $l"; done
  # shellcheck disable=SC2086
  for c in $(git log --format='%x01%H%n%B' $range | match); do
    hits="$hits${hits:+$nl}commit $c (message)"
  done
  if [ -n "$hits" ]; then
    echo "check-names: a denylisted name appears in:" >&2
    printf '%s\n' "$hits" >&2
    failed="$failed names"
  fi
fi

if [ -n "$failed" ]; then
  echo "check-names: failed:$failed" >&2
  exit 1
fi
echo "check-names: ok" >&2
