package sqldigest_antlr

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/antlr4-go/antlr/v4"
)

var reDollarTag = regexp.MustCompile(`^\$[A-Za-z_0-9]*\$$`)

// 常见多字符操作符
var multiOp = map[string]struct{}{
	">=": {}, "<=": {}, "<>": {}, "!=": {},
	"||": {}, "->": {}, "->>": {}, "#>": {}, "#>>": {},
	"@>": {}, "<@": {}, "::": {},
}

var reDollarN = regexp.MustCompile(`^\$(\d+)$`)
var reColon = regexp.MustCompile(`^:[A-Za-z_][A-Za-z_0-9]*$|^:\d+$`)
var reAtNamed = regexp.MustCompile(`^@[A-Za-z_][A-Za-z_0-9]*$`)

// ---- 小工具 ----

func isEOFToken(t antlr.Token) bool {
	return t == nil || t.GetTokenType() == antlr.TokenEOF || t.GetText() == "<EOF>"
}

// 主流程：把 token 流规范化渲染为 digest，并抽取参数
func renderAndExtract(original string, toks []antlr.Token, opt Options) (string, []ExParam) {
	var out strings.Builder
	var params []ExParam
	iParam := 1
	prevWord := ""
	tightNext := false // 用于压制“下一词前空格”，例如 "::" 后面的类型名

	// 工具：取输出里最后一个非空格字符
	lastNonSpace := func() byte {
		s := out.String()
		for i := len(s) - 1; i >= 0; i-- {
			if s[i] != ' ' {
				return s[i]
			}
		}
		return 0
	}
	needSpaceBeforeWord := func() {
		// 若前一个 token 要求与下一个“紧贴”，则跳过一次空格
		if tightNext {
			tightNext = false
			return
		}
		last := lastNonSpace()
		if last == 0 {
			return
		}
		// 在这些字符后面“通常不需要”空格：开括号、逗号、点、空格
		switch last {
		case '(', ',', '.', ' ':
			return
		default:
			out.WriteByte(' ')
		}
	}
	writeOp := func(op string) {
		// "::"（PG cast）要紧贴：expr::type
		if op == "::" {
			out.WriteString("::")
			// 紧贴后一个标识符/关键字
			tightNext = true
			return
		}
		// 其它操作符两侧空格
		if lastNonSpace() != 0 && lastNonSpace() != ' ' {
			out.WriteByte(' ')
		}
		out.WriteString(op)
		out.WriteByte(' ')
	}

	// 获取下一个“可见且非空白”的 token
	nextVisible := func(i int) (antlr.Token, int) {
		for j := i + 1; j < len(toks); j++ {
			t := toks[j]
			if isEOFToken(t) {
				return nil, i
			}
			if t.GetChannel() != antlr.TokenDefaultChannel {
				continue
			}
			if isWhitespace(t.GetText()) {
				continue
			}
			return t, j
		}
		return nil, i
	}

	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if isEOFToken(t) {
			break
		}
		if t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		text := t.GetText()
		if isWhitespace(text) {
			// 完全忽略空白；我们自己决定何时插空格
			continue
		}

		// 合并 DATE/TIME/TIMESTAMP/INTERVAL '...' 为一个参数
		if ok, kind := isDateLike(prevWord, strings.ToUpper(text), peekText(toks, i+1)); ok {
			if nv, j := nextVisible(i); nv != nil && isStringLiteral(nv.GetText()) {
				needSpaceBeforeWord()
				startRune := t.GetStart()
				endRune := nv.GetStop() + 1
				startByte := runeIndexToByte(original, startRune)
				endByte := runeIndexToByte(original, endRune)
				params = append(params, ExParam{
					Index: iParam, Type: kind,
					Value: original[startByte:endByte],
					Start: startByte, End: endByte,
				})
				iParam++
				out.WriteString("?")
				i = j
				prevWord = ""
				continue
			}
		}

		if reDollarTag.MatchString(text) {
			// 从当前 token 向后找到相同的 $tag$
			endIdx := -1
			for j := i + 1; j < len(toks); j++ {
				tj := toks[j]
				if isEOFToken(tj) {
					break
				}
				if tj == nil || tj.GetChannel() != antlr.TokenDefaultChannel {
					continue
				}
				if tj.GetText() == text {
					endIdx = j
					break
				}
			}
			if endIdx != -1 {
				needSpaceBeforeWord()
				startByte := runeIndexToByte(original, t.GetStart())
				endByte := runeIndexToByte(original, toks[endIdx].GetStop()+1)
				params = append(params, ExParam{
					Index: iParam, Type: "String",
					Value: original[startByte:endByte],
					Start: startByte, End: endByte,
				})
				iParam++
				out.WriteString("?")
				i = endIdx // 跳到结尾 $tag$
				prevWord = ""
				continue
			}
		}

		// 参数/字面量
		switch {
		case isBind(text):
			needSpaceBeforeWord()
			addParam(&out, &params, &iParam, original, t, classifyBind(text))
			prevWord = ""
			continue

		case isNumberLiteral(text) || isStringLiteral(text):
			needSpaceBeforeWord()
			typ := "Number"
			if isStringLiteral(text) {
				typ = "String"
			}
			addParam(&out, &params, &iParam, original, t, typ)
			prevWord = ""
			continue
		}

		// 关键字 TRUE/FALSE/NULL 不参数化（也要正确插空格）
		if ok, _ := isBoolOrNull(text); ok && !opt.ParamizeTimeFuncs {
			needSpaceBeforeWord()
			up := strings.ToUpper(text)
			out.WriteString(up)
			prevWord = up
			continue
		}

		// 其它 token：操作符/括号/逗号/点/单词
		if _, isOp := multiOp[text]; isOp {
			writeOp(text)
			prevWord = text
			continue
		}

		switch text {
		case ",":
			out.WriteString(", ")
			prevWord = ","
			continue
		case ".":
			// 点左右不加空格：schema.table / t.* 等
			out.WriteByte('.')
			prevWord = "."
			continue
		case "(":
			// IN ( 需要空格；函数名紧贴 "("
			if strings.EqualFold(prevWord, "IN") {
				out.WriteString(" (")
			} else {
				out.WriteByte('(')
			}
			prevWord = "("
			continue
		case ")":
			out.WriteByte(')')
			prevWord = ")"
			continue
		case "*":
			// 若前一个是 '.'（t.*）则不加空格，否则作为“词”处理（前后留空格）
			if lastNonSpace() == '.' {
				out.WriteByte('*')
			} else {
				needSpaceBeforeWord()
				out.WriteByte('*')
			}
			prevWord = "*"
			continue
		}

		// 一般单词（关键字/标识符）→ 统一大写，并按需插空格
		needSpaceBeforeWord()
		up := strings.ToUpper(text)
		out.WriteString(up)
		prevWord = up
	}

	d := strings.TrimSpace(out.String())
	// 折叠多空格，并保证 IN (
	d = strings.Join(strings.Fields(d), " ")
	d = strings.ReplaceAll(d, "IN(", "IN (")
	return d, params
}

