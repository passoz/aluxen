// Package migrations embute o SQL das migrações dos dois dialetos para dentro do
// binário. O pacote vive ao lado dos arquivos .sql porque `go:embed` só alcança
// o diretório do próprio pacote (não há como embutir ../../migrations de dentro
// de internal/infrastructure/db).
package migrations

import "embed"

// FS contém migrations/sqlite/*.sql e migrations/postgres/*.sql.
//
//go:embed sqlite/*.sql postgres/*.sql
var FS embed.FS
