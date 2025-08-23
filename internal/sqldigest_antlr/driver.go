package sqldigest_antlr

import (
	"fmt"
	ollex "tsql_digest_v4/internal/parsers/plsql"

	"github.com/antlr4-go/antlr/v4"

	mylex "tsql_digest_v4/internal/parsers/mysql"
	pglex "tsql_digest_v4/internal/parsers/postgresql"
	tsllex "tsql_digest_v4/internal/parsers/tsql"
)

// Dialect 支持的方言
type Dialect string

const (
	Postgres  Dialect = "postgres"
	MySQL     Dialect = "mysql"
	SQLServer Dialect = "sqlserver"
	Oracle    Dialect = "oracle"
)

// Options 控制生成行为
type Options struct {
	Dialect           Dialect
	ParamizeTimeFuncs bool // 是否把 NOW()/CURRENT_DATE 等也参数化（默认 false）
}

// ExParam 抽取到的参数
type ExParam struct {
	Index int
	Type  string // Number|String|Bool|Null|Date|Time|Timestamp|Interval|Bind|NamedBind
	Value string // 原文（未解码）
	Start int    // 原 SQL 的字节起点（含）
	End   int    // 原 SQL 的字节终点（不含）
}

// Result 产物
type Result struct {
	Digest string
	Params []ExParam
}

// BuildDigestANTLR：用 ANTLR 词法 token 流做“字面量/占位→? + 规范化渲染 + 抽参”
func BuildDigestANTLR(sql string, opt Options) (Result, error) {
	if opt.Dialect == "" {
		opt.Dialect = MySQL
	}
	// ANTLR 输入流（保留大小写）
	is := antlr.NewInputStream(sql)

	// 构造方言 lexer
	var lexer antlr.Lexer
	switch opt.Dialect {
	case Postgres:
		lexer = pglex.NewPostgreSQLLexer(is)
	case MySQL:
		lexer = mylex.NewMySQLLexer(is)
	case SQLServer:
		lexer = tsllex.NewTSqlLexer(is)
	case Oracle:
		lexer = ollex.NewPlSqlLexer(is)
	default:
		return Result{}, fmt.Errorf("unsupported dialect: %s", opt.Dialect)
	}

	// 拿到所有 token（包含隐藏通道）
	tokens := antlr.NewCommonTokenStream(lexer, 0)
	//if err := tokens.Fill(); err != nil {
	//	return Result{}, err
	//}
	tokens.Fill()

	// 基于可见 token 渲染 digest 并抽参（原文+位置）
	digest, params := renderAndExtract(sql, tokens.GetAllTokens(), opt)
	return Result{Digest: digest, Params: params}, nil
}
