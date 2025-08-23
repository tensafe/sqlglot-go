package sqldigest_antlr

// 仅基于字符判断空白（各方言 lexer 的空白通常在 HiddenChannel，这里是兜底）
func IsWhitespace(txt string) bool {
	for i := 0; i < len(txt); i++ {
		switch txt[i] {
		case ' ', '\t', '\n', '\r':
			// ok
		default:
			return false
		}
	}
	return len(txt) > 0
}
