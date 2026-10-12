// Package version holds build metadata injected via -ldflags.
package version

import "runtime"

// Set at build time:
//
//	-ldflags "-X github.com/sulaimantok/semar/internal/version.Version=v0.1.0
//	          -X github.com/sulaimantok/semar/internal/version.Commit=abc123"
var (
	Version = "dev"
	Commit  = "unknown"
)

// Info is the build information exposed by the API.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Get returns the current build information.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}
