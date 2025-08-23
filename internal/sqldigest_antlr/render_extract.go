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

// —— 非函数关键字（即便后面带 "(" 也不是“函数”）——
var nonFuncHeads = map[string]struct{}{
	"VALUES": {}, "SELECT": {}, "FROM": {}, "WHERE": {}, "GROUP": {}, "ORDER": {},
	"LIMIT": {}, "OFFSET": {}, "HAVING": {}, "JOIN": {}, "LEFT": {}, "RIGHT": {},
	"FULL": {}, "INNER": {}, "OUTER": {}, "CROSS": {}, "UNION": {}, "EXCEPT": {},
	"INTERSECT": {}, "INSERT": {}, "UPDATE": {}, "DELETE": {}, "MERGE": {}, "INTO": {},
	"SET": {}, "ON": {}, "USING": {}, "RETURNING": {}, "WITH": {}, "OVER": {},
}

// ---- 小工具 ----

func IsEOFToken(t antlr.Token) bool {
	return t == nil || t.GetTokenType() == antlr.TokenEOF || t.GetText() == "<EOF>"
}

func looksLikeIdent(s string) bool {
	if s == "" {
		return false
	}
	// 简单判定：首字符字母/下划线，或带引号的标识符
	if isQuotedIdent(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s)
	if r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z') {
		return true
	}
	return false
}

func isQuotedIdent(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '`', '"', '[':
		return true
	default:
		return false
	}
}

