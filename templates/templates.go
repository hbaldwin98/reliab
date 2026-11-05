// Package templates holds the testing-rules block for a repository's
// AGENTS.md and example reliab.yaml files.
package templates

import _ "embed"

// Agents is the testing-rules section added to AGENTS.md, between markers.
//
//go:embed agents.md
var Agents string
