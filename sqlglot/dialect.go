package sqlglot

import core "github.com/tensafe/sqlglot-go/internal/sqldigest_antlr"

// Re-export core enums so users don't import internal.
type Dialect = core.Dialect

const (
	MySQL     = core.MySQL
	Postgres  = core.Postgres
	SQLServer = core.SQLServer
	Oracle    = core.Oracle
)

// Also surface core Options/Result/ExParam for convenience.
type (
	Options = core.Options
	Result  = core.Result
	ExParam = core.ExParam
)
