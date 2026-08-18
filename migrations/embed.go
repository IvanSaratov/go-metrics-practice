package migrations

import "embed"

// Будем миграции держать внутри сервера для применения при старте
//
//go:embed *.sql
var Files embed.FS
