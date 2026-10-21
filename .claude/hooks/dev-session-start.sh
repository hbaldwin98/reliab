#!/bin/sh
# SessionStart hook in reliab's own repo: builds this checkout's reliab into
# .devbin/ (gitignored) and puts .devbin on PATH for the session's shell
# commands through CLAUDE_ENV_FILE, so each worktree runs its own code as
# `reliab` (knowledge/decisions/dev-environment.md). go build is the
# staleness check: on a current binary it writes nothing and takes a
# fraction of a second. It always exits 0; without go it only sets PATH.

cd "${CLAUDE_PROJECT_DIR:-.}" 2>/dev/null || exit 0
[ -f go.mod ] && [ -d cmd/reliab ] || exit 0
root=$(pwd)

if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
  printf 'export PATH="%s/.devbin:$PATH"\n' "$root" >>"$CLAUDE_ENV_FILE" 2>/dev/null
fi

command -v go >/dev/null 2>&1 || exit 0
ext=
[ "${OS:-}" = Windows_NT ] && ext=.exe
if ! out=$(go build -o ".devbin/reliab$ext" \
  -ldflags "-X github.com/hbaldwin98/reliab/internal/version.Version=v0.0.0-dev" ./cmd/reliab 2>&1); then
  # The agent sees this; a running reliab.exe on Windows also blocks the write.
  msg="reliab dev: building .devbin/reliab failed, so it may be stale; run go run ./cmd/reliab instead. go build said: $out"
  msg=$(printf '%s' "$msg" | tr '\n\r\t"\\' '     ')
  printf '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"%s"}}\n' "$msg"
fi
exit 0
