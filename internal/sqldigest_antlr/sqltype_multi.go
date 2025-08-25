package sqldigest_antlr

import (
	"github.com/antlr4-go/antlr/v4"
	"strings"
)

// --- 语句主关键词归类 ---
func classifyMainVerb(up string) string {
	switch up {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "MERGE",
		"REPLACE", "UPSERT": // 少量方言
		return up
	}
	return ""
}

func classifyOtherLead(up string, _ Dialect) string {
	switch up {
	case "WITH", "EXPLAIN", "ANALYZE",
		"CREATE", "ALTER", "DROP", "TRUNCATE",
		"GRANT", "REVOKE",
		"SET", "SHOW", "PRAGMA",
		"USE", "CALL", "EXEC",
		"BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE":
		return up
	}
	return ""
}

// 跳过 WITH CTE 列表，返回跟在 CTE 之后的主语句动词
func scanForwardMainVerbAfterWITH(original string, toks []antlr.Token, i int, spans []span, d Dialect) string {
	nv := func(idx int) (antlr.Token, int) { return nextVisibleWithComments(original, toks, idx, spans) }
	// 可选 RECURSIVE
	if t, j := nv(i); t != nil && strings.EqualFold(t.GetText(), "RECURSIVE") {
		i = j
	}
	for {
		// CTE 名 [ (cols...) ] AS ( ... )
		// 1) 名称或限定名
		if t, j := nv(i); t != nil && (looksLikeIdent(t.GetText()) || isQuotedIdent(t.GetText())) {
			i = j
			// 2) 可选列清单
			if t2, j2 := nv(i); t2 != nil && t2.GetText() == "(" {
				depth := 1
				k := j2
				for k+1 < len(toks) && depth > 0 {
					k++
					w := toks[k].GetText()
					if w == "(" {
						depth++
					} else if w == ")" {
						depth--
					}
				}
				i = k
			}
			// 3) 期望 AS ( ... )
			if t3, j3 := nv(i); t3 != nil && strings.EqualFold(t3.GetText(), "AS") {
				if t4, j4 := nv(j3); t4 != nil && t4.GetText() == "(" {
					depth := 1
					k := j4
					for k+1 < len(toks) && depth > 0 {
						k++
						w := toks[k].GetText()
						if w == "(" {
							depth++
						} else if w == ")" {
							depth--
						}
					}
					i = k
				}
			}
		}
		// 逗号 => 下一条 CTE；否则跳出到主语句
		if t5, j5 := nv(i); t5 != nil && t5.GetText() == "," {
			i = j5
			continue
		}
		break
	}
	// 下一可见 token 应是主语句的动词
	if t, _ := nv(i); t != nil {
		if kw := classifyMainVerb(strings.ToUpper(t.GetText())); kw != "" {
			return kw
		}
		if kw := classifyOtherLead(strings.ToUpper(t.GetText()), d); kw != "" {
			return kw
		}
	}
	return ""
}

type StmtInfo struct {
	Type      string
	StartTok  int
	EndTok    int
	StartByte int
	EndByte   int
}

func stmtTypeInRange(original string, toks []antlr.Token, lo, hi int, spans []span, d Dialect) string {
	depth := 0
	//nv := func(i int) (antlr.Token, int) { return nextVisibleWithComments(original, toks, i, spans) }

	for i := lo; i <= hi; i++ {
		t := toks[i]
		if IsEOFToken(t) || t.GetChannel() != antlr.TokenDefaultChannel || IsWhitespace(t.GetText()) {
			continue
		}
		if len(spans) > 0 {
			sb := RuneIndexToByte(original, t.GetStart())
			eb := RuneIndexToByte(original, t.GetStop()+1)
			if inAnySpan(sb, eb, spans) {
				continue
			}
		}
		up := strings.ToUpper(t.GetText())
		switch up {
		case "(":
			depth++
			continue
		case ")":
			if depth > 0 {
				depth--
			}
			continue
		case ";":
			if depth == 0 {
				return "UNKNOWN"
			}
		}
		if depth > 0 {
			continue
		}
		if up == "WITH" {
			if kw := scanForwardMainVerbAfterWITH(original, toks, i, spans, d); kw != "" {
				return kw
			}
			return "WITH"
		}
		if up == "EXPLAIN" || up == "ANALYZE" {
			// 找后面的主动词
			for j := i + 1; j <= hi; j++ {
				tj := toks[j]
				if IsEOFToken(tj) || tj.GetChannel() != antlr.TokenDefaultChannel || IsWhitespace(tj.GetText()) {
					continue
				}
				if len(spans) > 0 {
					sb := RuneIndexToByte(original, tj.GetStart())
					eb := RuneIndexToByte(original, tj.GetStop()+1)
					if inAnySpan(sb, eb, spans) {
						continue
					}
				}
				upj := strings.ToUpper(tj.GetText())
				if upj == "PLAN" || upj == "FOR" || upj == "VERBOSE" {
					continue
				}
				if kw := classifyMainVerb(upj); kw != "" {
					return kw
				}
				break
			}
			return up
		}
		if kw := classifyMainVerb(up); kw != "" {
			return kw
		}
		if kw := classifyOtherLead(up, d); kw != "" {
			return kw
		}
	}
	return "UNKNOWN"
}

