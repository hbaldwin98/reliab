package snapdiff

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"unicode/utf8"
)

// FileResult is the difference between an accepted snapshot file and a
// pending one.
type FileResult struct {
	Result
	// New is true when there is no accepted file yet; the change is then
	// additive-only.
	New bool
	// Binary is true when either file is not text. Binary files get no
	// label: Label is ValuesChanged when they differ, so a person compares
	// them.
	Binary bool
}

// CompareFiles labels the change from the accepted file to the pending one,
// as an adapter does for a snapshot tool that writes its pending snapshot
// beside the accepted one. A missing accepted file is a new snapshot. A
// byte-order mark does not count, and neither do line endings.
func CompareFiles(accepted, pending string) (FileResult, error) {
	cur, err := os.ReadFile(pending)
	if err != nil {
		return FileResult{}, err
	}
	var r FileResult
	acc, err := os.ReadFile(accepted)
	// if, not switch: Go coverage does not instrument case expressions, so
	// gremlins would call their mutants not covered.
	if errors.Is(err, fs.ErrNotExist) {
		r.New, acc, err = true, nil, nil
	}
	if err != nil {
		return FileResult{}, err
	}
	if !isText(cur) || !isText(acc) {
		r.Binary = true
		if !bytes.Equal(cur, acc) {
			r.Label = ValuesChanged
		}
		return r, nil
	}
	r.Result = Compare(text(acc), text(cur))
	return r, nil
}

func isText(b []byte) bool { return utf8.Valid(b) && bytes.IndexByte(b, 0) < 0 }

// text drops a leading byte-order mark.
func text(b []byte) string { return string(bytes.TrimPrefix(b, bom)) }

var bom = []byte{0xEF, 0xBB, 0xBF}
