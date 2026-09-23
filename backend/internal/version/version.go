// Package version holds build-time metadata injected by GoReleaser's -ldflags (see
// .goreleaser.yaml). A binary built any other way (go build, go run) keeps the "dev" defaults --
// there is no way to fake a release version without actually going through the release pipeline.
package version

var (
	// Version is the release tag (e.g. "v0.3.0"), or "dev" outside a GoReleaser build.
	Version = "dev"
	// Commit is the short git SHA the binary was built from.
	Commit = "unknown"
	// Date is the build time in RFC 3339, from GoReleaser's own clock, not the machine building it.
	Date = "unknown"
)
