// Package version holds build-time VCS metadata, populated via -ldflags at build time.
package version

// These values are injected at build time via -ldflags (see scripts/build.sh).
var (
	// CommitHash is the git commit the binary was built from.
	CommitHash = "unknown"
	// BuildTime is the time the binary was built.
	BuildTime = "unknown"
	// CommitTime is the time of the commit the binary was built from.
	CommitTime = "unknown"
)
