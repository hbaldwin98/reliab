package runner

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Step is one command a plan runs.
type Step struct {
	Name  string
	About string
	// Line is the shell line, placeholders filled.
	Line string
	// Targeted is true when Line is the command's targeted line.
	Targeted bool
	// After are the steps in this plan that must pass first.
	After     []string
	Exclusive bool
	Timeout   time.Duration
	Threshold float64
	Output    *Output
	// Stage is the step's depth in the after-graph, from 1: what --plan
	// shows as order. Steps in one stage may run together.
	Stage int
}

// Plan is what a run will do.
type Plan struct {
	Context string
	Base    string
	Target  *Target
	// Seed is RELIAB_SEED for the run's commands.
	Seed  uint32
	Steps []*Step
	// NotHere are commands with no line for the context.
	NotHere []string
	// NoTests are targeted commands skipped because the target has no
	// {tests} or {projects} to give them.
	NoTests []string
}

// Build plans a run. A command runs when it has a line for the context;
// with an explicit target and a targeted line, the targeted line runs
// instead. An after on a command that is not in the plan is dropped: it
// cannot fail first if it does not run.
func Build(cfg *Config, context string, t *Target, explicit bool, base string) (*Plan, error) {
	if !contains(Contexts, context) {
		return nil, fmt.Errorf("unknown context %q: use change, pr or audit", context)
	}
	p := &Plan{Context: context, Base: base, Target: t}
	in := map[string]*Step{}
	for _, name := range cfg.Names() {
		c := cfg.Commands[name]
		line := c.Line(context)
		if line == "" {
			p.NotHere = append(p.NotHere, name)
			continue
		}
		s := &Step{Name: name, About: strings.TrimSpace(c.About), Exclusive: c.Exclusive, Timeout: c.Timeout, Threshold: c.Threshold, Output: c.Output}
		if explicit && c.Targeted != "" {
			line, s.Targeted = c.Targeted, true
		}
		if placeholderRe.MatchString(line) {
			if t.All {
				return nil, fmt.Errorf("commands.%s.%s uses a placeholder, but %s targets everything: write the line without one, or pass --target", name, context, context)
			}
			filled, ok := Fill(line, t)
			if !ok {
				p.NoTests = append(p.NoTests, name)
				continue
			}
			line = filled
		}
		s.Line = line
		in[name] = s
		p.Steps = append(p.Steps, s)
	}
	for _, s := range p.Steps {
		for _, a := range cfg.Commands[s.Name].After {
			if in[a] != nil {
				s.After = append(s.After, a)
			}
		}
	}
	var depth func(*Step) int
	depth = func(s *Step) int {
		if s.Stage == 0 {
			s.Stage = 1
			for _, a := range s.After {
				if d := depth(in[a]) + 1; d > s.Stage {
					s.Stage = d
				}
			}
		}
		return s.Stage
	}
	for _, s := range p.Steps {
		depth(s)
	}
	return p, nil
}

// Fill replaces {files}, {projects} and {tests} with the target's lists as
// shell words. It returns false when a placeholder the line uses is empty:
// running the tool with no arguments would run everything, not nothing.
func Fill(line string, t *Target) (string, bool) {
	lists := map[string][]string{"files": t.Files, "projects": t.Projects, "tests": t.Tests}
	ok := true
	out := placeholderRe.ReplaceAllStringFunc(line, func(m string) string {
		items := lists[m[1:len(m)-1]]
		if len(items) == 0 {
			ok = false
		}
		return shellWords(items)
	})
	return out, ok
}

// shellWords quotes each item as one POSIX sh word when it needs quoting.
func shellWords(items []string) string {
	words := make([]string, len(items))
	for i, s := range items {
		if s != "" && strings.IndexFunc(s, unsafeShellRune) < 0 {
			words[i] = s
		} else {
			words[i] = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return strings.Join(words, " ")
}

// unsafeShellRune reports whether r needs quoting in a POSIX sh word.
func unsafeShellRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	}
	return !strings.ContainsRune("/._-#:", r)
}

// Print writes the plan for a person or an agent to read.
func (p *Plan) Print(w io.Writer) {
	fmt.Fprintf(w, "reliab run %s\n", p.Context)
	fmt.Fprintf(w, "target: %s\n", strings.Join(p.Target.Specs, ", "))
	if !p.Target.All {
		fmt.Fprintf(w, "  files: %d   projects: %s   tests: %s\n",
			len(p.Target.Files), listOrNone(p.Target.Projects), listOrNone(p.Target.Tests))
		if len(p.Target.Ranges) <= 5 {
			for _, f := range p.Target.Files {
				if rs, ok := p.Target.Ranges[f]; ok {
					fmt.Fprintf(w, "  %s lines %s\n", f, rangesText(rs))
				}
			}
		}
	}
	if p.Base != "" {
		fmt.Fprintf(w, "base: %s\n", p.Base)
	}
	fmt.Fprintln(w)
	if len(p.Steps) == 0 {
		fmt.Fprintln(w, "nothing to run")
	}
	for stage := 1; ; stage++ {
		var here []*Step
		for _, s := range p.Steps {
			if s.Stage == stage {
				here = append(here, s)
			}
		}
		if len(here) == 0 {
			break
		}
		fmt.Fprintf(w, "%d.\n", stage)
		for _, s := range here {
			var tags []string
			if s.Targeted {
				tags = append(tags, "targeted")
			}
			if s.Exclusive {
				tags = append(tags, "runs alone")
			}
			if len(s.After) > 0 {
				tags = append(tags, "after "+strings.Join(s.After, ", "))
			}
			if s.Timeout > 0 {
				tags = append(tags, "timeout "+s.Timeout.String())
			}
			if s.Threshold > 0 {
				tags = append(tags, fmt.Sprintf("mutation score >= %g%%", s.Threshold))
			}
			head := "  " + s.Name
			if len(tags) > 0 {
				head += "  (" + strings.Join(tags, "; ") + ")"
			}
			fmt.Fprintln(w, head)
			fmt.Fprintf(w, "    $ %s\n", s.Line)
			for _, l := range strings.Split(s.About, "\n") {
				fmt.Fprintf(w, "    %s\n", l)
			}
		}
	}
	if len(p.NoTests) > 0 {
		fmt.Fprintf(w, "\nskipped, target gives them nothing to run on: %s\n", strings.Join(p.NoTests, ", "))
	}
	if len(p.NotHere) > 0 {
		fmt.Fprintf(w, "not in %s: %s\n", p.Context, strings.Join(p.NotHere, ", "))
	}
}

func listOrNone(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	if len(xs) > 4 {
		return strings.Join(xs[:4], " ") + fmt.Sprintf(" (+%d)", len(xs)-4)
	}
	return strings.Join(xs, " ")
}

func rangesText(rs [][2]int) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		if r[0] == r[1] {
			parts[i] = fmt.Sprint(r[0])
		} else {
			parts[i] = fmt.Sprintf("%d-%d", r[0], r[1])
		}
	}
	return strings.Join(parts, ",")
}
