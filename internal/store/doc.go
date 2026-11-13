// Package store is the persistence layer. Queries are written in SQL and
// compiled with sqlc for both dialects (db/queries/{postgres,sqlite}).
// Migrations live in db/migrations and run with goose on startup.
package store
