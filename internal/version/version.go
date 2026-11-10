package version

import "fmt"

var (
	// Version is the semantic version of rl-sync (injected at build time or defaulted from VERSION).
	Version = "2.0.0"
	// Commit is the short git SHA of the build.
	Commit = "HEAD"
	// BuildMode is either "production" or "development".
	BuildMode = "production"
	// BuildTime is the ISO-8601 UTC timestamp of the build.
	BuildTime = ""
)

// IsDev reports whether the binary was built in development/debug mode.
func IsDev() bool {
	return BuildMode == "development" || BuildMode == "dev" || BuildMode == "debug"
}

// Display returns a human-readable version string.
func Display() string {
	if IsDev() {
		if Commit != "" && Commit != "HEAD" {
			return fmt.Sprintf("v%s-dev (%s)", Version, Commit)
		}
		return fmt.Sprintf("v%s-dev", Version)
	}
	return fmt.Sprintf("v%s", Version)
}
