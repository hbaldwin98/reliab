// Package templates embeds what reliab profile init writes into a repository.
package templates

import _ "embed"

// Profile is the reliability-profile.yaml skeleton. %NAME% and %CONDITION%
// are replaced.
//
//go:embed profile.yaml
var Profile string

// Agents is the testing-rules section added to AGENTS.md, between markers.
//
//go:embed agents.md
var Agents string
