package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hbaldwin98/reliab/internal/adapters"
	"github.com/hbaldwin98/reliab/pkg/findings"
)

// Trimmed from a real cargo-mutants 27.1.0 run.
const sample = `{
  "outcomes": [
    {"scenario": "Baseline", "summary": "Success", "log_path": "log/baseline.log", "diff_path": null},
    {"scenario": {"Mutant": {"name": "src/store/mod.rs:240:31: replace < with <= in Store::migrate", "package": "d", "file": "src/store/mod.rs",
      "function": {"function_name": "Store::migrate", "return_type": "-> Result<()>"},
      "span": {"start": {"line": 240, "column": 31}, "end": {"line": 240, "column": 32}}, "replacement": "<=", "genre": "BinaryOperator"}},
     "summary": "MissedMutant", "log_path": "log/a.log", "diff_path": "diff/a.diff"},
    {"scenario": {"Mutant": {"name": "src/store/mod.rs:227:20: replace < with <= in Store::migrate", "package": "d", "file": "src/store/mod.rs",
      "function": {"function_name": "Store::migrate", "return_type": "-> Result<()>"},
      "span": {"start": {"line": 227, "column": 20}, "end": {"line": 227, "column": 21}}, "replacement": "<=", "genre": "BinaryOperator"}},
     "summary": "MissedMutant", "log_path": "log/b.log", "diff_path": "diff/missing.diff"},
    {"scenario": {"Mutant": {"name": "src/store/mod.rs:191:9: replace Store::migrate -> Result<()> with Ok(())", "package": "d", "file": "src/store/mod.rs",
      "span": {"start": {"line": 191, "column": 9}, "end": {"line": 293, "column": 15}}, "replacement": "Ok(())", "genre": "FnValue"}},
     "summary": "Timeout", "log_path": "log/c.log", "diff_path": "diff/c.diff"},
    {"scenario": {"Mutant": {"name": "src/lib.rs:3:5: replace f -> i32 with 0", "package": "d", "file": "src/lib.rs",
      "span": {"start": {"line": 3, "column": 5}, "end": {"line": 3, "column": 6}}, "replacement": "0", "genre": "FnValue"}},
     "summary": "CaughtMutant", "log_path": "log/d.log", "diff_path": "diff/d.diff"}
  ],
  "total_mutants": 4, "missed": 2, "caught": 1, "timeout": 1, "unviable": 0, "success": 0,
  "cargo_mutants_version": "27.1.0"
}`

// mutantsOut writes a mutants.out directory holding body as outcomes.json.
func mutantsOut(t *testing.T, body string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "mutants.out")
	if err := os.MkdirAll(filepath.Join(dir, "diff"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "diff", "a.diff"), []byte("-    if v < 3 {\n+    if v <= 3 {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outcomes.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConvert(t *testing.T) {
	dir := mutantsOut(t, sample)
	raw, _ := os.ReadFile(filepath.Join(dir, "outcomes.json"))
	got, counts, err := convert(raw, dir)
	if err != nil {
		t.Fatal(err)
	}
	// knowledge/decisions/pr-evidence-summary.md: the findings file counts the
	// mutants tested so the PR summary can say tested, killed, and survived.
	// One caught and one timeout are killed; the baseline is not a mutant.
	if counts != (findings.MutantCounts{Killed: 2, Survived: 2}) {
		t.Errorf("counts = %+v", counts)
	}
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want the 2 missed mutants", got)
	}
	if got[0].Test != "src/store/mod.rs: replace < with <= in Store::migrate" || got[0].Location != "src/store/mod.rs:227:20" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Test != "src/store/mod.rs: replace < with <= in Store::migrate (#2)" || got[1].Location != "src/store/mod.rs:240:31" {
		t.Errorf("second = %+v", got[1])
	}
	if !strings.Contains(got[1].Message, "if v <= 3") {
		t.Errorf("diff not inlined: %q", got[1].Message)
	}
}

func TestConvertBaselineFailure(t *testing.T) {
	got, _, err := convert([]byte(`{"outcomes":[{"scenario":"Baseline","summary":"Failure","log_path":"log/baseline.log","diff_path":null}],"cargo_mutants_version":"27.1.0"}`), "mutants.out")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Test != "baseline" || !strings.Contains(got[0].Message, "mutants.out/log/baseline.log") {
		t.Fatalf("got %+v", got)
	}
}

func TestRun(t *testing.T) {
	survivors := mutantsOut(t, sample)
	empty := filepath.Join(t.TempDir(), "mutants.out")
	tests := []struct {
		name    string
		exit    int
		dir     string
		want    int
		wantErr bool
	}{
		// cargo-mutants exits 2 when it misses mutants.
		{"survivors", 2, survivors, 2, false},
		{"nothing to mutate", 0, empty, 0, false},
		{"failed without outcomes", 1, empty, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "findings.json")
			err := run(tt.exit, tt.dir, out)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("output written on error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile(out)
			got, err := adapters.ParseFindings(raw)
			if err != nil {
				t.Fatalf("reliab cannot read %s: %v", raw, err)
			}
			if len(got) != tt.want {
				t.Fatalf("findings = %+v, want %d", got, tt.want)
			}
		})
	}
}
