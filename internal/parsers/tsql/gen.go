// internal/parsers/postgresql/gen.go
package tsql

// 把 java -jar 起别名；go:generate 的工作目录就是本包目录
//go:generate -command antlr java -jar ../../../tools/antlr-4.13.0-complete.jar

// 注意：-o . 必须放在 .g4 文件参数“之前”，并加 -Xexact-output-dir 确保输出到本目录
// 如果你的语法文件在 third_party/grammars-v4/...，改成对应路径即可。
//
//go:generate antlr -Dlanguage=Go -visitor -listener -o . -Xexact-output-dir -package tsql ../../../grammars-v4/sql/tsql/TSqlLexer.g4 ../../../grammars-v4/sql/tsql/TSqlParser.g4
