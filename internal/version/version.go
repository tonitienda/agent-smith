// Package version exposes build-time metadata for Agent Smith binaries.
package version

// Values are overridden by -ldflags in release builds.
var (
	Name    = "smith"
	Version = "dev"
	Commit  = "unknown"
)

// String returns the human-readable CLI version.
func String() string {
	if Commit == "" || Commit == "unknown" {
		return Name + " " + Version
	}
	return Name + " " + Version + " (" + Commit + ")"
}
