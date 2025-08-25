package main

import (
	"fmt"
	"github.com/tensafe/sqlglot-go/sqlglot"
)

func main() {
	sql := `INSERT INTO t(a, ts) VALUES (1, NOW()), (2, NOW());`
	dig, params, sqltypes, err := sqlglot.Signature(sql, sqlglot.Options{
		Dialect:                sqlglot.MySQL,
		CollapseValuesInDigest: true, // digest collapses multi-row VALUES to one tuple
		ParamizeTimeFuncs:      true, // treat NOW()/CURRENT_DATE… as parameters
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("Digest:", dig)
	for _, p := range params {
		fmt.Printf("P#%d %-10s [%d,%d): %q\n", p.Index, p.Type, p.Start, p.End, p.Value)
	}
	fmt.Println("SqlTypes:", sqltypes)
}
