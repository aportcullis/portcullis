// Package assets embeds the built frontend into the single binary. The frontend (web/) builds into this package's dist/ directory; go:embed then bundles it. "all:dist" includes dotfiles so the directory is never empty.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the embedded frontend rooted at dist/.
func Dist() (fs.FS, error) { return fs.Sub(distFS, "dist") }
