package runner

import (
	"strings"
	"testing"
	"time"
)

// Source for every test in this file: knowledge/decisions/profile-v2.md,
// "The file" (keys and their meaning) and "Targets" (the three placeholders).

const valid = `version: 1
base: origin/main
commands:
  build:
    about: Compile.
    change: go build ./...
    pr: go build ./...
  unit:
    about: Tests.
    change: go test {projects}
    audit: go test ./...
    targeted: go test {projects}
    output: { file: unit.xml, parse: junit }
    after: [build]
    exclusive: true
    timeout: 90s
`

func TestParseValid(t *testing.T) {
	cfg, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Base != "origin/main" || len(cfg.Commands) != 2 {
		t.Fatalf("got %+v", cfg)
	}
	u := cfg.Commands["unit"]
	if u.Name != "unit" || u.Timeout != 90*time.Second || !u.Exclusive || u.Output.Parse != "junit" {
		t.Fatalf("unit = %+v", u)
	}
	if u.Line("change") != "go test {projects}" || u.Line("pr") != "" || u.Line("audit") != "go test ./..." {
		t.Fatalf("lines: %q %q %q", u.Line("change"), u.Line("pr"), u.Line("audit"))
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"version":        {"version: 2\ncommands: { a: { about: x, pr: y } }", "version must be 1"},
		"no commands":    {"version: 1\ncommands: {}", "at least one command"},
		"no about":       {"version: 1\ncommands: { a: { pr: y } }", "about is required"},
		"no line":        {"version: 1\ncommands: { a: { about: x } }", "needs a line"},
		"unknown key":    {"version: 1\ncommands: { a: { about: x, pr: y, run: z } }", "field run not found"},
		"top-level key":  {"version: 1\nroles: {}\ncommands: { a: { about: x, pr: y } }", "field roles not found"},
		"placeholder":    {"version: 1\ncommands: { a: { about: x, pr: 'go test {scope}' } }", "unknown placeholder {scope}"},
		"targeted ph":    {"version: 1\ncommands: { a: { about: x, targeted: 'go test {base}' } }", "commands.a.targeted: unknown placeholder {base}"},
		"after missing":  {"version: 1\ncommands: { a: { about: x, pr: y, after: [b] } }", `no command "b"`},
		"after self":     {"version: 1\ncommands: { a: { about: x, pr: y, after: [a] } }", "names itself"},
		"cycle":          {"version: 1\ncommands: { a: { about: x, pr: y, after: [b] }, b: { about: x, pr: y, after: [a] } }", "cycle a -> b -> a"},
		"timeout":        {"version: 1\ncommands: { a: { about: x, pr: y, timeout: soon } }", "not a duration"},
		"timeout zero":   {"version: 1\ncommands: { a: { about: x, pr: y, timeout: 0s } }", "not a duration"},
		"output partial": {"version: 1\ncommands: { a: { about: x, pr: y, output: { file: f } } }", "needs file and parse"},
		"parser":         {"version: 1\ncommands: { a: { about: x, pr: y, output: { file: f, parse: stryker } } }", `unknown parser "stryker"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}
