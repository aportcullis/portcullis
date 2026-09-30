// Package migrations embeds the ordered SQL migration files so they ship inside the binary and can be applied at startup.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
