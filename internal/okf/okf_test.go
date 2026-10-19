package okf

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// TestRepoBundleConforms keeps knowledge/ conformant on every test run.
func TestRepoBundleConforms(t *testing.T) {
	errs, warns := Check(os.DirFS("../../knowledge"))
	for _, w := range warns {
		t.Log("warning:", w)
	}
	for _, e := range errs {
		t.Error(e)
	}
}

func TestCheck(t *testing.T) {
	const fm = "---\ntype: Decision\n---\n\n# X\n"
	tests := []struct {
		name     string
		files    fstest.MapFS
		wantErr  string // substring of the single expected error; "" for none
		wantWarn string
	}{
		{name: "minimal concept", files: fstest.MapFS{"a.md": file(fm)}},
		{name: "crlf concept", files: fstest.MapFS{"a.md": file(strings.ReplaceAll(fm, "\n", "\r\n"))}},
		{name: "no frontmatter", files: fstest.MapFS{"a.md": file("# X\n")}, wantErr: "missing YAML frontmatter"},
		{name: "empty type", files: fstest.MapFS{"a.md": file("---\ntype: \"\"\n---\n")}, wantErr: "no non-empty type"},
		{name: "bad yaml", files: fstest.MapFS{"a.md": file("---\ntype: [\n---\n")}, wantErr: "does not parse"},
		{name: "root index version", files: fstest.MapFS{
			"index.md": file("---\nokf_version: \"0.2\"\n---\n\n# S\n\n* [A](a.md) - a\n"),
			"a.md":     file(fm),
		}},
		{name: "nested index frontmatter", files: fstest.MapFS{
			"d/index.md": file("---\nokf_version: \"0.2\"\n---\n# S\n"),
		}, wantErr: "not allowed"},
		{name: "index prose", files: fstest.MapFS{"index.md": file("# S\n\nsome prose\n")}, wantErr: "not a heading or link entry"},
		{name: "log newest first", files: fstest.MapFS{
			"log.md": file("# Log\n\n## 2026-10-17\n* **Update**: b\n\n## 2026-10-16\n* a\n"),
		}},
		{name: "log out of order", files: fstest.MapFS{
			"log.md": file("## 2026-10-16\n* a\n\n## 2026-10-17\n* b\n"),
		}, wantErr: "newest first"},
		{name: "log bad date", files: fstest.MapFS{"log.md": file("## Oct 17\n* a\n")}, wantErr: "YYYY-MM-DD"},
		{name: "bundle-relative link", files: fstest.MapFS{
			"d/a.md": file(fm + "[b](/b.md)\n"),
			"b.md":   file(fm),
		}},
		{name: "broken link warns", files: fstest.MapFS{"a.md": file(fm + "[x](/missing.md)\n")}, wantWarn: "broken link /missing.md"},
		{name: "external link ignored", files: fstest.MapFS{"a.md": file(fm + "[x](https://example.com/y)\n")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, warns := Check(tt.files)
			expectOne(t, "error", errs, tt.wantErr)
			expectOne(t, "warning", warns, tt.wantWarn)
		})
	}
}

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func expectOne(t *testing.T, what string, got []Problem, want string) {
	t.Helper()
	if want == "" {
		if len(got) != 0 {
			t.Errorf("unexpected %ss: %v", what, got)
		}
		return
	}
	if len(got) != 1 || !strings.Contains(got[0].Msg, want) {
		t.Errorf("%ss = %v, want one containing %q", what, got, want)
	}
}
