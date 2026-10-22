// Package docs embeds the pages reliab docs prints, so a reader gets the
// page for the installed version without the repository.
package docs

import _ "embed"

// Adapters is the adapter contract (adapters.md).
//
//go:embed adapters.md
var Adapters string

// Overview is what reliab does in a repository and what to run when
// (overview.md), for an agent asked to explain or use it.
//
//go:embed overview.md
var Overview string

// CI is the commands to wire into CI (ci.md).
//
//go:embed ci.md
var CI string

// Debug is the debug section, the adapters it was tried with, and the
// reliab debug commands (debug.md).
//
//go:embed debug.md
var Debug string

// Pages maps a reliab docs topic to its page.
var Pages = map[string]string{"adapters": Adapters, "ci": CI, "debug": Debug, "overview": Overview}
