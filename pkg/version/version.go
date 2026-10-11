// Package version holds the CLI version.
package version

import (
	"runtime/debug"
	"strings"
)

// Version is set at build time with
// -ldflags "-X github.com/serpapi/serpapi-cli/pkg/version.Version=1.2.3".
// Other builds, such as go install, report the module version instead.
var Version = "dev"

func init() {
	if Version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			Version = strings.TrimPrefix(v, "v")
		}
	}
}
