// Package migrations embeds the SQL migration files so cmd/migrate is a
// self-contained binary.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
