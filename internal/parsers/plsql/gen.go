package plsql

//go:generate -command antlr java -jar ../../../tools/antlr-4.13.0-complete.jar

// 生成到当前目录
//go:generate antlr -Dlanguage=Go -visitor -listener -o . -Xexact-output-dir -package plsql ../../../grammars-v4/sql/plsql/PlSqlLexer.g4 ../../../grammars-v4/sql/plsql/PlSqlParser.g4

// 先把 Java/C# 风格的接收者前缀替换为 Go 的
//go:generate go run ../../../tools/patchantlr/main.go -file ./plsql_lexer.go  -find this. -repl p.
//go:generate go run ../../../tools/patchantlr/main.go -file ./plsql_parser.go -find this. -repl p.
//go:generate go run ../../../tools/patchantlr/main.go -file ./plsql_parser.go -find self. -repl p.

// 处理“裸 this”——构造函数与 ATN 模拟器入参里的 this
// 1) parser 构造函数/返回值/ATNSimulator
//go:generate go run ../../../tools/patchantlr/main.go -file ./plsql_parser.go -find "this := new(PlSqlParser)"         -repl "p := new(PlSqlParser)"
//go:generate go run ../../../tools/patchantlr/main.go -file ./plsql_parser.go -find "return this"                      -repl "return p"
//go:generate go run ../../../tools/patchantlr/main.go -file ./plsql_parser.go -find "ParserATNSimulator(this,"         -repl "ParserATNSimulator(p,"
// （有些模板会是 NewParserATNSimulator(this,...）；上面这条已覆盖 "ParserATNSimulator(this," 子串）
//
// 2) lexer 构造函数/返回值/ATNSimulator
//go:generate go run ../../../tools/patchantlr/main.go -file ./plsql_lexer.go  -find "this := new(PlSqlLexer)"          -repl "l := new(PlSqlLexer)"
//go:generate go run ../../../tools/patchantlr/main.go -file ./plsql_lexer.go  -find "return this"                       -repl "return l"
//go:generate go run ../../../tools/patchantlr/main.go -file ./plsql_lexer.go  -find "LexerATNSimulator(this,"          -repl "LexerATNSimulator(l,"
