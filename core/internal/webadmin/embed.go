package webadmin

import "embed"

// FS holds the no-build web admin/player (ui/*: HTML, CSS, ES modules).
//
//go:embed all:ui
var FS embed.FS
