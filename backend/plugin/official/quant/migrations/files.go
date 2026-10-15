package migrations

import "embed"

//go:embed generation5/*.sql
var Files embed.FS
