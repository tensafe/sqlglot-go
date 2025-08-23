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

// 方言常见时间函数（统一大写）；用于 ParamizeTimeFuncs=true 时参数化
var timeFuncs = map[string]string{
	// 通用
	"CURRENT_TIMESTAMP": "Timestamp",
	"CURRENT_DATE":      "Date",
	"CURRENT_TIME":      "Time",
	"LOCALTIMESTAMP":    "Timestamp",

	// MySQL / PG
	"NOW":           "Timestamp",
	"UTC_TIMESTAMP": "Timestamp",

	// PostgreSQL
	"STATEMENT_TIMESTAMP":   "Timestamp",
	"TRANSACTION_TIMESTAMP": "Timestamp",
	"TIMEOFDAY":             "Timestamp",

	// SQL Server
	"GETDATE":        "Timestamp",
	"GETUTCDATE":     "Timestamp",
	"SYSDATETIME":    "Timestamp",
	"SYSUTCDATETIME": "Timestamp",

	// Oracle
	"SYSDATE":      "Timestamp",
	"SYSTIMESTAMP": "Timestamp",
}

// ---- 小工具 ----

func isEOFToken(t antlr.Token) bool {
	return t == nil || t.GetTokenType() == antlr.TokenEOF || t.GetText() == "<EOF>"
}

// 扫描任意 VALUES 段，若元组里出现绑定占位符（?/$n/:name/@p1）则返回 true
func valuesSectionHasBind(toks []antlr.Token) bool {
	inValues := false
	depth := 0
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if isEOFToken(t) {
			break
		}
		if t == nil || t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		txt := t.GetText()
		up := strings.ToUpper(txt)

		if up == "VALUES" {
			inValues = true
			depth = 0
			continue
		}
		if !inValues {
			continue
		}

		switch txt {
		case "(":
			depth++
			continue
		case ")":
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 && isBind(txt) {
			return true
		}
	}
	return false
}

// 扫描任意 VALUES 段，若元组里出现时间函数（ParamizeTimeFuncs=true 时视作变量）则返回 true
func valuesSectionHasTimeFunc(toks []antlr.Token) bool {
	inValues := false
	depth := 0
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if isEOFToken(t) {
			break
		}
		if t == nil || t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		txt := t.GetText()
		up := strings.ToUpper(txt)

		if up == "VALUES" {
			inValues = true
			depth = 0
			continue
		}
		if !inValues {
			continue
		}

		switch txt {
		case "(":
			depth++
			continue
		case ")":
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 {
			if _, ok := timeFuncs[up]; ok {
				return true
			}
		}
	}
	return false
}

