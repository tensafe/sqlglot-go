package main

import (
	"fmt"
	"github.com/tensafe/sqlglot-go/sqlglot"
)

//TIP <p>To run your code, right-click the code and select <b>Run</b>.</p> <p>Alternatively, click
// the <icon src="AllIcons.Actions.Execute"/> icon in the gutter and select the <b>Run</b> menu item from here.</p>

func main() {
	sql := `INSERT INTO t(a, ts) VALUES (1, NOW()), (2, NOW());`
	dig, params, err := sqlglot.Signature(sql, sqlglot.Options{
		Dialect:                sqlglot.MySQL,
		CollapseValuesInDigest: true,
		ParamizeTimeFuncs:      true,
	})
	if err != nil {
		return
	}
	// dig / params 即可用于归一化与参数审计
	//_ = dig
	//_ = params
	fmt.Println(dig)
	fmt.Println(params)
	return
}
