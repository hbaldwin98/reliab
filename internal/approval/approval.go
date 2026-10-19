// Package approval keeps a person's approvals of protected-path changes in
// reliab's local state. Each approval names the content approved, on the
// branch's base and in the working tree, by blob hash, so any later edit
// voids it. CI and the code host's owner review stay authoritative
// (knowledge/decisions/protected-path-approval.md).
package approval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Path is where approvals live, relative to the repo root. Hooks deny
// agents edits under .reliab/ and the approve command itself.
const Path = ".reliab/approvals.json"

// SchemaVersion is the approvals file version this build writes.
const SchemaVersion = "0.1.0"

// Approval is one person's approval of one protected file's change.
type Approval struct {
	Path string `json:"path"`
	// Blob is the approved content's blob hash, "" for a deletion. BaseBlob
	// is the file's blob on the merge base, "" when the branch added it.
	Blob     string `json:"blob"`
	BaseBlob string `json:"baseBlob"`
	// Base is the ref the change was measured from and MergeBase the commit
	// it resolved to.
	Base      string `json:"base"`
	MergeBase string `json:"mergeBase"`
	By        string `json:"by"`
	At        string `json:"at"`
}

type file struct {
	SchemaVersion string     `json:"schemaVersion"`
	Approvals     []Approval `json:"approvals"`
}

// Load reads the approvals under root; none is not an error.
func Load(root string) ([]Approval, error) {
	b, err := os.ReadFile(filepath.Join(root, Path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", Path, err)
	}
	if major, _, _ := strings.Cut(f.SchemaVersion, "."); major != "0" {
		return nil, fmt.Errorf("%s: schemaVersion %q: this reliab reads major version 0", Path, f.SchemaVersion)
	}
	return f.Approvals, nil
}

// Save writes approvals under root, replacing the file.
func Save(root string, as []Approval) error {
	b, err := json.MarshalIndent(file{SchemaVersion: SchemaVersion, Approvals: as}, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(root, Path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Match returns the approval of path's change from baseBlob to blob, or nil.
func Match(as []Approval, path, baseBlob, blob string) *Approval {
	for i := len(as) - 1; i >= 0; i-- {
		if a := as[i]; a.Path == path && a.Blob == blob && a.BaseBlob == baseBlob {
			return &as[i]
		}
	}
	return nil
}

// Put adds a, replacing any earlier approval of the same path.
func Put(as []Approval, a Approval) []Approval {
	out := as[:0:0]
	for _, x := range as {
		if x.Path != a.Path {
			out = append(out, x)
		}
	}
	return append(out, a)
}
