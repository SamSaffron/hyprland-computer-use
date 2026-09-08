package app

import (
	"fmt"
	"io"
)

// Release builds set these through linker flags; plain Go builds stay identifiable.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "hyprland-computer-use %s (commit %s, built %s)\n", Version, Commit, Date)
}
