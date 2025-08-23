// internal/parsers/myql/gen.go
package mysql

// 把 java -jar 起别名；go:generate 的工作目录就是本包目录
//go:generate -command antlr java -jar ../../../tools/antlr-4.13.0-complete.jar

// 注意：-o . 必须放在 .g4 文件参数“之前”，并加 -Xexact-output-dir 确保输出到本目录
// 如果你的语法文件在 third_party/grammars-v4/...，改成对应路径即可。
//
//go:generate antlr -Dlanguage=Go -visitor -listener -o . -Xexact-output-dir -package mysql ../../../grammars-v4/sql/mysql/Oracle/MySQLLexer.g4 ../../../grammars-v4/sql/mysql/Oracle/MySQLParser.g4

//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_lexer.go  -find this. -repl l.
// 构造函数及 ATN 模拟器（lexer）

//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_lexer.go  -find "this := new(MySqlLexer)"  -repl "l := new(MySqlLexer)"
//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_lexer.go  -find "return this"               -repl "return l"
//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_lexer.go  -find "LexerATNSimulator(this,"   -repl "LexerATNSimulator(l,"
//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_lexer.go  -find "func (p *MySQLLexer)"   -repl "func (l *MySQLLexer)"

// --- Parser: 用 p. ---
//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_parser.go -find this. -repl p.
//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_parser.go -find self. -repl p.
// 构造函数及 ATN 模拟器（parser）
//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_parser.go -find "this := new(MySQLParser)"   -repl "p := new(MySQLParser)"
//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_parser.go -find "return this"                 -repl "return p"
//go:generate go run ../../../tools/patchantlr/main.go -file ./mysql_parser.go -find "ParserATNSimulator(this,"    -repl "ParserATNSimulator(p,"
