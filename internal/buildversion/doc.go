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
// internal/display's boot screen reads Version directly when display.New
// draws it. cmd/cloudkey calls Populate before display.New, so the screen
// receives metadata resolved from ldflags or embedded VCS build info.
package buildversion
