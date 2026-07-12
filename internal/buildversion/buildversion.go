package buildversion

import "runtime/debug"

// Default values used when build metadata is unavailable (e.g. go run, local
// builds without VCS tags). They are intentionally human-readable sentinels
// rather than empty strings so that a binary built without VCS tagging still
// emits a meaningful startup banner.
const (
	DefaultVersion      = "dev"
	DefaultBuildRFC3339 = "1970-01-01T00:00:00Z"
	DefaultRevision     = "local"
)

// Info holds application build and version metadata.
type Info struct {
	Version      string `json:"version"`
	BuildRFC3339 string `json:"build_rfc3339"`
	Revision     string `json:"revision"`
}

// Version, BuildRFC3339, and Revision are set at link time via -X ldflags.
var (
	Version      = DefaultVersion
	BuildRFC3339 = DefaultBuildRFC3339
	Revision     = DefaultRevision
)

// Current holds the version info for the current build. It is populated by
// Populate() at startup and should not be modified after initialization.
var Current Info

// Populate replaces any variable still at its sentinel default with the
// corresponding value from Go's embedded VCS build info, then sets Current.
// Call it as the first statement of main().
func Populate() {
	Version, BuildRFC3339, Revision = readBuildInfo(Version, BuildRFC3339, Revision)
	Current = Info{Version: Version, BuildRFC3339: BuildRFC3339, Revision: Revision}
}

// readBuildInfo reads VCS and module metadata embedded at link time and
// delegates to applyBuildInfo. It is a no-op when build info is unavailable.
func readBuildInfo(version, buildRFC3339, revision string) (string, string, string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version, buildRFC3339, revision
	}
	return applyBuildInfo(version, buildRFC3339, revision, info)
}

// applyBuildInfo replaces each argument that still holds its default value
// with the corresponding value from info. Split out from readBuildInfo so
// the override logic can be unit tested against a hand-built *debug.BuildInfo
// instead of depending on the test binary's own embedded VCS metadata.
func applyBuildInfo(version, buildRFC3339, revision string, info *debug.BuildInfo) (string, string, string) {
	if version == DefaultVersion && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if revision == DefaultRevision && s.Value != "" {
				revision = s.Value
			}
		case "vcs.time":
			if buildRFC3339 == DefaultBuildRFC3339 && s.Value != "" {
				buildRFC3339 = s.Value
			}
		}
	}
	return version, buildRFC3339, revision
}
