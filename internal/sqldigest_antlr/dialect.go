package sqldigest_antlr

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	ollex "github.com/tensafe/sqlglot-go/internal/parsers/plsql"
	"sort"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	mylex "github.com/tensafe/sqlglot-go/internal/parsers/mysql"
	pglex "github.com/tensafe/sqlglot-go/internal/parsers/postgresql"
	tsllex "github.com/tensafe/sqlglot-go/internal/parsers/tsql"
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
	Dialect                Dialect
	ParamizeTimeFuncs      bool // 是否把 NOW()/CURRENT_DATE 等也参数化（默认 false）
	CollapseValuesInDigest bool
}

type ExParam struct {
	Index     int
	IndexHash string
	Type      string
	Value     string
	Start     int
	End       int
	// 新增：INSERT ... VALUES (...) , (...), ... 的行/列位置（1-based）
	Row int // 第几行 VALUES 元组
	Col int // 该行里的第几个参数（按出现顺序）
}

// Result 产物
type Result struct {
	Digest  string    `json:"digest,omitempty"`
	Params  []ExParam `json:"params,omitempty"`
	SQLType []string  `json:"sql_type,omitempty"` // 新增：按多语句返回每条类型
}

func MD5Prefix4(v interface{}) string {
	var s string
	switch val := v.(type) {
	case string:
		s = val
	case int, int8, int16, int32, int64:
		s = fmt.Sprintf("%d", val)
	case uint, uint8, uint16, uint32, uint64:
		s = fmt.Sprintf("%d", val)
	case float32, float64:
		s = fmt.Sprintf("%f", val)
	default:
		s = fmt.Sprintf("%v", val) // 兜底处理
	}

	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])[:4]
}

// BuildDigestANTLR：用 ANTLR 词法 token 流做“字面量/占位→? + 规范化渲染 + 抽参”
func BuildDigestANTLR(sql string, opt Options) (Result, error) {
	if opt.Dialect == "" {
		opt.Dialect = MySQL
	}
	//if !opt.CollapseValuesInDigest {
	//	opt.CollapseValuesInDigest = true
	//}
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
	digest, params := RenderAndExtract(sql, tokens.GetAllTokens(), opt)
	// 新增：如是 INSERT ... VALUES(...)，为每个参数标上 Row/Col
	annotateInsertRowCol(sql, opt.Dialect, &params)

	for i, p := range params {
		params[i].IndexHash = MD5Prefix4(p.Index)
	}

	// 新增：多语句类型收集
	stmtInfos := SplitStatements(sql, tokens.GetAllTokens(), opt)
	sqlTypes := make([]string, 0, len(stmtInfos))
	for _, s := range stmtInfos {
		sqlTypes = append(sqlTypes, s.Type)
	}
	// 如果啥都没识别出来（全空白），给个 UNKNOWN
	if len(sqlTypes) == 0 {
		sqlTypes = []string{"UNKNOWN"}
	}

	return Result{
		Digest:  digest,
		Params:  params,
		SQLType: sqlTypes,
	}, nil

	//return Result{Digest: digest, Params: params}, nil
}

// annotateInsertRowCol：若是 INSERT ... VALUES (...) , (...) ...，给每个参数打上 Row/Col
func annotateInsertRowCol(original string, dialect Dialect, params *[]ExParam) {
	if len(*params) == 0 {
		return
	}
	// 先粗判一下：不是 INSERT/VALUES 就直接返回
	up := strings.ToUpper(original)
	if !strings.Contains(up, "INSERT") || !strings.Contains(up, "VALUES") {
		return
	}

	// 重新词法（只看可见 token，拿到括号/逗号等精确信息）
	is := antlr.NewInputStream(original)
	var lexer antlr.Lexer
	switch dialect {
	case Postgres:
		lexer = pglex.NewPostgreSQLLexer(is)
	case MySQL:
		lexer = mylex.NewMySQLLexer(is)
	case SQLServer:
		lexer = tsllex.NewTSqlLexer(is)
	case Oracle:
		lexer = ollex.NewPlSqlLexer(is)
	default:
		lexer = mylex.NewMySQLLexer(is)
	}

	toks := antlr.NewCommonTokenStream(lexer, 0)
	toks.Fill()
	all := toks.GetAllTokens()

	// 定位 VALUES 段；在 VALUES 之后按“顶层括号”切出每个元组的字节区间
	type rng struct{ s, e int } // [s,e)
	var ranges []rng

	inValues := false
	started := false
	depth := 0
	tupleStartRune := -1

	nextVis := func(i int) antlr.Token {
		for j := i + 1; j < len(all); j++ {
			t := all[j]
			if IsEOFToken(t) {
				return nil
			}
			if t.GetChannel() != antlr.TokenDefaultChannel {
				continue
			}
			if IsWhitespace(t.GetText()) {
				continue
			}
			return t
		}
		return nil
	}

	for i := 0; i < len(all); i++ {
		t := all[i]
		if IsEOFToken(t) {
			break
		}
		if t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		txt := t.GetText()
		up := strings.ToUpper(txt)
		// 进入 VALUES 段
		if up == "VALUES" {
			inValues = true
			continue
		}
		if !inValues {
			continue
		}

		switch txt {
		case "(":
			if !started {
				started = true
				depth = 0
			}
			if depth == 0 {
				tupleStartRune = t.GetStart()
			}
			depth++
		case ")":
			if !started {
				continue
			}
			depth--
			if depth == 0 && tupleStartRune >= 0 {
				// 一个顶层元组结束
				s := RuneIndexToByte(original, tupleStartRune)
				e := RuneIndexToByte(original, t.GetStop()+1)
				ranges = append(ranges, rng{s: s, e: e})
				tupleStartRune = -1

				// 看下一个可见 token：若是逗号继续，否则结束 VALUES 段
				if nv := nextVis(i); nv != nil && nv.GetText() == "," {
					// 吞掉逗号，继续找下一个 '('
					continue
				} else {
					// 不是逗号，说明 VALUES 段结束（可能遇到 ON DUPLICATE/ON CONFLICT/RETURNING 等）
					i = len(all)
				}
			}
		}
	}

	if len(ranges) == 0 {
		return // 不是多行 VALUES（可能是 INSERT ... SELECT 或 ORACLE 的 INSERT ALL）
	}

	// 将参数按起始位置排序（稳定起见）
	arr := *params
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].Start < arr[j].Start })

	// 对每个元组区间内的参数，标 Row/Col
	for rIdx, rg := range ranges {
		rowParamsIdx := make([]int, 0, 4)
		for i := range arr {
			if arr[i].Start >= rg.s && arr[i].Start < rg.e {
				rowParamsIdx = append(rowParamsIdx, i)
			}
		}
		// 按出现顺序赋 Col（1-based）
		for c, pi := range rowParamsIdx {
			arr[pi].Row = rIdx + 1
			arr[pi].Col = c + 1
		}
	}

	// 写回
	*params = arr
}
