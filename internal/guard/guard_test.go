package guard

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/hookio"
	"github.com/hbaldwin98/reliab/internal/report"
	"github.com/hbaldwin98/reliab/internal/testrepo"
	"github.com/hbaldwin98/reliab/internal/triage"
	"github.com/hbaldwin98/reliab/internal/verify"
)

const passing = "protected_paths: [\"snapshots/**\"]\ncommands:\n  build: \"true\"\n"
const failing = "protected_paths: [\"snapshots/**\"]\ncommands:\n  build: \"true\"\n  analyzers: exit 1\n"

func open(t *testing.T, extra string) *Repo {
	t.Helper()
	root := testrepo.New(t, extra)
	r, ok, err := Open(root)
	if err != nil || !ok {
		t.Fatalf("Open: ok=%v err=%v", ok, err)
	}
	return r
}

func run(t *testing.T, r *Repo, tier string) *report.Report {
	t.Helper()
	rep, _, err := verify.Run(context.Background(), verify.Options{Root: r.Root, Profile: "reliability-profile.yaml", Tier: tier})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestOpenOutsideReliabRepo(t *testing.T) {
	t.Parallel()
	if _, ok, err := Open(t.TempDir()); ok || err != nil {
		t.Fatalf("ok=%v err=%v for a plain directory", ok, err)
	}
}

func TestPreEdit(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	tests := []struct {
		path string
		deny bool
	}{
		{"snapshots/s.txt", true},
		{filepath.Join(r.Root, "snapshots", "new.txt"), true},
		{".reliab/reports/inner-latest.json", true},
		{"src/a/a.txt", false},
		{filepath.Join(t.TempDir(), "elsewhere.txt"), false},
	}
	for _, tt := range tests {
		d := r.PreEdit(hookio.Input{Cwd: r.Root, ToolInput: hookio.ToolInput{FilePath: tt.path}})
		if (d.Deny != "") != tt.deny {
			t.Errorf("%s: deny=%q, want deny=%v", tt.path, d.Deny, tt.deny)
		}
	}
}

func TestPreBashProtectedAndAck(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	tests := []struct {
		cmd  string
		deny bool
	}{
		{"sed -i s/s/x/ snapshots/s.txt", true},
		{"echo x > snapshots/s.txt", true},
		{"echo x >> ./snapshots/s.txt", true},
		{"rm -rf snapshots", true},
		{"cp /tmp/x snapshots/s.txt", true},
		{`Set-Content snapshots\s.txt x`, true},
		{"git checkout -- snapshots/s.txt", true},
		{"echo hi > .reliab/reports/prepr-latest.json", true},
		{"reliab findings ack F-x --by human:me", true},
		// The baseline is a protected oracle a person writes (R2,
		// knowledge/decisions/mutation-threshold.md); proposing is the agent's.
		{"reliab baseline write --scope 'src/**'", true},
		{"reliab baseline propose --scope 'src/**'", false},
		{`& "C:\tools\reliab.exe" findings ack F-x --by human:me`, true},
		// knowledge/decisions/protected-path-approval.md: approving a
		// protected change is a person's decision, denied to every agent.
		{"reliab approve snapshots/s.txt --by human:me", true},
		{`& "C:\tools\reliab.exe" approve snapshots/s.txt --by human:me`, true},
		{"cd x && RELIAB=1 reliab.cmd  APPROVE tests/seed --by human:me", true},
		{"reliab gate protected --base main", false},
		{"cat snapshots/s.txt", false},
		{"grep -r foo snapshots", false},
		{"go test ./... > /dev/null", false},
		{"echo x > src/a/a.txt", false},
		{"reliab findings classify F-x --kind harness-defect --reason r", false},
		// knowledge/decisions/hook-decisions-v0.md: a path is a write target
		// only in its own simple command, as a redirection target or after a
		// write verb. Each read below was denied in an onboarding session
		// because a write verb appeared elsewhere in the command line.
		{`python -c "import json; json.load(open('.reliab/reports/prepr-latest.json'))"`, false},
		{`python -c "import json; json.load(open('.reliab/reports/prepr-latest.json'))" && rm -f /tmp/x`, false},
		{"git diff -- '*.verified.*'", false},
		{"git diff -- '*.verified.*' | tee /tmp/d.txt", false},
		{"git checkout -b next && git diff main -- '*.verified.*'", false},
		{"ls .reliab/out/x", false},
		{"ls .reliab/out/x; rm -rf /tmp/out", false},
		{"sed -i s/a/b/ notes.txt && cat snapshots/s.txt", false},
		{"cat snapshots/s.txt >> notes.txt", false},
		// ...while every write in a compound command is still seen.
		{"ls src && rm snapshots/s.txt", true},
		{"cat x | tee snapshots/s.txt", true},
		{"git status; echo x > .reliab/approvals.json", true},
		{"git status\nsed -i s/s/x/ snapshots/s.txt", true},
		{"go build ./... || cp /tmp/x 'snapshots/new.txt'", true},
	}
	for _, tt := range tests {
		d := r.PreBash(hookio.Input{Cwd: r.Root, ToolInput: hookio.ToolInput{Command: tt.cmd}})
		if (d.Deny != "") != tt.deny {
			t.Errorf("%q: deny=%q, want deny=%v", tt.cmd, d.Deny, tt.deny)
		}
	}
}

func push(r *Repo, cmd string) hookio.Decision {
	return r.PreBash(hookio.Input{Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}})
}