// 主流程：把 token 流规范化渲染为 digest，并抽取参数
func renderAndExtract(original string, toks []antlr.Token, opt Options) (string, []ExParam) {
	var out strings.Builder
	var params []ExParam
	iParam := 1
	prevWord := ""
	tightNext := false // 用于压制“下一词前空格”，例如 "::" 后面的类型名
	parenDepth := 0    // 全局括号深度：遇到 '('++，遇到 ')'--；仅在 >0 时才输出 ')'

	// INSERT…VALUES 折叠控制：仅当用户允许且 VALUES 段无绑定变量时才折叠
	allowCollapseValues := opt.CollapseValuesInDigest && !valuesSectionHasBind(toks)
	if allowCollapseValues && opt.ParamizeTimeFuncs {
		// 时间函数也被视作“变量”时，VALUES 有时间函数则不折叠
		if valuesSectionHasTimeFunc(toks) {
			allowCollapseValues = false
		}
	}
	inValues := false    // 是否处于 VALUES 段
	valsDepth := 0       // VALUES 内括号层级（仅在 inValues=true 时维护）
	renderedTuples := 0  // 已渲染的顶层元组个数
	suppressOut := false // 折叠时抑制输出（但仍抽参）

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
		if suppressOut {
			return
		}
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
			if !suppressOut {
				out.WriteString("::")
			}
			// 紧贴后一个标识符/关键字
			tightNext = true
			return
		}
		// 其它操作符两侧空格
		if suppressOut {
			return
		}
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
				if !suppressOut {
					out.WriteString("?")
				}
				i = j
				prevWord = ""
				continue
			}
		}

		// —— PG 的 $tag$…$tag$（含 $$…$$）整体视为一个字符串参数 ——
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
				if !suppressOut {
					out.WriteString("?")
				}
				i = endIdx // 跳到结尾 $tag$
				prevWord = ""
				continue
			}
		}

		// —— 时间函数作为参数（可选） ——（NOW(), GETDATE(), CURRENT_DATE 等）
		if opt.ParamizeTimeFuncs {
			up := strings.ToUpper(text)
			if kind, isTime := timeFuncs[up]; isTime {
				// 可能是无参函数（NOW()）或关键字（CURRENT_DATE）
				if nv, _ := nextVisible(i); nv != nil && nv.GetText() == "(" {
					// 消费到配对的 ")"
					depth := 0
					endIdx := -1
					for j := i + 1; j < len(toks); j++ {
						tj := toks[j]
						if isEOFToken(tj) {
							break
						}
						if tj.GetChannel() != antlr.TokenDefaultChannel {
							continue
						}
						switch tj.GetText() {
						case "(":
							depth++
						case ")":
							depth--
							if depth == 0 {
								endIdx = j
								break
							}
						}
					}
					if endIdx != -1 {
						needSpaceBeforeWord()
						startByte := runeIndexToByte(original, t.GetStart())
						endByte := runeIndexToByte(original, toks[endIdx].GetStop()+1)
						params = append(params, ExParam{
							Index: iParam, Type: kind,
							Value: original[startByte:endByte],
							Start: startByte, End: endByte,
						})
						iParam++
						if !suppressOut {
							out.WriteString("?")
						}
						i = endIdx
						prevWord = ""
						continue
					}
				} else {
					// 无括号形式：CURRENT_DATE / SYSDATE
					needSpaceBeforeWord()
					startByte := runeIndexToByte(original, t.GetStart())
					endByte := runeIndexToByte(original, t.GetStop()+1)
					params = append(params, ExParam{
						Index: iParam, Type: kind,
						Value: original[startByte:endByte],
						Start: startByte, End: endByte,
					})
					iParam++
					if !suppressOut {
						out.WriteString("?")
					}
					prevWord = ""
					continue
				}
			}
		}

		// 参数/字面量
		switch {
		case isBind(text):
			needSpaceBeforeWord()
			addParam(&out, suppressOut, &params, &iParam, original, t, classifyBind(text))
			prevWord = ""
			continue

		case isNumberLiteral(text) || isStringLiteral(text):
			needSpaceBeforeWord()
			typ := "Number"
			if isStringLiteral(text) {
				typ = "String"
			}
			addParam(&out, suppressOut, &params, &iParam, original, t, typ)
			prevWord = ""
			continue
		}

		// 关键字 TRUE/FALSE/NULL 不参数化（也要正确插空格）
		if ok, _ := isBoolOrNull(text); ok && !opt.ParamizeTimeFuncs {
			needSpaceBeforeWord()
			up := strings.ToUpper(text)
			if !suppressOut {
				out.WriteString(up)
			}
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
			// 在 VALUES 顶层且允许折叠时，跳过元组分隔逗号
			if inValues && valsDepth == 0 && allowCollapseValues {
				prevWord = ","
				continue
			}
			if !suppressOut {
				out.WriteString(", ")
			}
			prevWord = ","
			continue

		case ".":
			// 点左右不加空格：schema.table / t.* 等
			if !suppressOut {
				out.WriteByte('.')
			}
			prevWord = "."
			continue

		case "(":
			parenDepth++ // 全局深度+1

			// 在 VALUES 段：顶层 "(" 表示一个新元组
			if inValues && valsDepth == 0 {
				if renderedTuples == 0 {
					// 第一个元组：正常渲染
					suppressOut = false
					renderedTuples = 1
				} else if allowCollapseValues {
					// 后续元组：只抽参，不渲染
					suppressOut = true
				}
			}
			// 仅在 VALUES 段内维护元组深度
			if inValues {
				valsDepth++
			}

			if !suppressOut {
				if strings.EqualFold(prevWord, "IN") {
					out.WriteString(" (")
				} else {
					out.WriteByte('(')
				}
			}
			prevWord = "("
			continue

		case ")":
			// 若这是“多余的右括号”，直接忽略，不输出
			if parenDepth == 0 {
				prevWord = ")"
				continue
			}
			parenDepth-- // 有匹配的 '('，再处理 VALUES 深度/输出

			// 结束一个括号层级（仅对 VALUES 路径做专门处理）
			if inValues && valsDepth > 0 {
				valsDepth--
				if valsDepth == 0 {
					// 一个顶层元组结束；若后面不是逗号，则 VALUES 段结束
					if nv, _ := nextVisible(i); nv == nil || nv.GetText() != "," {
						inValues = false
					}
					// 元组结束后恢复渲染，便于输出后续子句
					suppressOut = false
				}
			}
			if !suppressOut {
				out.WriteByte(')')
			}
			prevWord = ")"
			continue

		case ";":
			// 结束当前语句：重置所有临时状态
			inValues = false
			valsDepth = 0
			renderedTuples = 0
			suppressOut = false
			tightNext = false
			parenDepth = 0

			out.WriteByte(';')
			out.WriteByte(' ')
			prevWord = ";"
			continue

		case "*":
			// 若前一个是 '.'（t.*）则不加空格，否则作为“词”处理（前后留空格）
			if !suppressOut {
				if lastNonSpace() == '.' {
					out.WriteByte('*')
				} else {
					needSpaceBeforeWord()
					out.WriteByte('*')
				}
			}
			prevWord = "*"
			continue
		}

		// 一般单词（关键字/标识符）→ 统一大写，并按需插空格
		needSpaceBeforeWord()
		up := strings.ToUpper(text)

		// 遇到 VALUES 进入折叠识别段
		if up == "VALUES" {
			inValues = true
			valsDepth = 0
			renderedTuples = 0
			suppressOut = false
		}

		if !suppressOut {
			out.WriteString(up)
		}
		prevWord = up
	}

	d := strings.TrimSpace(out.String())
	// 折叠多空格，并保证 IN (
	d = strings.Join(strings.Fields(d), " ")
	d = strings.ReplaceAll(d, "IN(", "IN (")

	d = sanitizeParens(d)
	return d, params
}

