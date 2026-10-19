package adapters

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/hbaldwin98/reliab/internal/report"
)

// Cobertura XML coverage, written by many coverage tools across stacks.
// reliab reads it for counts only: uncovered lines are not findings,
// because coverage gaps do not drive tests (R7; changed-lines coverage is a
// CI backstop with a threshold, see knowledge/decisions/external-adapters.md).
type coberturaDoc struct {
	XMLName xml.Name `xml:"coverage"`
	Sources []string `xml:"sources>source"`
	Classes []struct {
		Filename string `xml:"filename,attr"`
		Lines    []struct {
			Number string `xml:"number,attr"`
			Hits   string `xml:"hits,attr"`
		} `xml:"lines>line"`
	} `xml:"packages>package>classes>class"`
}

// parseCobertura counts the lines a Cobertura report measured and how many
// tests reached, each file and line once. With in.Changed set it counts
// only changed lines.
func parseCobertura(raw []byte, in Input) (parsed, error) {
	var doc coberturaDoc
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return parsed{}, fmt.Errorf("not a Cobertura coverage report: %w", err)
	}
	source := ""
	if len(doc.Sources) > 0 {
		source = doc.Sources[0]
	}
	type key struct {
		file string
		line int
	}
	hit := map[key]bool{}
	for _, c := range doc.Classes {
		file := filepath.FromSlash(c.Filename)
		if !filepath.IsAbs(file) && source != "" {
			file = filepath.Join(filepath.FromSlash(source), file)
		}
		file = rel(in.Root, file)
		for _, l := range c.Lines {
			n, err := strconv.Atoi(l.Number)
			if err != nil {
				return parsed{}, fmt.Errorf("%s: line number %q", c.Filename, l.Number)
			}
			if in.Changed != nil && !in.Changed[file][n] {
				continue
			}
			k := key{file, n}
			h, _ := strconv.ParseFloat(l.Hits, 64)
			hit[k] = hit[k] || h > 0
		}
	}
	c := &report.LineCounts{Valid: len(hit)}
	for _, covered := range hit {
		if covered {
			c.Covered++
		}
	}
	return parsed{coverage: c}, nil
}
