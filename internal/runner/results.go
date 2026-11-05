package runner

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// PrintResults writes one line per step, then each failure's findings, and
// reports whether anything did not pass.
func PrintResults(w io.Writer, results []*StepResult) bool {
	failed := false
	fmt.Fprintln(w)
	for _, r := range results {
		line := fmt.Sprintf("%-8s %-12s", mark(r.Status), r.Step.Name)
		switch r.Status {
		case Blocked:
			line += " " + r.BlockedBy + " did not pass"
		default:
			line += " " + r.Elapsed.Round(100*time.Millisecond).String()
			if r.Outside > 0 {
				line += fmt.Sprintf("  (%d findings outside the target)", r.Outside)
			}
		}
		fmt.Fprintln(w, strings.TrimRight(line, " "))
		if r.Status != Passed {
			failed = true
		}
	}
	for _, r := range results {
		if r.Status == Passed || r.Status == Blocked {
			continue
		}
		fmt.Fprintf(w, "\n%s: %s (log: %s)\n", r.Step.Name, r.Status, r.Log)
		for i, f := range r.Findings {
			if i == 10 {
				fmt.Fprintf(w, "  ... and %d more\n", len(r.Findings)-10)
				break
			}
			head := f.Location
			if f.Test != "" {
				head = strings.TrimSpace(f.Test + " " + head)
			}
			msg := firstLines(f.Message, 6)
			if head != "" && strings.Contains(msg, f.Location) {
				head = f.Test
			}
			if head != "" {
				fmt.Fprintf(w, "  - %s\n", head)
				if msg != "" {
					fmt.Fprintf(w, "    %s\n", strings.ReplaceAll(msg, "\n", "\n    "))
				}
			} else {
				fmt.Fprintf(w, "  - %s\n", strings.ReplaceAll(msg, "\n", "\n    "))
			}
		}
		if about := strings.TrimSpace(r.Step.About); about != "" {
			fmt.Fprintf(w, "  about: %s\n", strings.ReplaceAll(about, "\n", " "))
		}
	}
	return failed
}

func mark(s Status) string {
	switch s {
	case Passed:
		return "pass"
	case Blocked:
		return "blocked"
	case TimedOut:
		return "timeout"
	case Cancelled:
		return "cancel"
	}
	return "FAIL"
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}
