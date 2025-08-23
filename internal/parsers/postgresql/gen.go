// internal/parsers/postgresql/gen.go
package postgresql

// 把 java -jar 起别名；go:generate 的工作目录就是本包目录
//go:generate -command antlr java -jar ../../../tools/antlr-4.13.0-complete.jar

// 注意：-o . 必须放在 .g4 文件参数“之前”，并加 -Xexact-output-dir 确保输出到本目录
// 如果你的语法文件在 third_party/grammars-v4/...，改成对应路径即可。
//
//go:generate antlr -Dlanguage=Go -visitor -listener -o . -Xexact-output-dir -package postgresql ../../../grammars-v4/sql/postgresql/PostgreSQLLexer.g4 ../../../grammars-v4/sql/postgresql/PostgreSQLParser.g4

// 先把 Java/C# 风格的接收者前缀替换为 Go 的
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_lexer.go  -find this. -repl p.
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_parser.go -find this. -repl p.
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_parser.go -find self. -repl p.

// 处理“裸 this”——构造函数与 ATN 模拟器入参里的 this
// 1) parser 构造函数/返回值/ATNSimulator
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_parser.go -find "this := new(PostgreSQLParser)"         -repl "p := new(PostgreSQLParser)"
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_parser.go -find "return this"                      -repl "return p"
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_parser.go -find "ParserATNSimulator(this,"         -repl "ParserATNSimulator(p,"
// （有些模板会是 NewParserATNSimulator(this,...）；上面这条已覆盖 "ParserATNSimulator(this," 子串）
//
// 2) lexer 构造函数/返回值/ATNSimulator
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_lexer.go  -find "this := new(PostgreSQLParser)"          -repl "l := new(PostgreSQLParser)"
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_lexer.go  -find "return this"                       -repl "return l"
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_lexer.go  -find "LexerATNSimulator(this,"          -repl "LexerATNSimulator(l,"
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_lexer.go  -find "p.HandleLessLessGreaterGreater()"          -repl "l.HandleLessLessGreaterGreater()"
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_lexer.go  -find "p.PushTag()"          -repl "l.PushTag()"
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_lexer.go  -find "p.HandleNumericFail()"          -repl "l.HandleNumericFail()"
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_lexer.go  -find "p.PopTag()"          -repl "l.PopTag()"
//go:generate go run ../../../tools/patchantlr/main.go -file ./postgresql_lexer.go  -find "p.UnterminatedBlockCommentDebugAssert()"          -repl "l.UnterminatedBlockCommentDebugAssert()"
