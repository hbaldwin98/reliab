package dap

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// docs/windows.md: a temporary directory can come back as an 8.3 short name
// (C:\Users\RUNNER~1\... on GitHub's runners); an adapter's path spelled that
// way must still be repo-relative in the transcript
// (knowledge/decisions/debug-adapter-seam.md).
func TestPathMapperMapsAShortNamePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a long directory name")
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	long, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := syscall.UTF16PtrFromString(long)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 1024)
	n, err := syscall.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil {
		t.Fatal(err)
	}
	short := syscall.UTF16ToString(buf[:n])
	if short == long {
		t.Skip("this volume makes no 8.3 short names")
	}
	if got := NewPathMapper(long, "").Rel(filepath.Join(short, "src", "a.go")); got != "src/a.go" {
		t.Errorf("Rel(%q) = %q, want src/a.go", filepath.Join(short, "src", "a.go"), got)
	}
}
