# sqlglot-go

**受 Python 版 sqlglot 启发的 Go 语言 SQL 规范化与参数提取库。**  
在 **MySQL / Postgres / SQL Server / Oracle** 多方言下，将 SQL 归一成稳定的 **Digest**，并抽取 **参数**（含原 SQL 的字节偏移）。

- **稳定 Digest**：用于查询聚合、缓存 key、隐私友好型日志/指标
- **可靠参数抽取**：数字/字符串/绑定/时间字面量 → 参数；给出 `[Start, End)` 字节区间
- **方言感知**：`$1` / `:name` / `@var`，PG dollar-quoted，Oracle `q'[]'` 等
- **多行 INSERT 折叠**（可选）
- **时间函数参数化**（可选）：`NOW()`、`SYSDATE`、`CURRENT_DATE`…
- **用户无需生成代码**：词法/语法已提交在仓库内

---

## 安装

```bash
go get github.com/tensafe/sqlglot-go@latest
```

导入公开包：

```go
import "github.com/tensafe/sqlglot-go/sqlglot"
```

---

## 快速上手

```go
import (
	"fmt"
	"github.com/tensafe/sqlglot-go/sqlglot"
)

func main() {
	sql := `INSERT INTO t(a, ts) VALUES (1, NOW()), (2, NOW());`
	dig, params,sqltypes, err := sqlglot.Signature(sql, sqlglot.Options{
		Dialect:                sqlglot.MySQL,
		CollapseValuesInDigest: true,  // digest collapses multi-row VALUES to one tuple
		ParamizeTimeFuncs:      true,  // treat NOW()/CURRENT_DATE… as parameters
	})
	if err != nil { panic(err) }

	fmt.Println("Digest:", dig)
	for _, p := range params {
		fmt.Printf("P#%d %-10s [%d,%d): %q\n", p.Index, p.Type, p.Start, p.End, p.Value)
	}
  fmt.Println("SqlTypes:", sqltypes)
}
```

示例输出：

```
Digest: INSERT INTO T(A, TS) VALUES(?, ?);
P#1 Number     [29,30): "1"
P#2 Timestamp  [32,37): "NOW()"
P#3 Number     [41,42): "2"
P#4 Timestamp  [44,49): "NOW()"
SqlTypes: [INSERT]
```

---

## API 速览

```go
// 顶层帮助函数：
func Signature(sql string, opt Options) (digest string, params []ExParam,sqltypes []string, err error)
func ExtractParams(sql string, opt Options) ([]ExParam, error)
func ResultFor(sql string, opt Options) (Result, error)

// 方言：
const (
  MySQL Dialect = iota
  Postgres
  SQLServer
  Oracle
)

type Options struct {
  Dialect                Dialect // 必填
  CollapseValuesInDigest bool    // 折叠 INSERT ... VALUES (...),(...),... 到 digest 中的一组 (...)
  ParamizeTimeFuncs      bool    // 将 NOW/SYSDATE/CURRENT_DATE... 参数化（安全零参/精度变体）
}

type Result struct {
  Digest string
  Params []ExParam // {Index, Type, Value, Start, End}，Start/End 为原 SQL 的字节偏移
  SQLType []string
}

// 与 python/sqlglot 对齐的占位符（目前返回 ErrNotImplemented）：
// Parse, ParseOne, Transpile
```

---

## 行为与方言要点

**归一化**
- 关键字大写，空白折叠；修复 `IN (` 的空格
- 数字/字符串 → `?`（包含 hex/bin、PG dollar-quoted、Oracle `q'[]'`）
- 绑定占位符保留：`?`、`$1..$n`、`:name`、`@name`
- `DATE|TIME|TIMESTAMP '...'` → 合并为一个参数
- **时间函数**（可选）：`NOW()`、`CURRENT_DATE`、`SYSDATE`、`SYSUTCDATETIME()`、`CURRENT_TIMESTAMP(3)`…
- **多行 INSERT 折叠**（可选且安全时）：digest 仅保留首个 `(...)`，参数仍全部抽取
- 去除注释（含 MySQL 版本注释 `/*!40101 ...*/`）
- 支持多语句 `;`

**方言亮点**
- **Postgres**：`$$...$$`、`$tag$...$tag$` → 单个字符串参数；`expr::TYPE` 紧贴
- **Oracle**：`q'[]' / () / {} / <>` 字符串；`DATE '...'`；JSON/XMLTABLE/MATCH_RECOGNIZE 记号化安全
- **SQL Server**：`AT TIME ZONE`、`OPENJSON`、命名 `@变量`
- **MySQL**：`JSON_TABLE`、`X'ABCD'`、`0xFF`、版本注释

---

## 集成范式

**A) Web/gRPC 中间件日志**
```go
dig, params, _ := sqlglot.Signature(sql, sqlglot.Options{Dialect: sqlglot.Postgres})
logger.Infow("db.query", "digest", dig, "n_params", len(params))
```

**B) 指标（按 digest 分桶）**
```go
labels := prometheus.Labels{"digest": dig, "db": "orders"}
dbQueryCounter.With(labels).Inc()
dbLatencyHist.With(labels).Observe(elapsed.Seconds())
```

**C) 脱敏日志**
```go
safe := dig // 已无字面量
// 只写入 safe 到日志/审计
```

**D) 预编译缓存 Key**
```go
key := fmt.Sprintf("%s|%s", dialectName(opt.Dialect), dig)
stmt := cache.GetOrPrepare(key, sql)
```

---

## 基准测试

仓库内已提供 `bench_test.go`，直接运行：

```bash
go test -run ^$ -bench . -benchmem ./...
go test -run ^$ -bench Signature -benchmem -benchtime=3s -count=3 ./...
```

建议：使用 `-count=5` 与固定 Go 版本，获得更稳定结果。

---

## 常见问题（开发与发布）

- **在本仓库开发**：不要在该模块内 `go get` 自己；直接 `go build`/`go test` 即可。
- **工作区（go.work）开发**：
  ```bash
  go work init .
  # 若已在 use . 中，不要再对同一模块写 replace => .
  ```
- **在外部项目验证本地未发布改动**：在“消费方”的 `go.mod`：
  ```go
  replace github.com/tensafe/sqlglot-go => /path/to/sqlglot-go
  ```
- **发布**：确保 `go.mod` 无 replace，`go mod tidy`，打 tag `vX.Y.Z`。

---

## 参与贡献

```bash
git clone https://github.com/tensafe/sqlglot-go
cd sqlglot-go
go mod tidy
go test ./...
```

欢迎提交覆盖方言边界场景的失败用例与修复。

---

## 许可证

MIT