func TestPrePush(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	if d := push(r, "git push origin main"); !strings.Contains(d.Deny, "no pre-PR report") {
		t.Fatalf("without a report: %+v", d)
	}
	run(t, r, "prepr")
	for _, cmd := range []string{"git push origin main", "git status && git push", "gh pr create --fill"} {
		if d := push(r, cmd); d.Deny != "" {
			t.Errorf("%q after a clean prepr run: %s", cmd, d.Deny)
		}
	}
	testrepo.Write(t, r.Root, "src/a/a.txt", "edited")
	for _, cmd := range []string{"git push", `bash -c "git push origin main"`} {
		if d := push(r, cmd); !strings.Contains(d.Deny, "stale") {
			t.Errorf("%q after an edit: %+v", cmd, d)
		}
	}
}

func TestPrePushOpenFindingsAndVerdict(t *testing.T) {
	t.Parallel()
	r := open(t, failing)
	run(t, r, "prepr")
	if d := r.PrePush(); !strings.Contains(d.Deny, "open finding") {
		t.Fatalf("with an open finding: %+v", d)
	}

	r = open(t, passing+"require_reviewer_verdict: true\n")
	run(t, r, "prepr")
	if d := r.PrePush(); !strings.Contains(d.Deny, "verdict") {
		t.Fatalf("without a required verdict: %+v", d)
	}
}

func TestStop(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	if d := r.Stop(hookio.Input{}); d.Block != "" {
		t.Fatalf("clean repo, no report: %s", d.Block)
	}
	testrepo.Write(t, r.Root, "test/a_test.txt", "t")
	testrepo.Write(t, r.Root, "docs/notes.md", "n")
	if d := r.Stop(hookio.Input{}); d.Block != "" {
		t.Fatalf("only tests and docs changed: %s", d.Block)
	}
	testrepo.Write(t, r.Root, "src/a/a.txt", "edited")
	if d := r.Stop(hookio.Input{}); !strings.Contains(d.Block, "src/a/a.txt") {
		t.Fatalf("product change without a report: %+v", d)
	}
	if d := r.Stop(hookio.Input{StopHookActive: true}); d.Block != "" {
		t.Fatal("blocked although stop_hook_active is set")
	}
	run(t, r, "inner")
	if d := r.Stop(hookio.Input{}); d.Block != "" {
		t.Fatalf("after a clean run: %s", d.Block)
	}
	testrepo.Write(t, r.Root, "src/b/b.txt", "edited")
	if d := r.Stop(hookio.Input{}); !strings.Contains(d.Block, "src/b/b.txt") {
		t.Fatalf("product change after the report: %+v", d)
	}
}

