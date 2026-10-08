// Package seeds embeds dummy data for local development. Seeds are tracked in
// their own version table (seed_migrations), separate from the schema, and
// must never run in production.
package seeds

import "embed"

//go:embed *.sql
var FS embed.FS
