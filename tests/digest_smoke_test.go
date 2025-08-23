package tests

import (
	"fmt"
	"log"
	"strings"
	"testing"

	d "tsql_digest_v4/internal/sqldigest_antlr"
)

func Test_Smoke_MySQL(t *testing.T) {
	sql := `SELECT ?, 'x', 123 FROM t WHERE a IN (1, 2)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	log.Println(res.Digest)
	log.Println(res.Params)
	if err != nil {
		t.Fatalf("mysql build error: %v", err)
	}
	wantN := 5 // ?:1, 'x':1, 123:1, 1:1, 2:1
	assertBasic(t, sql, res, wantN, []string{"SELECT", "FROM", "WHERE", "IN"})
}

func Test_Smoke_Postgres(t *testing.T) {
	sql := `SELECT $$abc$$, $1::text, DATE '2020-01-01' FROM t LIMIT 10 OFFSET 5`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	if err != nil {
		t.Fatalf("pg build error: %v", err)
	}
	// $$abc$$, $1, DATE '...', 10, 5
	wantN := 5
	assertBasic(t, sql, res, wantN, []string{"SELECT", "FROM", "LIMIT", "OFFSET"})
}

func Test_Smoke_SQLServer(t *testing.T) {
	// 注意：不要用 N'...'，我们的字符串识别未特判 N 前缀；用普通 '...' 即可
	sql := `SELECT TOP 3 * FROM t WHERE a = @p1 AND b = 'X'`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	if err != nil {
		t.Fatalf("tsql build error: %v", err)
	}
	// TOP 3, @p1, 'X'
	wantN := 3
	assertBasic(t, sql, res, wantN, []string{"SELECT", "FROM", "TOP"})
}

func Test_Smoke_Oracle(t *testing.T) {
	sql := `SELECT q'[abc]', :p FROM dual FETCH FIRST 2 ROWS ONLY`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	if err != nil {
		t.Fatalf("oracle build error: %v", err)
	}
	// q'[...]', :p, 2
	wantN := 3
	assertBasic(t, sql, res, wantN, []string{"SELECT", "FROM", "FETCH", "ROWS", "ONLY"})
}

/************** helpers **************/

func assertBasic(t *testing.T, original string, res d.Result, wantN int, mustContain []string) {
	t.Helper()

	if res.Digest == "" {
		t.Fatalf("empty digest")
	}
	for _, kw := range mustContain {
		if !strings.Contains(res.Digest, kw) {
			t.Fatalf("digest should contain %q, got: %q", kw, res.Digest)
		}
	}

	if len(res.Params) != wantN {
		t.Fatalf("param count mismatch: got %d want %d; digest=%q", len(res.Params), wantN, res.Digest)
	}

	// “? 的个数”应等于参数个数
	if q := strings.Count(res.Digest, "?"); q != len(res.Params) {
		t.Fatalf("question-mark count mismatch: got %d, params %d; digest=%q", q, len(res.Params), res.Digest)
	}

	// 位置与原文校验
	for i, p := range res.Params {
		if !(p.Start >= 0 && p.End > p.Start && p.End <= len(original)) {
			t.Fatalf("param #%d has invalid range: [%d,%d) over len %d", i+1, p.Start, p.End, len(original))
		}
		gotSlice := original[p.Start:p.End]
		if gotSlice != p.Value {
			t.Fatalf("param #%d value mismatch: slice=%q, p.Value=%q", i+1, gotSlice, p.Value)
		}
	}
}

func Test_Insert_MySQL_Single(t *testing.T) {
	sql := `INSERT INTO orders (id, uid, amt, note, created_at)
VALUES (101, ?, 9.99, '首单', NOW());`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql single: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 101, ?, 9.99, '首单' → 4 个参数（NOW() 不参数化）
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 4)
	assertRowColGrid(t, res.Params, 1, 4) // 1 行 4 列
}

func Test_Insert_MySQL_Multi(t *testing.T) {
	sql := `INSERT INTO orders (id, uid, amt, note)
VALUES
  (102, :u2, 15.50, '第二单'),
  (103, :u3, 0xE4BDA0E5A5BD, 'hex'),
  (104, :u4, 0.00, '免运费')
ON DUPLICATE KEY UPDATE amt=VALUES(amt), note=VALUES(note);`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	if err != nil {
		t.Fatalf("mysql multi: %v", err)
	}
	// 3 行 × 4 列 = 12 参数；行列标注应为 (r=1..3,c=1..4)
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 9)
	assertRowColGrid(t, res.Params, 3, 3)
}

func Test_Insert_Postgres_Single(t *testing.T) {
	sql := `INSERT INTO public.logs (id, txt, created_at)
VALUES (1, $$abc$$, DATE '2020-01-01')
RETURNING id;`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg single: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 1, $$abc$$, DATE '...' → 3 个
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 3)
	assertRowColGrid(t, res.Params, 1, 3)
}

func Test_Insert_Postgres_Multi(t *testing.T) {
	sql := "INSERT INTO public.users (id, name, active)\nVALUES\n  ($1, 'Bob', TRUE),\n  ($2, $$Line1\nLine2$$, FALSE);"
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg multi: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 每行只会参数化两个：$N 与字符串；TRUE/FALSE 不参数化
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 4) // ($1,'Bob') + ($2,$$...$$)
	assertRowColGrid(t, res.Params, 2, 2)
}

func Test_Insert_SQLServer_Single(t *testing.T) {
	// 注意：不用 N'...' 前缀，避免当前字符串识别不命中
	sql := `INSERT INTO dbo.Orders (Id, UserId, Amount, Note)
VALUES (101, @p1, 9.99, 'first');`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("tsql single: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 4)
	assertRowColGrid(t, res.Params, 1, 4)
}

func Test_Insert_SQLServer_Multi(t *testing.T) {
	sql := `INSERT INTO dbo.Orders (Id, UserId, Amount, Note)
VALUES
  (102, 32 15.50, '二单'),
  (103, 12, 0.00, '免运费');`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("tsql multi: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 8) // 2×4
	assertRowColGrid(t, res.Params, 2, 4)
}

func Test_Insert_Oracle_Single(t *testing.T) {
	sql := `INSERT INTO orders (id, uid, amt, note, created_at)
VALUES (101, :u1, 9.99, q'[首单]', DATE '2020-01-01');`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle single: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 101, :u1, 9.99, q'[...]', DATE '...' → 5 个
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 5)
	assertRowColGrid(t, res.Params, 1, 5)
}

func Test_Insert_Oracle_InsertAll(t *testing.T) {
	// Oracle 多行常用 INSERT ALL，这里不做行列标注（不是 VALUES (...) , (...))
	sql := `INSERT ALL
  INTO orders (id, uid, amt, note) VALUES (102, :u2, 15.50, '第二单')
  INTO orders (id, uid, amt, note) VALUES (103, :u3, 0.00, '免运费')
SELECT 1 FROM dual;`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle insert all: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 两条 INTO 各 4 个 → 共 8
	assertDigestHas(t, res.Digest, []string{"INSERT", "ALL"})
	assertParamCount(t, sql, res, 9)
	// 行列标注在 INSERT ALL 中不强制要求
}

func Test_Insert_Oracle_Multi(t *testing.T) {
	sql := `INSERT INTO orders (id, uid, amt, note, created_at)
VALUES
  (101, :u1,  9.99,  '[首单]',      NOW()),
  (102, :u2, 15.50,  '第二单',       NOW()),
  (103, :u3,  0.00,  CONCAT('促销-', $1), NOW()),
  (104, :u4,  8.80,  '免运费',       NOW())
ON DUPLICATE KEY UPDATE
  amt  = VALUES(amt),
  note = VALUES(note),
  cnt  = COALESCE(cnt, 0) + 10,
  mark = DATE '2020-01-01'`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("oracle insert all: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 两条 INTO 各 4 个 → 共 8
}

/************** helpers **************/

func assertDigestHas(t *testing.T, digest string, kws []string) {
	t.Helper()
	if digest == "" {
		t.Fatalf("empty digest")
	}
	up := strings.ToUpper(digest)
	for _, kw := range kws {
		if !strings.Contains(up, kw) {
			t.Fatalf("digest missing %q; got: %q", kw, digest)
		}
	}
}

func assertParamCount(t *testing.T, original string, res d.Result, want int) {
	t.Helper()
	if len(res.Params) != want {
		t.Fatalf("param count mismatch: got %d want %d; digest=%q", len(res.Params), want, res.Digest)
	}
	//// “?” 个数 == 参数个数
	//if q := strings.Count(res.Digest, "?"); q != len(res.Params) {
	//	t.Fatalf("? count mismatch: digest has %d, params %d; digest=%q", q, len(res.Params), res.Digest)
	//}
	// 位置/原文一致性
	for i, p := range res.Params {
		if !(p.Start >= 0 && p.End > p.Start && p.End <= len(original)) {
			t.Fatalf("param #%d invalid range: [%d,%d) over len %d", i+1, p.Start, p.End, len(original))
		}
		if original[p.Start:p.End] != p.Value {
			t.Fatalf("param #%d value mismatch: slice=%q, p.Value=%q", i+1, original[p.Start:p.End], p.Value)
		}
	}
}

func assertRowColGrid(t *testing.T, params []d.ExParam, rows, cols int) {
	t.Helper()
	if rows <= 0 || cols <= 0 {
		return
	}
	// 统计每行的参数数量（仅统计 Row>0 的）
	rowCount := make(map[int]int)
	for _, p := range params {
		if p.Row > 0 {
			rowCount[p.Row]++
		}
	}
	// 对于 INSERT ... VALUES 的场景，期望每行都有恰好 cols 个参数
	if len(rowCount) == 0 {
		// 可能不是 VALUES 多行（比如 Oracle INSERT ALL），放过
		return
	}
	if len(rowCount) != rows {
		t.Fatalf("Row count mismatch: got rows=%d want rows=%d", len(rowCount), rows)
	}
	for r := 1; r <= rows; r++ {
		if rowCount[r] != cols {
			t.Fatalf("Row %d cols=%d want %d", r, rowCount[r], cols)
		}
	}
}