// An agent that commits its change before stopping leaves a clean working
// tree; with no report, only the session's starting tree shows the change.
func TestStopSeesChangesCommittedThisSession(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	in := hookio.Input{SessionID: "11111111-2222-3333-4444-555555555555"}
	r.SessionStart(in)
	testrepo.Write(t, r.Root, "src/a/a.txt", "edited")
	testrepo.Git(t, r.Root, "add", "-A")
	testrepo.Git(t, r.Root, "commit", "-qm", "edit")
	if d := r.Stop(in); !strings.Contains(d.Block, "src/a/a.txt") {
		t.Fatalf("committed product change without a report: %+v", d)
	}
	if d := r.Stop(hookio.Input{SessionID: "another-session"}); d.Block != "" {
		t.Fatalf("a session that started after the commit was blocked: %s", d.Block)
	}
	r.SessionStart(hookio.Input{SessionID: in.SessionID, Source: "compact"})
	if d := r.Stop(in); !strings.Contains(d.Block, "src/a/a.txt") {
		t.Fatalf("a compacted session lost its starting tree: %+v", d)
	}
	run(t, r, "inner")
	if d := r.Stop(in); d.Block != "" {
		t.Fatalf("after a clean run: %s", d.Block)
	}
	for _, bad := range []string{"../escape", ".", ".."} {
		if p := r.sessionAnchor(bad); p != "" {
			t.Errorf("session id %q anchored at %s", bad, p)
		}
	}
}

func TestStopOpenFindings(t *testing.T) {
	t.Parallel()
	r := open(t, failing)
	rep := run(t, r, "inner")
	if d := r.Stop(hookio.Input{}); !strings.Contains(d.Block, "F-analyzers") {
		t.Fatalf("unclassified finding: %+v", d)
	}
	if _, err := triage.Classify(r.Root, rep.Findings[0].ID, report.KindLegacyDefectOrQuirk, "pre-existing lint debt", time.Now()); err != nil {
		t.Fatal(err)
	}
	if d := r.Stop(hookio.Input{}); d.Block != "" {
		t.Fatalf("a finding that needs a human blocked the stop: %s", d.Block)
	}
}

func TestSessionStart(t *testing.T) {
	r := open(t, failing)
	if c := r.SessionStart(hookio.Input{}).Context; !strings.Contains(c, "profile test") || !strings.Contains(c, "No report yet") {
		t.Fatalf("context = %q", c)
	}
	run(t, r, "inner")
	if c := r.SessionStart(hookio.Input{}).Context; !strings.Contains(c, "open F-analyzers") {
		t.Fatalf("context = %q", c)
	}
	testrepo.Write(t, r.Root, ".reliab-version", "v0.1.0\n")
	t.Setenv("RELIAB_PLUGIN_VERSION", "v0.2.0")
	if c := r.SessionStart(hookio.Input{}).Context; !strings.Contains(c, "pins v0.1.0") {
		t.Fatalf("no version warning: %q", c)
	}
}

// A snapshot tool that approves by command names no protected path, so the
// profile lists such commands and the hook denies them.
func TestPreBashProtectedCommands(t *testing.T) {
	t.Parallel()
	r := open(t, passing+`protected_commands: ['\bcargo\s+insta\s+(accept|review)\b', '\bINSTA_UPDATE=(always|auto|new|unseen)\b']`+"\n")
	tests := []struct {
		cmd  string
		deny bool
	}{
		{"cargo insta accept", true},
		{"cargo insta review --workspace", true},
		{"INSTA_UPDATE=always cargo test -p service", true},
		{"$env:INSTA_UPDATE='always'; cargo test", false}, // PowerShell spelling: the profile must list it to deny it
		{"INSTA_UPDATE=no cargo test -p service", false},
		{"cargo insta test", false},
		{"cargo test", false},
	}
	for _, tt := range tests {
		d := r.PreBash(hookio.Input{Cwd: r.Root, ToolInput: hookio.ToolInput{Command: tt.cmd}})
		if (d.Deny != "") != tt.deny {
			t.Errorf("%q: deny=%q, want deny=%v", tt.cmd, d.Deny, tt.deny)
		}
		if tt.deny && !strings.Contains(d.Deny, "characterization-diff") {
			t.Errorf("%q: reason does not say what to do instead: %q", tt.cmd, d.Deny)
		}
	}
	if ctx := r.SessionStart(hookio.Input{}); !strings.Contains(ctx.Context, "never run commands matching") {
		t.Errorf("session start does not list protected commands: %q", ctx.Context)
	}
}

