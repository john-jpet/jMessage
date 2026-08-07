// Package webui embeds the built frontend (web/dist) into the server
// binary. The Dockerfile copies web/dist into dist/ before `go build`
// so a single binary serves both the API and the SPA; dist/ contains
// only a placeholder in source control (go:embed requires the
// directory to exist at compile time).
package webui

import "embed"

//go:embed all:dist
var Dist embed.FS
