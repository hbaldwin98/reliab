// Command okfindex rewrites each section index.md of an OKF bundle from its
// concepts' frontmatter (okf.Index). Usage: go run ./internal/okf/okfindex
// [bundle], default knowledge; or go generate ./internal/okf.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hbaldwin98/reliab/internal/okf"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "okfindex:", err)
		os.Exit(1)
	}
}

// run regenerates the indexes of the bundle args names, default knowledge,
// saying on log which files it wrote.
func run(args []string, log io.Writer) error {
	root := "knowledge"
	if len(args) > 0 {
		root = args[0]
	}
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
		fmt.Fprintln(log, "okfindex: wrote", path)
	}
	return nil
}