func SplitStatements(original string, toks []antlr.Token, opt Options) []StmtInfo {
	var spans []span
	if opt.Dialect == MySQL {
		spans = findMySQLCommentSpans(original)
	}
	firstVis := func(from int) (int, antlr.Token) {
		for i := from; i < len(toks); i++ {
			t := toks[i]
			if IsEOFToken(t) || t.GetChannel() != antlr.TokenDefaultChannel || IsWhitespace(t.GetText()) {
				continue
			}
			if len(spans) > 0 {
				sb := RuneIndexToByte(original, t.GetStart())
				eb := RuneIndexToByte(original, t.GetStop()+1)
				if inAnySpan(sb, eb, spans) {
					continue
				}
			}
			return i, t
		}
		return -1, nil
	}
	lastVis := func(from, to int) (int, antlr.Token) {
		for i := to; i >= from; i-- {
			t := toks[i]
			if IsEOFToken(t) || t.GetChannel() != antlr.TokenDefaultChannel || IsWhitespace(t.GetText()) {
				continue
			}
			if len(spans) > 0 {
				sb := RuneIndexToByte(original, t.GetStart())
				eb := RuneIndexToByte(original, t.GetStop()+1)
				if inAnySpan(sb, eb, spans) {
					continue
				}
			}
			return i, t
		}
		return -1, nil
	}

	var out []StmtInfo
	depth := 0
	stmtStart := -1
	if i, t := firstVis(0); t != nil {
		stmtStart = i
	}
	flush := func(endTok int) {
		if stmtStart < 0 || endTok < stmtStart {
			return
		}
		h, ht := lastVis(stmtStart, endTok)
		l, lt := firstVis(stmtStart)
		if ht == nil || lt == nil || h < l {
			stmtStart = -1
			return
		}
		sb := RuneIndexToByte(original, lt.GetStart())
		eb := RuneIndexToByte(original, ht.GetStop()+1)
		typ := stmtTypeInRange(original, toks, l, h, spans, opt.Dialect)
		out = append(out, StmtInfo{Type: typ, StartTok: l, EndTok: h, StartByte: sb, EndByte: eb})
		if i, t := firstVis(endTok + 1); t != nil {
			stmtStart = i
		} else {
			stmtStart = -1
		}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if IsEOFToken(t) || t.GetChannel() != antlr.TokenDefaultChannel || IsWhitespace(t.GetText()) {
			continue
		}
		if len(spans) > 0 {
			sb := RuneIndexToByte(original, t.GetStart())
			eb := RuneIndexToByte(original, t.GetStop()+1)
			if inAnySpan(sb, eb, spans) {
				continue
			}
		}
		switch t.GetText() {
		case "(":
			depth++
		case ")":
			if depth > 0 {
				depth--
			}
		case ";":
			if depth == 0 {
				flush(i)
			}
		}
	}
	if stmtStart >= 0 {
		last := len(toks) - 1
		for last >= 0 && IsEOFToken(toks[last]) {
			last--
		}
		if last >= 0 {
			flush(last)
		}
	}
	return out
}
