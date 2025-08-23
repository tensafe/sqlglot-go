// Package sqlglot exposes a sqlglot-like API for Go.
// It wraps internal/ANTLR-based parsing to produce stable "signatures"
// (normalized SQL digests) and parameter extraction, inspired by python sqlglot.
//
// Typical usage:
//
//	dig, params, err := sqlglot.Signature(sql, sqlglot.Options{Dialect: sqlglot.Postgres})
package sqlglot
