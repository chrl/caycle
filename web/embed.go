// Package web embeds the built React dashboard. Run `npm run build` in this
// directory before `go build` to refresh it.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
