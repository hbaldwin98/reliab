// Command reliab-adapter-golangci-lint converts golangci-lint's JSON output
// into reliab's findings format (docs/adapters.md,
// knowledge/decisions/external-adapters.md).
//
//	golangci-lint run --output.json.path lint.json ./...; reliab-adapter-golangci-lint -exit $? lint.json findings.json
//
// Each issue becomes a finding. Its test is the repo-relative file, the
// linter, and the message without line or column numbers, so it keeps its
// id when code moves and is the same on Windows and Linux; identical issues
// in one file are numbered (#2), (#3) in source order. The location keeps
// file:line:col. golangci-lint's JUnit output names a testcase by
// file:line:col with the OS's separators, which is why this reads JSON.
//
// -exit is golangci-lint's exit code (pkg/exitcodes in golangci-lint
// v2.15.0): 0 ran clean, 1 found issues. Anything else means the run
// failed, even with a JSON file: a compile error exits 7 and writes one
// with no issues, which would otherwise read as a clean run. Then, or with
// the JSON missing or unreadable, this exits 1 and writes nothing, and
// reliab reports the command as failed.
//
// Paths in the JSON may be relative to the working directory or absolute
// (golangci-lint's relative-path-mode); -root, default the working
// directory, is the repository root they are made relative to.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hbaldwin98/reliab/pkg/findings"
)

type output struct {
	Issues *[]issue `json:"Issues"`
}

type issue struct {
	FromLinter  string   `json:"FromLinter"`
	Text        string   `json:"Text"`
	SourceLines []string `json:"SourceLines"`
	Pos         struct {
		Filename string `json:"Filename"`
		Line     int    `json:"Line"`
		Column   int    `json:"Column"`
	} `json:"Pos"`
}

func main() { os.Exit(cli(os.Args[1:], os.Stderr)) }

// cli runs the adapter and returns its exit code: 0 written, 1 failed, 2
// usage.
func cli(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("reliab-adapter-golangci-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	exit := fs.Int("exit", -1, "golangci-lint's exit code")
	root := fs.String("root", ".", "the repository root that paths are made relative to")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: reliab-adapter-golangci-lint -exit <code> [-root <dir>] <golangci-lint.json> <findings.json>")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 || *exit < 0 {
		fs.Usage()
		return 2
	}
	if err := run(*exit, *root, fs.Arg(0), fs.Arg(1)); err != nil {
		fmt.Fprintln(stderr, "reliab-adapter-golangci-lint:", err)
		return 1
	}
	return 0
}

func run(exit int, root, in, out string) error {
	if exit != 0 && exit != 1 {
		return fmt.Errorf("golangci-lint exited %d: the run failed (a config, build, or type-check error), whatever its JSON says", exit)
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	fs, err := convert(raw, absRoot(root))
	if err != nil {
		return fmt.Errorf("%s: %w", in, err)
	}
	if exit == 1 && len(fs) == 0 {
		return fmt.Errorf("golangci-lint exited 1 but %s has no issues", in)
	}
	return findings.Write(out, fs)
}

// convert turns golangci-lint's JSON into findings, ordered by file and
// position, with identical issues in one file numbered.
func convert(raw []byte, root string) ([]findings.Finding, error) {
	var o output
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("not golangci-lint JSON: %w", err)
	}
	if o.Issues == nil {
		return nil, fmt.Errorf("not golangci-lint JSON: no Issues")
	}
	is := *o.Issues
	for i := range is {
		is[i].Pos.Filename = repoPath(root, is[i].Pos.Filename)
	}
	sort.SliceStable(is, func(i, j int) bool {
		a, b := is[i].Pos, is[j].Pos
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	seen := map[string]int{}
	out := make([]findings.Finding, 0, len(is))
	for _, is := range is {
		test := fmt.Sprintf("%s: %s: %s", is.Pos.Filename, is.FromLinter, withoutPositions(is.Text))
		seen[test]++
		if n := seen[test]; n > 1 {
			test += fmt.Sprintf(" (#%d)", n)
		}
		msg := is.FromLinter + ": " + is.Text
		if len(is.SourceLines) > 0 {
			msg += "\n" + strings.Join(is.SourceLines, "\n")
		}
		out = append(out, findings.Finding{
			Test:     test,
			Location: fmt.Sprintf("%s:%d:%d", is.Pos.Filename, is.Pos.Line, is.Pos.Column),
			Message:  msg,
		})
	}
	return out, nil
}

var (
	// file.go:12 or file.go:12:3 inside a message.
	filePos = regexp.MustCompile(`(\.[A-Za-z0-9]+):\d+(:\d+)?\b`)
	// "line 12", "column 3", "col 3".
	wordPos = regexp.MustCompile(`(?i)\b(line|column|col)\s+\d+\b`)
)

// withoutPositions drops line and column numbers from a message, so a
// finding keeps its id when code moves.
func withoutPositions(s string) string {
	s = filePos.ReplaceAllString(s, "$1")
	return wordPos.ReplaceAllString(s, "$1")
}

// repoPath makes p, as golangci-lint wrote it, relative to root with
// forward slashes. Separators are normalised by hand, so a Windows path
// converts the same on any OS. A path outside root stays as written.
func repoPath(root, p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if !isAbs(p) {
		return path.Clean(p)
	}
	r := strings.TrimSuffix(strings.ReplaceAll(root, `\`, "/"), "/")
	if len(p) > len(r) && strings.EqualFold(p[:len(r)], r) && p[len(r)] == '/' {
		return path.Clean(p[len(r)+1:])
	}
	return p
}

// isAbs reports whether a slash path is absolute on either OS.
func isAbs(p string) bool {
	return strings.HasPrefix(p, "/") || (len(p) > 2 && p[1] == ':' && p[2] == '/')
}

// absRoot makes root absolute, leaving one already absolute on either OS as
// it is.
func absRoot(root string) string {
	if isAbs(strings.ReplaceAll(root, `\`, "/")) {
		return root
	}
	if a, err := filepath.Abs(root); err == nil {
		return a
	}
	return root
}
