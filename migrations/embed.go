// Package migrations embeds the versioned schema migrations so the migrate
// command works without the SQL files on disk.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