// 将 antlr 的 rune 索引用到 UTF-8 字节索引
func RuneIndexToByte(s string, runeIdx int) int {
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

// ---------- 注释区间扫描（MySQL 专用） ----------
type span struct{ S, E int } // [S,E) 字节区间

// 基于原文扫描 MySQL 注释：/*! ... */、/* ... */、-- ...\n、# ...\n。
// 忽略 ' " ` 引号中的模式，避免误删字符串内容。
func findMySQLCommentSpans(s string) []span {
	var spans []span
	b := []byte(s)
	n := len(b)

	inSingle, inDouble, inBack, esc := false, false, false, false // ' " ` 与转义
	for i := 0; i < n; {
		ch := b[i]

		// 处理转义（只在单/双引号里考虑 \ ）
		if (inSingle || inDouble) && ch == '\\' && !esc {
			esc = true
			i++
			continue
		}
		if esc {
			esc = false
			i++
			continue
		}

		// 引号状态机
		if ch == '\'' && !inDouble && !inBack {
			inSingle = !inSingle
			i++
			continue
		}
		if ch == '"' && !inSingle && !inBack {
			inDouble = !inDouble
			i++
			continue
		}
		if ch == '`' && !inSingle && !inDouble {
			inBack = !inBack
			i++
			continue
		}
		// 引号内不识别注释
		if inSingle || inDouble || inBack {
			i++
			continue
		}

		// 块注释 / 版本注释：/*...*/ 或 /*!...*/
		if ch == '/' && i+1 < n && b[i+1] == '*' {
			start := i
			i += 2
			for i+1 < n && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			if i+1 < n {
				i += 2 // 吃掉 "*/"
			}
			spans = append(spans, span{S: start, E: i})
			continue
		}

		// 行注释：-- ...\n
		if ch == '-' && i+1 < n && b[i+1] == '-' {
			start := i
			i += 2
			for i < n && b[i] != '\n' {
				i++
			}
			if i < n {
				i++ // 吃掉换行
			}
			spans = append(spans, span{S: start, E: i})
			continue
		}

		// 行注释：# ...\n
		if ch == '#' {
			start := i
			i++
			for i < n && b[i] != '\n' {
				i++
			}
			if i < n {
				i++
			}
			spans = append(spans, span{S: start, E: i})
			continue
		}

		i++
	}
	return spans
}

// 判断 token 的 [startByte,endByte) 是否与任何注释区间相交
func inAnySpan(startByte, endByte int, spans []span) bool {
	for _, sp := range spans {
		if !(endByte <= sp.S || startByte >= sp.E) {
			return true
		}
	}
	return false
}

// 向后看一个“可见且非注释”的 token
func nextVisibleWithComments(original string, toks []antlr.Token, i int, commentSpans []span) (antlr.Token, int) {
	for j := i + 1; j < len(toks); j++ {
		t := toks[j]
		if IsEOFToken(t) || t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		if IsWhitespace(t.GetText()) {
			continue
		}
		if len(commentSpans) > 0 {
			sb := RuneIndexToByte(original, t.GetStart())
			eb := RuneIndexToByte(original, t.GetStop()+1)
			if inAnySpan(sb, eb, commentSpans) {
				continue
			}
		}
		return t, j
	}
	return nil, i
}

// 向前看一个“可见且非注释”的 token
func prevVisibleWithComments(original string, toks []antlr.Token, i int, commentSpans []span) (antlr.Token, int) {
	for j := i - 1; j >= 0; j-- {
		t := toks[j]
		if IsEOFToken(t) || t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		if IsWhitespace(t.GetText()) {
			continue
		}
		if len(commentSpans) > 0 {
			sb := RuneIndexToByte(original, t.GetStart())
			eb := RuneIndexToByte(original, t.GetStop()+1)
			if inAnySpan(sb, eb, commentSpans) {
				continue
			}
		}
		return t, j
	}
	return nil, i
}

// 判断当前标识符（索引 idx）是否处于 "INTO <schema.>table (" 这种“表名+列清单”上下文
func isTableColumnListContext(original string, toks []antlr.Token, idx int, commentSpans []span) bool {
	// 向前允许出现 . 和 标识符/带引号标识符；最终应该遇到 INTO
	j := idx - 1
	for j >= 0 {
		t, k := prevVisibleWithComments(original, toks, j+1, commentSpans)
		if t == nil {
			break
		}
		txt := t.GetText()
		up := strings.ToUpper(txt)
		if up == "INTO" {
			return true
		}
		// 允许 schema.qual
		if txt == "." || looksLikeIdent(txt) || isQuotedIdent(txt) {
			j = k - 1
			continue
		}
		break
	}
	return false
}

// 扫描任意 VALUES 段，若元组里出现绑定占位符（?/$n/:name/@p1）则返回 true
func valuesSectionHasBind(toks []antlr.Token) bool {
	inValues := false
	depth := 0
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if IsEOFToken(t) {
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

// —— 检测各元组“值头部”是否兼容 ——
// 仅当：所有元组长度一致、且每列在所有元组里都“可折叠”为同一占位模式时返回 true。
// 当 paramizeFuncs==true：L(字面量)/B(绑定)/F(任意函数) 都视为可参数化占位 "P"；只要不是复杂表达式 O 就可折叠。
// 当 paramizeFuncs==false：L 必须对齐为 L；B 不会出现（前面已禁止 binds 折叠）；F 必须同名；遇到 O 不折叠。
func valuesFunctionsConsistent(original string, toks []antlr.Token, commentSpans []span, paramizeFuncs bool) bool {
	type tupleHeads []string
	var all []tupleHeads

	inValues := false
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if IsEOFToken(t) || t.GetChannel() != antlr.TokenDefaultChannel || IsWhitespace(t.GetText()) {
			continue
		}
		if strings.EqualFold(t.GetText(), "VALUES") {
			inValues = true
			continue
		}
		if !inValues || t.GetText() != "(" {
			continue
		}

		// 扫描一个顶层元组
		heads := tupleHeads{}
		j := i + 1
		for j < len(toks) {
			// 找当前值的“头部标签”
			h, nxt := classifyValueHead(original, toks, j, commentSpans, paramizeFuncs)
			if h == "" {
				h = "O"
			}
			heads = append(heads, h)

			// 跳到这个值结束（顶层逗号或元组右括号）
			k := nxt
			depth := 1
			for ; k < len(toks); k++ {
				tk := toks[k]
				if tk == nil || tk.GetChannel() != antlr.TokenDefaultChannel {
					continue
				}
				w := tk.GetText()
				if w == "(" {
					depth++
				} else if w == ")" {
					depth--
					if depth == 0 {
						break
					}
				} else if w == "," && depth == 1 {
					k++
					break
				}
			}
			if k >= len(toks) {
				break
			}
			// 元组结束
			if depth == 0 {
				j = k + 1
				break
			}
			// 下一个值
			j = k
		}
		all = append(all, heads)
		i = j - 1
	}

	if len(all) <= 1 {
		return true
	}
	// 长度一致
	l := len(all[0])
	for _, h := range all {
		if len(h) != l {
			return false
		}
	}
	// 列对列检查
	for c := 0; c < l; c++ {
		base := all[0][c]
		for r := 1; r < len(all); r++ {
			cur := all[r][c]
			// 任何一方是复杂表达式 O -> 不折叠
			if base == "O" || cur == "O" {
				return false
			}
			if paramizeFuncs {
				// 参数化模式：P(可参数化，占位) / L 都视为最终占位 "?"，因此兼容
				continue
			}
			// 非参数化函数模式：要求同类
			// 1) 字面量必须对齐为 L
			if base == "L" && cur == "L" {
				continue
			}
			// 2) 函数：必须同名（形如 F:NAME）
			if strings.HasPrefix(base, "F:") && strings.HasPrefix(cur, "F:") && base == cur {
				continue
			}
			// 其它组合 -> 不折叠
			return false
		}
	}
	return true
}

func splitHead(h string) (kind, fn string) {
	if strings.HasPrefix(h, "F:") {
		return "F", strings.TrimPrefix(h, "F:")
	}
	switch h {
	case "L", "B", "O":
		return h, ""
	default:
		return "O", ""
	}
}

// 取一个值的“头部标签”及扫描到的索引（从 idx 开始，跳过空白/注释）
// 返回 (head, nextIndex)
// head: L/B/F:<NAME>/O
// 当 paramizeFuncs=true：L/B/F 都归并为 "P"（可参数化占位）；复杂为 "O"
func classifyValueHead(original string, toks []antlr.Token, idx int, commentSpans []span, paramizeFuncs bool) (string, int) {
	// 跳过空白/注释
	i := idx
	for i < len(toks) {
		t := toks[i]
		if t == nil || t.GetChannel() != antlr.TokenDefaultChannel || IsWhitespace(t.GetText()) {
			i++
			continue
		}
		if len(commentSpans) > 0 {
			sb := RuneIndexToByte(original, t.GetStart())
			eb := RuneIndexToByte(original, t.GetStop()+1)
			if inAnySpan(sb, eb, commentSpans) {
				i++
				continue
			}
		}
		break
	}
	if i >= len(toks) {
		return "", i
	}
	t := toks[i]
	w := t.GetText()
	up := strings.ToUpper(w)

	// +/- 数字 -> 字面量
	if w == "-" || w == "+" {
		if nv, _ := nextVisibleWithComments(original, toks, i, commentSpans); nv != nil && isNumberLiteral(nv.GetText()) {
			if paramizeFuncs {
				return "P", i + 1
			}
			return "L", i + 1
		}
		return "O", i
	}

	// 字面量 / 绑定
	if isNumberLiteral(w) || isStringLiteral(w) || reDollarTag.MatchString(w) {
		if paramizeFuncs {
			return "P", i + 1
		}
		return "L", i + 1
	}
	if isBind(w) {
		if paramizeFuncs {
			return "P", i + 1
		}
		return "B", i + 1
	}
	// DATE/TIME/TIMESTAMP/INTERVAL '...'
	if ok, _ := isDateLike("", up, "X"); ok {
		if nv, _ := nextVisibleWithComments(original, toks, i, commentSpans); nv != nil && isStringLiteral(nv.GetText()) {
			if paramizeFuncs {
				return "P", i + 1
			}
			return "L", i + 1
		}
	}

	// 括号开头 -> 复杂
	if w == "(" {
		return "O", i
	}

	// 无括号时间关键字 -> 当作函数头
	if _, isTime := timeFuncs[up]; isTime {
		if nv, _ := nextVisibleWithComments(original, toks, i, commentSpans); nv == nil || nv.GetText() != "(" {
			if paramizeFuncs {
				return "P", i + 1
			}
			return "F:" + up, i + 1
		}
		// 有括号则走函数路径
	}

	// 标识符/限定名 + "(" 视作函数头；但 VALUES/INTO 等非函数关键字除外
	if looksLikeIdent(w) || isQuotedIdent(w) {
		headStart := i
		headEnd := i
		k := i
		for {
			tk := toks[k]
			if tk == nil || tk.GetChannel() != antlr.TokenDefaultChannel {
				break
			}
			word := tk.GetText()
			if !looksLikeIdent(word) && !isQuotedIdent(word) {
				break
			}
			headEnd = k
			// schema.func
			if nvDot, di := nextVisibleWithComments(original, toks, k, commentSpans); nvDot != nil && nvDot.GetText() == "." {
				if nvWord, wi := nextVisibleWithComments(original, toks, di, commentSpans); nvWord != nil && (looksLikeIdent(nvWord.GetText()) || isQuotedIdent(nvWord.GetText())) {
					k = wi
					headEnd = k
					continue
				}
			}
			break
		}
		headUp := strings.ToUpper(toks[headStart].GetText())
		if _, bad := nonFuncHeads[headUp]; bad {
			return "O", headEnd + 1
		}
		if isTableColumnListContext(original, toks, headStart, commentSpans) {
			return "O", headEnd + 1
		}
		if nv, _ := nextVisibleWithComments(original, toks, headEnd, commentSpans); nv != nil && nv.GetText() == "(" {
			if paramizeFuncs {
				return "P", headEnd + 1
			}
			// 非参数化：保留函数名（取最后一段）
			nameTok := toks[headEnd]
			name := strings.ToUpper(strings.Trim(nameTok.GetText(), "`\"[]"))
			return "F:" + name, headEnd + 1
		}
		return "O", headEnd + 1
	}

	return "O", i + 1
}

// 主流程：把 token 流规范化渲染为 digest，并抽取参数
func RenderAndExtract(original string, toks []antlr.Token, opt Options) (string, []ExParam) {
	var out strings.Builder
	var params []ExParam
	iParam := 1
	prevWord := ""
	tightNext := false // 用于压制“下一词前空格”，例如 "::" 后面的类型名
	parenDepth := 0    // 全局括号深度：遇到 '('++，遇到 ')'--；仅在 >0 时才输出 ')'

	// MySQL 注释预处理：找出注释区间，循环中跳过落入区间的 token
	var commentSpans []span
	if opt.Dialect == MySQL {
		commentSpans = findMySQLCommentSpans(original)
	}

	// INSERT…VALUES 折叠控制（硬门禁 + 形状一致性）
	allowCollapseValues := false
	if opt.CollapseValuesInDigest {
		allowCollapseValues = !valuesSectionHasBind(toks) &&
			valuesFunctionsConsistent(original, toks, commentSpans, opt.ParamizeTimeFuncs)
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

	// 获取下一个“可见且非空白”的 token（同时跳过注释区）
	nextVisible := func(i int) (antlr.Token, int) {
		return nextVisibleWithComments(original, toks, i, commentSpans)
	}

	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if IsEOFToken(t) {
			break
		}
		if t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		text := t.GetText()
		if IsWhitespace(text) {
			continue
		}

		// —— 注释过滤：当前 token 落在注释区间内则跳过 ——
		if len(commentSpans) > 0 {
			startByte := RuneIndexToByte(original, t.GetStart())
			endByte := RuneIndexToByte(original, t.GetStop()+1)
			if inAnySpan(startByte, endByte, commentSpans) {
				continue
			}
		}

		// 合并 DATE/TIME/TIMESTAMP/INTERVAL '...' 为一个参数
		if ok, kind := isDateLike(prevWord, strings.ToUpper(text), peekText(toks, i+1)); ok {
			if nv, j := nextVisible(i); nv != nil && isStringLiteral(nv.GetText()) {
				needSpaceBeforeWord()
				startRune := t.GetStart()
				endRune := nv.GetStop() + 1
				startByte := RuneIndexToByte(original, startRune)
				endByte := RuneIndexToByte(original, endRune)
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
				if IsEOFToken(tj) {
					break
				}
				if tj == nil || tj.GetChannel() != antlr.TokenDefaultChannel {
					continue
				}
				if len(commentSpans) > 0 {
					sb := RuneIndexToByte(original, tj.GetStart())
					eb := RuneIndexToByte(original, tj.GetStop()+1)
					if inAnySpan(sb, eb, commentSpans) {
						continue
					}
				}
				if tj.GetText() == text {
					endIdx = j
					break
				}
			}
			if endIdx != -1 {
				needSpaceBeforeWord()
				startByte := RuneIndexToByte(original, t.GetStart())
				endByte := RuneIndexToByte(original, toks[endIdx].GetStop()+1)
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

		// —— 时间关键字/时间函数参数化（安全形态） ——
		if opt.ParamizeTimeFuncs {
			up := strings.ToUpper(text)
			kind, isTime := timeFuncs[up]
			if isTime {
				// 1) 无括号关键字：SYSDATE / CURRENT_DATE ...
				if up == "SYSDATE" || up == "SYSTIMESTAMP" || up == "CURRENT_DATE" || up == "CURRENT_TIME" || up == "CURRENT_TIMESTAMP" {
					if nv1, _ := nextVisible(i); nv1 == nil || nv1.GetText() != "(" {
						needSpaceBeforeWord()
						startByte := RuneIndexToByte(original, t.GetStart())
						endByte := RuneIndexToByte(original, t.GetStop()+1)
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
				// 2) func() / func(3)（仅“零参或单一数字精度”两种安全形态）
				if nv1, idx1 := nextVisible(i); nv1 != nil && nv1.GetText() == "(" {
					if nv2, idx2 := nextVisible(idx1); nv2 != nil {
						// func()
						if nv2.GetText() == ")" {
							needSpaceBeforeWord()
							startByte := RuneIndexToByte(original, t.GetStart())
							endByte := RuneIndexToByte(original, nv2.GetStop()+1)
							params = append(params, ExParam{
								Index: iParam, Type: kind,
								Value: original[startByte:endByte],
								Start: startByte, End: endByte,
							})
							iParam++
							if !suppressOut {
								out.WriteString("?")
							}
							i = idx2
							prevWord = ""
							continue
						}
						// func(3)
						if isNumberLiteral(nv2.GetText()) {
							if nv3, idx3 := nextVisible(idx2); nv3 != nil && nv3.GetText() == ")" {
								needSpaceBeforeWord()
								startByte := RuneIndexToByte(original, t.GetStart())
								endByte := RuneIndexToByte(original, nv3.GetStop()+1)
								params = append(params, ExParam{
									Index: iParam, Type: kind,
									Value: original[startByte:endByte],
									Start: startByte, End: endByte,
								})
								iParam++
								if !suppressOut {
									out.WriteString("?")
								}
								i = idx3
								prevWord = ""
								continue
							}
						}
					}
				}
			}
		}

		// —— 通用函数参数化（当 ParamizeTimeFuncs=true 时启用） ——
		if opt.ParamizeTimeFuncs && looksLikeIdent(text) {
			upHead := strings.ToUpper(text)
			if _, bad := nonFuncHeads[upHead]; !bad && !isTableColumnListContext(original, toks, i, commentSpans) {
				// 允许限定名：schema.func
				nameEnd := i
				k := i
				for {
					tk := toks[k]
					if tk == nil || tk.GetChannel() != antlr.TokenDefaultChannel {
						break
					}
					w := tk.GetText()
					if !looksLikeIdent(w) && !isQuotedIdent(w) {
						break
					}
					nameEnd = k
					if nvDot, di := nextVisibleWithComments(original, toks, k, commentSpans); nvDot != nil && nvDot.GetText() == "." {
						if nvWord, wi := nextVisibleWithComments(original, toks, di, commentSpans); nvWord != nil && (looksLikeIdent(nvWord.GetText()) || isQuotedIdent(nvWord.GetText())) {
							k = wi
							nameEnd = k
							continue
						}
					}
					break
				}
				// 必须紧跟 "("
				if nv, parIdx := nextVisibleWithComments(original, toks, nameEnd, commentSpans); nv != nil && nv.GetText() == "(" {
					depth := 1
					endIdx := -1
					for j := parIdx + 1; j < len(toks); j++ {
						tj := toks[j]
						if tj == nil || tj.GetChannel() != antlr.TokenDefaultChannel {
							continue
						}
						if len(commentSpans) > 0 {
							sb := RuneIndexToByte(original, tj.GetStart())
							eb := RuneIndexToByte(original, tj.GetStop()+1)
							if inAnySpan(sb, eb, commentSpans) {
								continue
							}
						}
						w := tj.GetText()
						if w == "(" {
							depth++
						} else if w == ")" {
							depth--
							if depth == 0 {
								endIdx = j
								break
							}
						} else if w == ";" {
							// 跨语句不合法
							break
						}
					}
					if endIdx != -1 {
						needSpaceBeforeWord()
						startByte := RuneIndexToByte(original, t.GetStart())
						endByte := RuneIndexToByte(original, toks[endIdx].GetStop()+1)
						params = append(params, ExParam{
							Index: iParam, Type: "Func",
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
			if inValues && valsDepth == 0 && allowCollapseValues {
				// 折叠时跳过元组分隔逗号
				prevWord = ","
				continue
			}
			if !suppressOut {
				out.WriteString(", ")
			}
			prevWord = ","
			continue

		case ".":
			if !suppressOut {
				out.WriteByte('.')
			}
			prevWord = "."
			continue

		case "(":
			parenDepth++

			// 在 VALUES 段：顶层 "(" 表示一个新元组
			if inValues && valsDepth == 0 {
				if renderedTuples == 0 {
					// 第一个元组：正常渲染
					suppressOut = false
					renderedTuples = 1
				} else {
					// 后续元组：只有允许折叠时才抑制输出
					suppressOut = allowCollapseValues
				}
			}
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
			parenDepth-- // 有匹配的 '('

			// 结束一个括号层级（仅对 VALUES 路径做专门处理）
			if inValues && valsDepth > 0 {
				valsDepth--
				if valsDepth == 0 {
					if nv, _ := nextVisible(i); nv == nil || nv.GetText() != "," {
						inValues = false
					}
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

			// —— 只有在已有内容时才真正输出分号（避免前导 ';'） ——
			if lastNonSpace() != 0 {
				out.WriteByte(';')
				out.WriteByte(' ')
			}
			prevWord = ";"
			continue

		case "*":
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
	startByte := RuneIndexToByte(original, startRune)
	endByte := RuneIndexToByte(original, endRune)

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

// 下一个可见 token 的文本（不考虑注释，供 isDateLike 的 peek 用）
func peekText(toks []antlr.Token, i int) string {
	for j := i; j < len(toks); j++ {
		t := toks[j]
		if IsEOFToken(t) {
			return ""
		}
		if t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		if IsWhitespace(t.GetText()) {
			continue
		}
		return t.GetText()
	}
	return ""
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
