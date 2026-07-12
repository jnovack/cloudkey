// Package buildversion holds the application-level build metadata variables
// (Version, BuildRFC3339, Revision) and the Populate function that resolves
// them from Go's embedded VCS build info.
//
// The variables are set at link time via ldflags, e.g.:
//
//	-X github.com/jnovack/cloudkey/internal/buildversion.Version=v1.2.3
//
// cmd/cloudkey must call Populate as the first statement of main(): it
// replaces any variable still at its sentinel default ("dev", epoch, "local")
// with the value from runtime/debug.ReadBuildInfo, then populates Current,
// the snapshot the rest of the binary reads.
//
// internal/display's boot screen reads Version directly rather than Current,
// since it draws during package init() — before main() has had a chance to
// call Populate() — so it only ever sees the raw ldflags value, not the
// debug.ReadBuildInfo fallback.
package buildversion
