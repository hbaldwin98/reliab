//go:build windows

package verify

import (
	"os"
	"path/filepath"
)

// shellCandidates are tried in order: sh on PATH (Git Bash), then Git for
// Windows' default install location.
func shellCandidates() []string {
	out := []string{"sh"}
	for _, env := range []string{"ProgramFiles", "ProgramW6432"} {
		if dir := os.Getenv(env); dir != "" {
			out = append(out, filepath.Join(dir, "Git", "bin", "sh.exe"))
		}
	}
	return out
}
