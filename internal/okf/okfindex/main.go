// Command okfindex rewrites each section index.md of an OKF bundle from its
// concepts' frontmatter (okf.Index). Usage: go run ./internal/okf/okfindex
// [bundle], default knowledge; or go generate ./internal/okf.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hbaldwin98/reliab/internal/okf"
)

func main() {
	root := "knowledge"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if err := run(root); err != nil {
		fmt.Fprintln(os.Stderr, "okfindex:", err)
		os.Exit(1)
	}
}

func run(root string) error {
	fsys := os.DirFS(root)
	dirs, headings, err := okf.Sections(fsys)
	if err != nil {
		return err
	}
	for i, dir := range dirs {
		want, err := okf.Index(fsys, dir, headings[i])
		if err != nil {
			return err
		}
		path := filepath.Join(root, dir, "index.md")
		if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, want) {
			continue
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "okfindex: wrote", path)
	}
	return nil
}
