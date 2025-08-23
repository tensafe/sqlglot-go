//go:build bindfuzz_with_antlr

package tests

import (
	"github.com/antlr4-go/antlr/v4"
	"regexp"
	"strings"
	d "tsql_digest_v4/internal/sqldigest_antlr"
	"unicode/utf8"
)

//func BindifySQL(sql string, dialect d.Dialect, ratio float64, r *rand.Rand) string {
//	if ratio <= 0 {
//		return sql
//	}
//	lex := makeLexer(dialect, antlr.NewInputStream(sql))
//	// 拉平所有 token（含隐藏通道）
//	var toks []antlr.Token
//	for {
//		t := lex.NextToken()
//		if t == nil {
//			break
//		}
//		toks = append(toks, t)
//		if t.GetTokenType() == antlr.TokenEOF {
//			break
//		}
//	}
//
//	nextIdx := maxExistingBindIndex(sql, dialect) + 1
//
//	var out strings.Builder
//	lastByte := 0
//	for _, t := range toks {
//		if t == nil || t.GetTokenType() == antlr.TokenEOF {
//			break
//		}
//		startByte := runeIndexToByte(sql, t.GetStart())
//		endByte := runeIndexToByte(sql, t.GetStop()+1)
//
//		text := t.GetText()
//		ch := t.GetChannel()
//
//		replace := false
//		if ch == antlr.TokenDefaultChannel {
//			// 仅在默认通道上考虑替换
//			if isStringLiteralToken(text, dialect) || isNumberLiteralToken(text) {
//				if r.Float64() < ratio {
//					replace = true
//				}
//			}
//		}
//		if replace {
//			out.WriteString(sql[lastByte:startByte])
//			out.WriteString(makeBind(dialect, &nextIdx))
//			lastByte = endByte
//		}
//	}
//	out.WriteString(sql[lastByte:])
//	return out.String()
//}

/* ---------- 词法器工厂 & 辅助 ---------- */

func makeLexer(di d.Dialect, input antlr.CharStream) antlr.Lexer {
	switch di {
	case d.Postgres:
		return pg.NewPostgreSQLLexer(input)
	case d.SQLServer:
		return ms.NewTSqlLexer(input)
	case d.Oracle:
		return or.NewPlSqlLexer(input)
	default:
		return my.NewMySQLLexer(input)
	}
}

func isNumberLiteralToken(text string) bool {
	if text == "" {
		return false
	}
	// 十进制/小数/科学计数或 0x.. 十六进制
	if text[0] == '-' || text[0] == '+' {
		if len(text) > 1 && text[1] >= '0' && text[1] <= '9' {
			return true
		}
	}
	if text[0] >= '0' && text[0] <= '9' {
		return true
	}
	tl := strings.ToLower(text)
	return strings.HasPrefix(tl, "0x")
}

func isStringLiteralToken(text string, di d.Dialect) bool {
	if text == "" {
		return false
	}
	if text[0] == '\'' || text[0] == '"' {
		return true
	}
	// PG dollar-quote
	if rePGDollar.MatchString(text) {
		return true
	}
	// Oracle q'[]'
	if (text[0] == 'q' || text[0] == 'Q') && len(text) > 2 && text[1] == '\'' {
		return true
	}
	return false
}

var rePGDollar = regexp.MustCompile(`^\$[A-Za-z_0-9]*\$$`)

func runeIndexToByte(s string, runeIdx int) int {
	if runeIdx <= 0 {
		return 0
	}
	i := 0
	for pos := 0; pos < runeIdx && i < len(s); {
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
		pos++
	}
	return i
}

// 与文本版一致：
func makeBind(dialect d.Dialect, nextIdx *int) string        { /* 同上 */ return "" }
func maxExistingBindIndex(sql string, dialect d.Dialect) int { /* 同上 */ return 0 }
