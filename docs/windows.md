# Developing reliab on Windows

Gotchas met while developing reliab itself on Windows, often in a git
worktree under `.claude/worktrees/`. For using reliab on Windows, see the
README.

- **Shells.** Claude Code runs hooks and the Bash tool under Git Bash; the
  project hooks in `.claude/settings.json` need it (`sh`). Without Git Bash a
  hook errors without blocking, and the session goes on.
- **`reliab` on PATH.** The SessionStart hook builds this checkout into
  `.devbin/` and puts it on PATH for the Bash tool. The PowerShell tool may
  not get that PATH; there, run `go run ./cmd/reliab <args>` from the
  checkout's root. If the hook says the build failed, a running
  `reliab.exe` may hold the file: Windows will not replace a running
  executable.
- **Git Bash paths.** Git Bash spells `C:\src` as `/c/src`, and `pwd` prints
  that form. Go and native tools take `C:/src` or `C:\src`. The pre-bash guard
  reads a `/c/...` path as a drive path.
- **Short names.** A temporary directory can come back as an 8.3 short name,
  such as `C:\Users\RUNNER~1\AppData\Local\Temp` on GitHub's runners. Compare
  paths after `filepath.EvalSymlinks`, and never treat a `~` inside a name as
  home (only a leading `~` expands).
- **Symlinked temp directories are macOS, not Windows.** macOS's `/var` is a
  symlink to `/private/var`, so a test that compares a temp path to a
  resolved one fails there and passes on Windows. Resolve both sides.
- **Line ends.** `.gitattributes` stores text with LF (`.ps1` and `.cmd`
  with CRLF). An editor or tool that writes CRLF into a `.go`, `.md`, or
  `.sh` file leaves a diff on every line, and `sh` fails on a script with
  CRLF. Write files with the Edit or Write tool; the project's PreToolUse
  hook warns when a shell command writes a `.go` or `.md` file another way.
- **PowerShell quoting.** Single quotes are literal; double quotes expand `$`
  and the backtick escape. Passing `--` or `@` to a native program needs
  `--%` or quoting. A here-string closes with `'@` at column 0.
- **UTF-8 for Python.** Python on Windows reads and writes files in the
  system code page unless told otherwise, which mangles non-ASCII text. The
  project settings set `PYTHONUTF8=1`; pass `encoding="utf-8"` anyway.
- **gremlins from the module root.** On Windows, gremlins run from the
  module root sees no coverage and reports every mutant not covered
  (go-gremlins/gremlins#222). `scripts/mutation.sh` runs it from each
  package's directory; do the same by hand.
