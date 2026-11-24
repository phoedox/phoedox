// Package web embeds the built frontend (web/dist) into the server binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// ProtoPlayer plays whiteboard prototypes (player/proto-player.js): the
// web app bundles it, the server puts it into share pages and exports.
//
//go:embed player/proto-player.js
var ProtoPlayer string

// Tokens are the design tokens (colours of both themes), read by server-side
// drawings such as whiteboard pictures so they use the app's colours.
//
//go:embed src/styles/tokens.css
var Tokens string

// Dist returns the built frontend, or nil if it has not been built yet.
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
