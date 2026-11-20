// Package migrations embebe los scripts SQL para que el binario los aplique al
// arrancar (ver repository.Migrate).
package migrations

import "embed"

// FS contiene los archivos NNNNNN_nombre.up.sql de esta carpeta.
//
//go:embed *.up.sql
var FS embed.FS
