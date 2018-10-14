package build

import (
	"fmt"
	"runtime"
)

var (
	// Application supplied by the linker
	Application = "goexecutable"
	// Version supplied by the linker
	Version = "v0.0.0"
	// Revision supplied by the linker
	Revision = "00000000"
	// GoVersion supplied by the runtime
	GoVersion = runtime.Version()
)

// init returns the version
func init() {
	fmt.Printf("%s version %s git revision %s go version %s\n", Application, Version, Revision, GoVersion)
}