// —— 渲染/抽参辅助 ——

// 添加一个参数：输出 "?"（可抑制），并记录原文与位置
func addParam(out *strings.Builder, suppress bool, arr *[]ExParam, iParam *int, original string, tok antlr.Token, typ string) {
	startRune := tok.GetStart()
	endRune := tok.GetStop() + 1
	startByte := runeIndexToByte(original, startRune)
	endByte := runeIndexToByte(original, endRune)

	if !suppress {
		out.WriteString("?")
	}
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
	switch up {
	case "DATE":
		if next != "" && isStringLiteral(next) {
			return true, "Date"
		}
	case "TIME":
		if next != "" && isStringLiteral(next) {
			return true, "Time"
		}
	case "TIMESTAMP":
		if next != "" && isStringLiteral(next) {
			return true, "Timestamp"
		}
	case "INTERVAL":
		if next != "" && isStringLiteral(next) {
			return true, "Interval"
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

// sanitizeParens 移除签名中“多余的右括号‘)’”。
// 以分号 ';' 为语句边界分别处理，不尝试补齐缺失的左括号，仅防多出来的 ')'.
func sanitizeParens(s string) string {
	var sb strings.Builder
	bal := 0 // 当前语句内的 '(' 余额
	for _, r := range s {
		switch r {
		case '(':
			bal++
			sb.WriteRune(r)
		case ')':
			if bal > 0 {
				bal--
				sb.WriteRune(r)
			} // 若 bal==0，跳过这个多余的 ')'
		case ';':
			// 语句结束：重置括号余额并写出分号
			bal = 0
			sb.WriteRune(r)
		default:
			sb.WriteRune(r)
		}
	}
	// 统一再做一次轻量空白收敛，和主体保持一致风格
	out := strings.TrimSpace(sb.String())
	out = strings.Join(strings.Fields(out), " ")
	out = strings.ReplaceAll(out, "IN(", "IN (")
	return out
}
