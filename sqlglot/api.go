package sqlglot

import (
	"errors"
	core "github.com/tensafe/sqlglot-go/internal/sqldigest_antlr"
)

// Signature normalizes SQL into a stable digest and extracts parameters.
// This mirrors sqlglot's top-level convenience APIs (parse/transpile/normalize),
// but focuses on digest/params which your engine specializes in.
func Signature(sql string, opt Options) (digest string, params []ExParam, types []string, err error) {
	res, err := core.BuildDigestANTLR(sql, opt)
	if err != nil {
		return "", nil, nil, err
	}

	return res.Digest, res.Params, res.SQLType, nil
}

// ExtractParams returns only the parameters discovered in SQL.
func ExtractParams(sql string, opt Options) ([]ExParam, error) {
	res, err := core.BuildDigestANTLR(sql, opt)
	if err != nil {
		return nil, err
	}
	return res.Params, nil
}

// ResultFor mirrors your core Result for callers that prefer one-shot struct return.
func ResultFor(sql string, opt Options) (Result, error) {
	return core.BuildDigestANTLR(sql, opt)
}

// -----------------------------------------------------------------------------
// Placeholders (align naming with python sqlglot; implement later when AST ready)
// -----------------------------------------------------------------------------

var ErrNotImplemented = errors.New("not implemented")

// Parse returns AST nodes (placeholder for future AST integration).
func Parse(sql string, opt Options) (any, error) { // keep signature stable; change to []*ast.Node later
	return nil, ErrNotImplemented
}

// ParseOne returns a single AST node (placeholder).
func ParseOne(sql string, opt Options) (any, error) {
	return nil, ErrNotImplemented
}

// Transpile converts SQL between dialects (placeholder).
func Transpile(sql string, from Dialect, to Dialect, opt Options) (string, error) {
	return "", ErrNotImplemented
}
