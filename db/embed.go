// Package db embeds the SQL migrations for both dialects.
package db

import "embed"

// Migrations holds migrations/postgres and migrations/sqlite.
//
//go:embed migrations
var Migrations embed.FS
