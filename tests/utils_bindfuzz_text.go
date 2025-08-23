package tests

import (
	"fmt"
	d "github.com/tensafe/sqlglot-go/internal/sqldigest_antlr"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

/*
   BindifySQL: 在 SQL 文本中随机把“数字/字符串字面量”替换为方言占位符（受 ratio 控制）。
   - 避开字符串与注释（支持 '...'、$$...$$、$tag$...$tag$、Oracle q'[]' 族、--
   - 不进入引号内部，不破坏原有空白/注释/标识符 - 会读取已有的绑定编号（$N、@pN、:pN），从最大值+1继续编号（MySQL 仍用 ?）
*/

func BindifySQL(sql string, dialect d.Dialect, ratio float64, r *rand.Rand) string {
	if ratio <= 0 {
		return sql
	}
	nextIdx := maxExistingBindIndex(sql, dialect) + 1

	var out strings.Builder
	out.Grow(len(sql))

	type mode int
	const (
		mBase         mode = iota
		mSQuote            // '...'
		mDQuote            // "..."（默认当作标识符/字符串，避免进入）
		mLineComment       // -- ... \n
		mBlockComment      // /* ... */
		mDollarQuote       // $$...$$ or $tag$...$tag$
		mOracleQ           // q'X...X'，X 可为 (),[],<>,{} 四种括号，或任意单字符（保守选四种）
	)

	i := 0
	n := len(sql)
	//var dqTag string   // $tag$ 的 tag（含$）
	//var oqOpen, oqClose rune // Oracle q'X...X' 的左右括号

	emit := func(b, e int) { out.WriteString(sql[b:e]) }

	// 简单数字/字符串识别（必须在 mBase 下）
	isDigit := func(b byte) bool { return b >= '0' && b <= '9' }
	// 提取数字（含小数/科学计数/前导- 但避免把标识符的一部分吃掉）
	grabNumber := func(pos int) (end int, ok bool) {
		i := pos
		// 可选一元负号，要求后面跟数字
		if i < n && (sql[i] == '-' || sql[i] == '+') {
			if i+1 < n && isDigit(sql[i+1]) {
				i++
			} else {
				return pos, false
			}
		}
		if i < n && isDigit(sql[i]) {
			i++
			// 十进制部分
			for i < n && (sql[i] >= '0' && sql[i] <= '9') {
				i++
			}
			// 小数
			if i < n && sql[i] == '.' {
				j := i + 1
				for j < n && (sql[j] >= '0' && sql[j] <= '9') {
					j++
				}
				// 至少要有一位小数
				if j > i+1 {
					i = j
				}
			}
			// 科学计数 e/E(+/-)N
			if i < n && (sql[i] == 'e' || sql[i] == 'E') {
				j := i + 1
				if j < n && (sql[j] == '+' || sql[j] == '-') {
					j++
				}
				k := j
				for k < n && isDigit(sql[k]) {
					k++
				}
				if k > j {
					i = k
				}
			}
			return i, true
		}
		// 0xABC 十六进制
		if i+2 <= n && (strings.HasPrefix(strings.ToLower(sql[i:min(i+2, n)]), "0x")) {
			j := i + 2
			for j < n {
				c := sql[j]
				if (c >= '0' && c <= '9') || (c|32 >= 'a' && c|32 <= 'f') {
					j++
					continue
				}
				break
			}
			if j > i+2 {
				return j, true
			}
		}
		return pos, false
	}
	// 提取单引号字符串：支持 '' 转义
	grabSQuote := func(pos int) int {
		i := pos + 1
		for i < n {
			if sql[i] == '\'' {
				if i+1 < n && sql[i+1] == '\'' { // ''
					i += 2
					continue
				}
				return i + 1
			}
			i++
		}
		return n
	}
	// 双引号：直接跳过到下一个 "（不处理转义，保守看待）
	grabDQuote := func(pos int) int {
		i := pos + 1
		for i < n {
			if sql[i] == '"' {
				return i + 1
			}
			i++
		}
		return n
	}
	// -- 注释到行尾
	grabLineComment := func(pos int) int {
		i := pos + 2
		for i < n {
			if sql[i] == '\n' {
				return i + 1
			}
			i++
		}
		return n
	}
	// /* ... */ 块注释
	grabBlockComment := func(pos int) int {
		i := pos + 2
		for i+1 < n {
			if sql[i] == '*' && sql[i+1] == '/' {
				return i + 2
			}
			i++
		}
		return n
	}
	// $tag$...$tag$（含 $$...$$）
	isDollarStart := func(pos int) (end int, tag string, ok bool) {
		if pos >= n || sql[pos] != '$' {
			return pos, "", false
		}
		j := pos + 1
		for j < n && ((sql[j] >= 'A' && sql[j] <= 'Z') || (sql[j] >= 'a' && sql[j] <= 'z') || (sql[j] >= '0' && sql[j] <= '9') || sql[j] == '_') {
			j++
		}
		if j < n && sql[j] == '$' {
			return j + 1, sql[pos : j+1], true // tag 形如 $xxx$
		}
		return pos, "", false
	}
	grabUntilDollarTag := func(pos int, tag string) int {
		// 寻找下一个精确的 tag
		idx := strings.Index(sql[pos:], tag)
		if idx < 0 {
			return n
		}
		return pos + idx + len(tag)
	}
	// Oracle q'X...X'
	isOracleQStart := func(pos int) (end int, open, close rune, ok bool) {
		if pos+2 >= n || (sql[pos] != 'q' && sql[pos] != 'Q') || sql[pos+1] != '\'' {
			return pos, 0, 0, false
		}
		// 取分隔符
		i2 := pos + 2
		r, w := utf8.DecodeRuneInString(sql[i2:])
		if r == utf8.RuneError {
			return pos, 0, 0, false
		}
		var o, c rune
		switch r {
		case '[':
			o, c = '[', ']'
		case '(':
			o, c = '(', ')'
		case '{':
			o, c = '{', '}'
		case '<':
			o, c = '<', '>'
		default:
			// 也可能是任意单字符，这里保守不处理，返回 false
			return pos, 0, 0, false
		}
		return i2 + w, o, c, true
	}
	grabOracleQ := func(pos int, open, close rune) int {
		// pos 指向内容起始处；需找到 close 后的单引号
		i := pos
		depth := 0
		for i < n {
			r, w := utf8.DecodeRuneInString(sql[i:])
			if r == open {
				depth++
				i += w
				continue
			}
			if r == close {
				depth--
				i += w
				if depth <= 0 {
					// 期望后面紧跟 '
					if i < n && sql[i] == '\'' {
						return i + 1
					}
					return i
				}
				continue
			}
			i += w
		}
		return n
	}

	// 现有绑定最大编号（为 @pN / :pN / $N）
	//maxExistingBindIndex := func(sql string, dialect d.Dialect) int { return maxExistingBindIndex(sql, dialect) } // 仅为了文意

	for i < n {
		switch {
		case i+1 < n && sql[i] == '-' && sql[i+1] == '-':
			// -- 行注释
			j := grabLineComment(i)
			emit(i, j)
			i = j
			continue
		case i+1 < n && sql[i] == '/' && sql[i+1] == '*':
			// 块注释
			j := grabBlockComment(i)
			emit(i, j)
			i = j
			continue
		case sql[i] == '\'':
			// '...'
			j := grabSQuote(i)
			// 在 mBase 下允许把整个 '...' 换成占位符
			if r.Float64() < ratio {
				out.WriteString(makeBind(dialect, &nextIdx))
			} else {
				emit(i, j)
			}
			i = j
			continue
		case sql[i] == '"':
			// 双引号（标识符）——不进入，直接跳过
			j := grabDQuote(i)
			emit(i, j)
			i = j
			continue
		default:
			// $$ 或 $tag$
			if end, tag, ok := isDollarStart(i); ok {
				// 当前是 $tag$ 开始
				closePos := grabUntilDollarTag(end, tag)
				// 替换整个块为占位符/或保持
				if r.Float64() < ratio {
					out.WriteString(makeBind(dialect, &nextIdx))
				} else {
					emit(i, closePos)
				}
				i = closePos
				continue
			}
			// Oracle q'X...X'
			if end, o, c, ok := isOracleQStart(i); ok {
				j := grabOracleQ(end, o, c)
				if r.Float64() < ratio {
					out.WriteString(makeBind(dialect, &nextIdx))
				} else {
					emit(i, j)
				}
				i = j
				continue
			}
			// 数字
			if j, ok := grabNumber(i); ok {
				// 避免把标识符的一部分吃掉：前导若为字母/下划线，略过
				if i > 0 {
					ch := sql[i-1]
					if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch == '_' {
						emit(i, j)
						i = j
						continue
					}
				}
				if r.Float64() < ratio {
					out.WriteString(makeBind(dialect, &nextIdx))
				} else {
					emit(i, j)
				}
				i = j
				continue
			}
			// 其它字符：原样输出一个 rune
			_, w := utf8.DecodeRuneInString(sql[i:])
			emit(i, i+w)
			i += w
		}
	}
	return out.String()
}

// 依据方言生成下一个占位符；会就地递增 nextIdx（MySQL 用 ? 不递增编号，但会推进计数以便一致性）
func makeBind(dialect d.Dialect, nextIdx *int) string {
	switch dialect {
	case d.Postgres:
		b := fmt.Sprintf("$%d", *nextIdx)
		*nextIdx++
		return b
	case d.SQLServer:
		b := fmt.Sprintf("@p%d", *nextIdx)
		*nextIdx++
		return b
	case d.Oracle:
		b := fmt.Sprintf(":p%d", *nextIdx)
		*nextIdx++
		return b
	default: // MySQL
		*nextIdx++
		return "?"
	}
}

// 扫描已有绑定编号的最大值
func maxExistingBindIndex(sql string, dialect d.Dialect) int {
	var re *regexp.Regexp
	switch dialect {
	case d.Postgres:
		re = regexp.MustCompile(`\$(\d+)`)
	case d.SQLServer:
		re = regexp.MustCompile(`@p?(\d+)`)
	case d.Oracle:
		re = regexp.MustCompile(`:p?(\d+)`)
	default:
		return 0
	}
	maxN := 0
	for _, m := range re.FindAllStringSubmatch(sql, -1) {
		if len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > maxN {
				maxN = n
			}
		}
	}
	return maxN
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
