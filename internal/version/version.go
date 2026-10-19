// Package version holds the build version, stamped by the release pipeline.
package version

import "strings"

// Version is set at build time with
// -ldflags "-X github.com/hbaldwin98/reliab/internal/version.Version=vX.Y.Z".
var Version = "dev"

// IsDev reports whether this is a development build rather than a release:
// the default "dev" or any version ending in "-dev".
func IsDev() bool { return Version == "dev" || strings.HasSuffix(Version, "-dev") }
