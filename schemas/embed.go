package schemas

import "embed"

// Files contains immutable schema migrations embedded in the packaged application.
//
//go:embed *.sql
var Files embed.FS