// —— 渲染/抽参辅助 ——

// 添加一个参数：输出 "?"，并记录原文与位置
func addParam(out *strings.Builder, arr *[]ExParam, iParam *int, original string, tok antlr.Token, typ string) {
	startRune := tok.GetStart()
	endRune := tok.GetStop() + 1
	startByte := runeIndexToByte(original, startRune)
	endByte := runeIndexToByte(original, endRune)

	out.WriteString("?")
	*arr = append(*arr, ExParam{
		Index: *iParam, Type: typ,
		Value: original[startByte:endByte],
		Start: startByte, End: endByte,
	})
	*iParam++
}

// 文本是否是绑定占位符
func isBind(text string) bool {
	return text == "?" || reDollarN.MatchString(text) || reColon.MatchString(text) || reAtNamed.MatchString(text)
}
func classifyBind(text string) string {
	if text == "?" {
		return "Bind"
	}
	if reDollarN.MatchString(text) {
		return "Bind" // $1
	}
	if reColon.MatchString(text) || reAtNamed.MatchString(text) {
		return "NamedBind"
	}
	return "Bind"
}

// 布尔/NULL（按原样输出，不参数化）
func isBoolOrNull(text string) (bool, string) {
	up := strings.ToUpper(text)
	switch up {
	case "TRUE", "FALSE":
		return true, "Bool"
	case "NULL":
		return true, "Null"
	}
	return false, ""
}

// 数字/字符串字面量判断（保守策略足以应付签名）
func isNumberLiteral(text string) bool {
	if text == "" {
		return false
	}
	// 0-9 开头；或 0x.. 十六进制
	if text[0] >= '0' && text[0] <= '9' {
		return true
	}
	if strings.HasPrefix(strings.ToLower(text), "0x") {
		return true
	}
	return false
}
func isStringLiteral(text string) bool {
	if text == "" {
		return false
	}
	if text[0] == '\'' || text[0] == '"' {
		return true
	}
	// PostgreSQL: $$...$$ / E'..'
	if strings.HasPrefix(text, "$$") || strings.HasPrefix(text, "E'") || strings.HasPrefix(text, "e'") {
		return true
	}
	// Oracle: q'[...]'
	if len(text) > 2 && (text[0] == 'q' || text[0] == 'Q') && text[1] == '\'' {
		return true
	}
	// x'ABCD' / b'0101'
	if len(text) > 2 && (text[0] == 'x' || text[0] == 'X' || text[0] == 'b' || text[0] == 'B') && text[1] == '\'' {
		return true
	}
	return false
}

// DATE/TIME/TIMESTAMP/INTERVAL '...' 合并判断
func isDateLike(prevWord, curr, next string) (bool, string) {
	up := strings.ToUpper(curr)
	if up == "DATE" || up == "TIME" || up == "TIMESTAMP" || up == "INTERVAL" {
		if next != "" && isStringLiteral(next) {
			kind := strings.Title(strings.ToLower(up)) // Date/Time/Timestamp/Interval
			return true, kind
		}
	}
	return false, ""
}

// 下一个可见 token 的文本
func peekText(toks []antlr.Token, i int) string {
	for j := i; j < len(toks); j++ {
		t := toks[j]
		if isEOFToken(t) {
			return ""
		}
		if t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		if isWhitespace(t.GetText()) {
			continue
		}
		return t.GetText()
	}
	return ""
}

// 将 antlr 的 rune 索引用到 UTF-8 字节索引
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
