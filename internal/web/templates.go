package web

import "embed"

//go:embed templates/*.html
var templates embed.FS
