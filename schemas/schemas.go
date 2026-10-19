// Package schemas embeds reliab's JSON Schemas and validates documents
// against them. The schemas are the contract; Go types mirror them.
package schemas

import (
	"bytes"
	"embed"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed *.schema.json
var files embed.FS

// Supported major versions of each document kind. A document whose
// schemaVersion has another major is rejected.
const (
	ProfileMajor  = 0
	ReportMajor   = 0
	FindingsMajor = 0
	BaselineMajor = 0
)

// Kind names a schema.
type Kind string

const (
	Profile  Kind = "profile"
	Report   Kind = "report"
	Findings Kind = "findings"
	Baseline Kind = "baseline"
)

const baseURL = "https://github.com/hbaldwin98/reliab/schemas/"

var compiled = sync.OnceValues(func() (map[Kind]*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	for _, name := range []string{"profile.schema.json", "report.schema.json", "findings.schema.json", "baseline.schema.json"} {
		raw, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if err := c.AddResource(baseURL+name, doc); err != nil {
			return nil, err
		}
	}
	out := map[Kind]*jsonschema.Schema{}
	for _, k := range []Kind{Profile, Report, Findings, Baseline} {
		s, err := c.Compile(baseURL + string(k) + ".schema.json")
		if err != nil {
			return nil, err
		}
		out[k] = s
	}
	return out, nil
})

// Validate checks doc, a value decoded from JSON (maps, slices, strings,
// json.Number or float64, bools, nil), against the schema for kind, and
// checks its schemaVersion major.
func Validate(kind Kind, doc any) error {
	all, err := compiled()
	if err != nil {
		return fmt.Errorf("compiling schemas: %w", err)
	}
	if err := all[kind].Validate(doc); err != nil {
		return err
	}
	return checkMajor(kind, doc)
}

func checkMajor(kind Kind, doc any) error {
	m, _ := doc.(map[string]any)
	v, _ := m["schemaVersion"].(string)
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return fmt.Errorf("schemaVersion %q is not semver", v)
	}
	want := map[Kind]int{Profile: ProfileMajor, Report: ReportMajor, Findings: FindingsMajor, Baseline: BaselineMajor}[kind]
	if n != want {
		return fmt.Errorf("%s schemaVersion %s: this reliab supports major version %d", kind, v, want)
	}
	return nil
}
