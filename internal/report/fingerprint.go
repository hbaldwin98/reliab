package report

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// Noise that changes between runs of the same failure.
	timestamps = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?|\b\d{2}:\d{2}:\d{2}(\.\d+)?\b`)
	durations  = regexp.MustCompile(`\b(\d+(\.\d+)?(h|ms|µs|us|ns|m|s))+\b`)
	addresses  = regexp.MustCompile(`\b0x[0-9a-fA-F]{6,}\b`)
	drivePaths = regexp.MustCompile(`\b[A-Za-z]:[\\/][^\s"'()<>:]*`)
	unixPaths  = regexp.MustCompile(`(^|[\s"'(=])/([^\s"'()<>:/]+/)+[^\s"'()<>:]*`)
	// A temp path's own name is random: keep only that it was one.
	tempRest = regexp.MustCompile(`<tmp>[^\s"'()<>:]*`)
)

// Fingerprint identifies the failure a command-level finding records, for
// carrying its classification only while the failure is the same. It hashes
// the message and observed output after removing what differs between runs
// of one failure: line endings and trailing spaces, the repository root and
// the temp directory, other absolute paths, timestamps, durations, and
// memory addresses. root is the repository root.
func Fingerprint(f Finding, root string) string {
	sum := sha256.Sum256([]byte(Normalize(f.Message, root) + "\x00" + Normalize(f.Observed, root)))
	return hex.EncodeToString(sum[:8])
}

// Normalize is the text Fingerprint hashes.
func Normalize(s, root string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for _, p := range []struct{ dir, name string }{{root, "<root>"}, {os.TempDir(), "<tmp>"}} {
		if p.dir == "" {
			continue
		}
		for _, form := range []string{p.dir, filepath.ToSlash(p.dir), filepath.FromSlash(p.dir)} {
			s = replaceFold(s, form, p.name)
		}
	}
	s = tempRest.ReplaceAllString(s, "<tmp>")
	s = drivePaths.ReplaceAllString(s, "<path>")
	s = unixPaths.ReplaceAllString(s, "$1<path>")
	s = timestamps.ReplaceAllString(s, "<time>")
	s = durations.ReplaceAllString(s, "<dur>")
	s = addresses.ReplaceAllString(s, "<addr>")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// replaceFold replaces every case-insensitive occurrence of old in s.
func replaceFold(s, old, repl string) string {
	if old == "" {
		return s
	}
	return regexp.MustCompile(`(?i)`+regexp.QuoteMeta(old)).ReplaceAllLiteralString(s, repl)
}
