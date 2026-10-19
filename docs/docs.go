// Package docs embeds the pages reliab docs prints, so a reader gets the
// page for the installed version without the repository.
package docs

import _ "embed"

// Adapters is the adapter contract (adapters.md).
//
//go:embed adapters.md
var Adapters string

// Pages maps a reliab docs topic to its page.
var Pages = map[string]string{"adapters": Adapters}
