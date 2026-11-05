package runner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"strings"
)

// FindFunc returns the inclusive line range of the function or method named
// name in src. A Go file is parsed; name may be Func or Type.Method. Other
// languages are scanned: a line declaring name( followed by a braced body,
// or for Python a def and its indented block. More than one match is an
// error, so a target never silently picks the wrong overload.
func FindFunc(file string, src []byte, name string) (int, int, error) {
	if path.Ext(file) == ".go" {
		return findGoFunc(file, src, name)
	}
	if path.Ext(file) == ".py" {
		return findPyFunc(src, name)
	}
	return findBraceFunc(src, name)
}

func findGoFunc(file string, src []byte, name string) (int, int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return 0, 0, err
	}
	recv, fn, isMethod := strings.Cut(name, ".")
	if !isMethod {
		fn, recv = recv, ""
	}
	var hits [][2]int
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != fn {
			continue
		}
		if recv != "" && recvName(fd) != recv {
			continue
		}
		hits = append(hits, [2]int{fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line})
	}
	return pick(hits, name)
}

func recvName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	t := fd.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

func pick(hits [][2]int, name string) (int, int, error) {
	switch len(hits) {
	case 0:
		return 0, 0, fmt.Errorf("no function %s", name)
	case 1:
		return hits[0][0], hits[0][1], nil
	}
	lines := make([]string, len(hits))
	for i, h := range hits {
		lines[i] = fmt.Sprint(h[0])
	}
	return 0, 0, fmt.Errorf("%d functions named %s (lines %s); name the file's lines instead", len(hits), name, strings.Join(lines, ", "))
}

// callPrefixRe marks a line where name( is a call, not a declaration.
var callPrefixRe = regexp.MustCompile(`(\.|\bnew\s+|\breturn\s+|\bawait\s+|=\s*|\(\s*|,\s*)$`)

func findBraceFunc(src []byte, name string) (int, int, error) {
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:] // Class.Method: the scanner matches the method
	}
	declRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*(<[^()]*>)?\s*\(`)
	lines := strings.Split(string(src), "\n")
	var hits [][2]int
	for i, line := range lines {
		code := stripLineComment(line)
		loc := declRe.FindStringIndex(code)
		if loc == nil || callPrefixRe.MatchString(code[:loc[0]]) {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(code), ";") {
			continue // a call statement or an abstract declaration
		}
		if end, ok := braceEnd(lines, i); ok {
			hits = append(hits, [2]int{i + 1, end + 1})
		}
	}
	return pick(hits, name)
}

func stripLineComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		return s[:i]
	}
	return s
}

// braceEnd finds the line closing the body that opens at or after line i.
// It skips string and char literals and comments roughly; an expression
// body (=>) ends at the first line ending in ';'.
func braceEnd(lines []string, i int) (int, bool) {
	depth, opened := 0, false
	inBlock := false
	for j := i; j < len(lines) && j < i+5000; j++ {
		line := lines[j]
		if !opened && j > i+3 {
			return 0, false // declaration with no body nearby
		}
		if !opened && strings.Contains(line, "=>") && !strings.Contains(line, "{") {
			for k := j; k < len(lines); k++ {
				if strings.HasSuffix(strings.TrimSpace(stripLineComment(lines[k])), ";") {
					return k, true
				}
			}
			return 0, false
		}
		for k := 0; k < len(line); k++ {
			c := line[k]
			if inBlock {
				if c == '*' && k+1 < len(line) && line[k+1] == '/' {
					inBlock = false
					k++
				}
				continue
			}
			switch c {
			case '/':
				if k+1 < len(line) && line[k+1] == '/' {
					k = len(line)
				} else if k+1 < len(line) && line[k+1] == '*' {
					inBlock = true
					k++
				}
			case '"', '\'', '`':
				k = skipString(line, k)
			case '{':
				depth++
				opened = true
			case '}':
				depth--
				if opened && depth == 0 {
					return j, true
				}
			}
		}
	}
	return 0, false
}

func skipString(line string, k int) int {
	q := line[k]
	for k++; k < len(line); k++ {
		switch line[k] {
		case '\\':
			k++
		case q:
			return k
		}
	}
	return k
}

func findPyFunc(src []byte, name string) (int, int, error) {
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	defRe := regexp.MustCompile(`^(\s*)(async\s+)?def\s+` + regexp.QuoteMeta(name) + `\s*\(`)
	lines := strings.Split(string(src), "\n")
	var hits [][2]int
	for i, line := range lines {
		m := defRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent := len(m[1])
		end := i
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if len(lines[j])-len(strings.TrimLeft(lines[j], " \t")) <= indent {
				break
			}
			end = j
		}
		hits = append(hits, [2]int{i + 1, end + 1})
	}
	return pick(hits, name)
}