// knowledge/decisions/reviewer-verdict.md: only the reviewer subagent records
// a verdict; the author session and any other agent are denied, as with
// findings ack.
func TestPreBashVerdictOnlyForReviewer(t *testing.T) {
	t.Parallel()
	r := open(t, passing)
	cmds := []string{
		"reliab report verdict --outcome pass --by me --notes ok",
		`& "C:\tools\reliab.exe" report verdict --outcome pass --by me --notes ok`,
		"cd /tmp && RELIAB_BIN=x reliab.cmd   report   verdict --outcome fail --by me --notes no",
	}
	for _, cmd := range cmds {
		for _, agent := range []string{"", "reviewer", "other:reviewer", "general-purpose"} {
			d := r.PreBash(hookio.Input{Cwd: r.Root, AgentType: agent, ToolInput: hookio.ToolInput{Command: cmd}})
			if !strings.Contains(d.Deny, ReviewerAgent) {
				t.Errorf("%q as agent %q: deny=%q, want a denial naming the reviewer", cmd, agent, d.Deny)
			}
		}
		if d := r.PreBash(hookio.Input{Cwd: r.Root, AgentType: ReviewerAgent, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny != "" {
			t.Errorf("%q as the reviewer: %s", cmd, d.Deny)
		}
	}
	for _, cmd := range []string{"reliab report summary", "reliab report route"} {
		if d := r.PreBash(hookio.Input{Cwd: r.Root, ToolInput: hookio.ToolInput{Command: cmd}}); d.Deny != "" {
			t.Errorf("%q: %s", cmd, d.Deny)
		}
	}
}

// The push gate accepts only a passing verdict for the report's own tree; an
// edit after the review makes the report stale.
func TestPrePushVerdictBoundToTree(t *testing.T) {
	t.Parallel()
	r := open(t, passing+"require_reviewer_verdict: true\n")
	rep := run(t, r, "prepr")
	setVerdict := func(outcome, tree string) {
		t.Helper()
		rep.Verdict = &report.Verdict{Outcome: outcome, By: ReviewerAgent, At: report.Now(time.Now()), Notes: "checked", TreeHash: tree}
		if err := report.Rewrite(r.Root, rep); err != nil {
			t.Fatal(err)
		}
	}
	setVerdict(report.VerdictFail, rep.Run.TreeHash)
	if d := r.PrePush(); !strings.Contains(d.Deny, "verdict is fail") {
		t.Errorf("failing verdict: %+v", d)
	}
	setVerdict(report.VerdictPass, strings.Repeat("0", 40))
	if d := r.PrePush(); !strings.Contains(d.Deny, "another tree") {
		t.Errorf("verdict for another tree: %+v", d)
	}
	setVerdict(report.VerdictPass, "")
	if d := r.PrePush(); !strings.Contains(d.Deny, "another tree") {
		t.Errorf("verdict bound to no tree: %+v", d)
	}
	setVerdict(report.VerdictPass, rep.Run.TreeHash)
	if d := r.PrePush(); d.Deny != "" {
		t.Errorf("passing verdict for this tree: %s", d.Deny)
	}
	testrepo.Write(t, r.Root, "src/a/a.txt", "edited after review")
	if d := r.PrePush(); !strings.Contains(d.Deny, "stale") {
		t.Errorf("edit after review: %+v", d)
	}
}
