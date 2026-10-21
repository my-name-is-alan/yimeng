// Package migrations embeds the versioned SQL migrations applied at startup.
// File names must be NNNN_description.sql; versions are applied in ascending order.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
