// Package web embeds the built React dashboard (web/dist) into the binary.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
