// Package website embeds the built product website (website/dist), served
// by cmd/phoedox-website (ADR-0054). Build it with `bun run build` first.
package website

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built website, or nil if it has not been built yet.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
