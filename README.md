# sqlglot-go

**SQL signature & parameter extraction for Go — inspired by python/sqlglot.**  
Normalize queries into stable **digests** and extract **parameters** across **MySQL / Postgres / SQL Server / Oracle**.

- **Stable digests** for query grouping, caching keys, and privacy-friendly analytics
- **Reliable parameter extraction** with byte offsets into the original SQL
- **Dialect-aware**: `$1` / `:name` / `@var`, PG dollar-quoted, Oracle `q'[]'`, etc.
- **Insert VALUES collapsing** (opt-in) for multi-row INSERTs
- **Time functions parameterization** (opt-in): `NOW()`, `SYSDATE`, `CURRENT_DATE`, …
- **No codegen required for users** — generated lexers/parsers are committed

---

## Install

```bash
go get github.com/tensafe/sqlglot-go@latest
```

Public package to import:

```go
import "github.com/tensafe/sqlglot-go/sqlglot"
```

---

## Quick Start

```go
package main

import (
	"fmt"
	"github.com/tensafe/sqlglot-go/sqlglot"
)

func main() {
	sql := `INSERT INTO t(a, ts) VALUES (1, NOW()), (2, NOW());`
	dig, params,sqltypes, err := sqlglot.Signature(sql, sqlglot.Options{
		Dialect:                sqlglot.MySQL,
		CollapseValuesInDigest: true,  // digest collapses multi-row VALUES to one tuple
		ParamizeTimeFuncs:      true,  // treat NOW()/CURRENT_DATE… as parameters
	})
	if err != nil { panic(err) }

	fmt.Println("Digest:", dig)
	for _, p := range params {
		fmt.Printf("P#%d %-10s [%d,%d): %q\n", p.Index, p.Type, p.Start, p.End, p.Value)
	}
  fmt.Println("SqlTypes:", sqltypes)
}
```

Example output:

```
Digest: INSERT INTO T(A, TS) VALUES(?, ?);
P#1 Number     [29,30): "1"
P#2 Timestamp  [32,37): "NOW()"
P#3 Number     [41,42): "2"
P#4 Timestamp  [44,49): "NOW()"
SqlTypes: [INSERT]
```

---

## API at a glance

```go
// High-level helpers:
func Signature(sql string, opt Options) (digest string, params []ExParam, err error)
func ExtractParams(sql string, opt Options) ([]ExParam, error)
func ResultFor(sql string, opt Options) (Result, error)

// Dialects:
const (
  MySQL Dialect = iota
  Postgres
  SQLServer
  Oracle
)

type Options struct {
  Dialect                Dialect // required
  CollapseValuesInDigest bool    // collapse INSERT ... VALUES (...),(...),... in digest
  ParamizeTimeFuncs      bool    // parameterize NOW/SYSDATE/CURRENT_DATE... (safe forms)
}

type Result struct {
  Digest string
  Params []ExParam // {Index, Type, Value, Start, End} with byte offsets into the original SQL
}

// Placeholders for future compatibility (return ErrNotImplemented):
Parse, ParseOne, Transpile
```

---

## Behavior & Dialects

**Normalization**
- Keywords uppercased; whitespace normalized; `IN (` spacing fixed.
- Numbers & strings → `?` (includes hex/bin, PG dollar-quoted, Oracle `q'[]'`).
- Binds remain params: `?`, `$1..$n`, `:name`, `@name`.
- `DATE|TIME|TIMESTAMP '...'` → a single parameter.
- Time functions *(opt-in)*: `NOW()`, `CURRENT_DATE`, `SYSDATE`, `SYSUTCDATETIME()`, `CURRENT_TIMESTAMP(3)`…
- Multi-row INSERT collapsing *(opt-in & safe)*: digest keeps **one** tuple; all params still extracted.
- Comments removed, including MySQL versioned comments `/*!40101 ...*/`.
- Multi-statement `;` supported.

**Dialect highlights**
- **Postgres**: `$$...$$`, `$tag$...$tag$` → single string param; `expr::TYPE` kept tight.
- **Oracle**: `q'[]' / () / {} / <>` strings; `DATE '...'`; JSON/XMLTABLE/MATCH_RECOGNIZE tokens supported.
- **SQL Server**: `AT TIME ZONE`, `OPENJSON`, named `@vars`.
- **MySQL**: `JSON_TABLE`, `X'ABCD'`, `0xFF`, versioned comments.

---

## Integration patterns

**A) HTTP/gRPC middleware logging**
```go
dig, params, _ := sqlglot.Signature(sql, sqlglot.Options{Dialect: sqlglot.Postgres})
logger.Infow("db.query", "digest", dig, "n_params", len(params))
```

**B) Metrics (group by digest)**
```go
labels := prometheus.Labels{"digest": dig, "db": "orders"}
dbQueryCounter.With(labels).Inc()
dbLatencyHist.With(labels).Observe(elapsed.Seconds())
```

**C) Redacted logging**
```go
safe := dig // literal-free
// store only `safe` in logs/audit
```

**D) Prepared statement cache key**
```go
key := fmt.Sprintf("%s|%s", dialectName(opt.Dialect), dig)
stmt := cache.GetOrPrepare(key, sql)
```

---

## Benchmarks

Run the included `bench_test.go`:

```bash
go test -run ^$ -bench . -benchmem ./...
go test -run ^$ -bench Signature -benchmem -benchtime=3s -count=3 ./...
```

Notes: use `-count=5` & `-run=^$` to avoid unit tests; pin your Go version for reproducibility.

---

## Troubleshooting (dev vs release)

- **Local dev in this repo**: do **not** `go get` the main module from itself. Just `go build`/`go test`.
- **Workspace** (`go.work`) recommended during development:
  ```bash
  go work init .
  # do NOT add a replace for the same module if it's already in `use .`
  ```
- **Testing a local unreleased change from another project**: in the consumer’s `go.mod`:
  ```go
  replace github.com/tensafe/sqlglot-go => /path/to/sqlglot-go
  ```
- **Release**: ensure no `replace` left in `go.mod`, run `go mod tidy`, then tag `vX.Y.Z`.

---

## Contributing

```bash
git clone https://github.com/tensafe/sqlglot-go
cd sqlglot-go
go mod tidy
go test ./...
```

PRs with dialect edge cases and failing tests are welcome.

---

## License

MIT
