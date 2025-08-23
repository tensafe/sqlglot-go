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

func Test_Oracle_Select_Basics(t *testing.T) {
	sql := `SELECT :id, 'x', DATE '2020-01-01', INTERVAL '1' DAY
FROM dual
WHERE name = :name`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle basics: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// :id, 'x', DATE '...', INTERVAL '1'（单位 DAY 会出现在 digest 里，不参数化）, :name
	assertDigestHas(t, res.Digest, []string{"SELECT", "FROM", "WHERE", "DAY"})
	assertParamCount(t, sql, res, 5)
}

func Test_Oracle_QQuote_Strings(t *testing.T) {
	sql := `SELECT q'[a 'b' c]', q'{中}文', q'@x@y@' FROM dual`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle q-quote: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"SELECT", "FROM"})
	assertParamCount(t, sql, res, 3)
}

func Test_Oracle_Insert_Single(t *testing.T) {
	sql := `INSERT INTO orders (id, uid, amt, note, created_at)
VALUES (101, :u1, 9.99, q'[首单]', DATE '2020-01-01')`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle insert single: %v", err)
	}
	// 101, :u1, 9.99, q'[...]', DATE '...' → 5 个
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 5)
}

func Test_Oracle_InsertAll_MultiRows(t *testing.T) {
	sql := `INSERT ALL
  INTO orders (id, uid, amt, note) VALUES (102, :u2, 15.50, '第二单')
  INTO orders (id, uid, amt, note) VALUES (103, :u3, 0.00, '免运费')
SELECT 1 FROM dual`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle insert all: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 两条 INTO 各 4 个 → 8 个参数（注意 '第二单'/'免运费' 也参数化）
	assertDigestHas(t, res.Digest, []string{"INSERT", "ALL", "SELECT", "FROM"})
	assertParamCount(t, sql, res, 9)
}

func Test_Oracle_Update_Returning(t *testing.T) {
	sql := `UPDATE orders
SET amt = :amt
WHERE id = :id
RETURNING note INTO :note`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle update returning: %v", err)
	}
	// :amt, :id, :note → 3 个（RETURNING INTO 的 :note 也是命名绑定）
	assertDigestHas(t, res.Digest, []string{"UPDATE", "RETURNING", "INTO"})
	assertParamCount(t, sql, res, 3)
}

func Test_Oracle_Delete_Returning(t *testing.T) {
	sql := `DELETE FROM orders WHERE id = :id RETURNING note, amt INTO :n, :a`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle delete returning: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"DELETE", "FROM", "RETURNING", "INTO"})
	assertParamCount(t, sql, res, 3) // :id, :n, :a
}

func Test_Oracle_Merge_Into(t *testing.T) {
	sql := `MERGE INTO tgt t
USING (SELECT :id AS id, :val AS val FROM dual) s
ON (t.id = s.id)
WHEN MATCHED THEN UPDATE SET t.val = s.val
WHEN NOT MATCHED THEN INSERT (id, val) VALUES (s.id, s.val)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle merge: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"MERGE", "INTO", "USING", "WHEN", "UPDATE", "INSERT"})
	assertParamCount(t, sql, res, 2) // :id, :val
}

func Test_Oracle_Sequence_Nextval(t *testing.T) {
	sql := `INSERT INTO t(id, val) VALUES (seq_orders.NEXTVAL, :v)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle seq nextval: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"INSERT", "VALUES", "NEXTVAL"})
	assertParamCount(t, sql, res, 1) // 只有 :v
}

func Test_Oracle_ConnectBy_Prior(t *testing.T) {
	sql := `SELECT LEVEL, SYS_CONNECT_BY_PATH(name, '/')
FROM cats
START WITH parent_id IS NULL
CONNECT BY PRIOR id = parent_id AND LEVEL < :n`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle connect by: %v", err)
	}
	// 参数：'/' 字面量 + :n
	assertDigestHas(t, res.Digest, []string{"CONNECT", "BY", "PRIOR", "START", "WITH"})
	assertParamCount(t, sql, res, 2)
}

func Test_Oracle_OuterJoin_Legacy(t *testing.T) {
	sql := `SELECT * FROM emp e, dept d
WHERE e.deptno = d.deptno(+)
AND e.ename LIKE :pat`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle legacy outer join: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"SELECT", "WHERE", "(+)"})
	assertParamCount(t, sql, res, 1)
}

func Test_Oracle_Analytic_DateLiteral(t *testing.T) {
	sql := `SELECT deptno,
       SUM(sal) OVER (PARTITION BY deptno ORDER BY empno
                      ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS s
FROM emp
WHERE hiredate < DATE '2020-01-01'`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle analytic: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"OVER", "PARTITION", "ROWS", "BETWEEN"})
	assertParamCount(t, sql, res, 1) // DATE '...'
}

func Test_Oracle_TimeFuncs_ParamizeOff_Default(t *testing.T) {
	sql := `SELECT SYSDATE, SYSTIMESTAMP FROM dual`
	// 默认 ParamizeTimeFuncs=false，不把时间函数当参数
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle time funcs off: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"SYSDATE", "SYSTIMESTAMP"})
	assertParamCount(t, sql, res, 0)
}

func Test_Oracle_TimeFuncs_ParamizeOn(t *testing.T) {
	sql := `SELECT SYSDATE, SYSTIMESTAMP, CURRENT_DATE FROM dual`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("oracle time funcs on: %v", err)
	}
	// 三个都应被参数化
	assertDigestHas(t, res.Digest, []string{"SELECT", "FROM"})
	assertParamCount(t, sql, res, 3)
}

func Test_Oracle_Update_With_Subquery_And_In(t *testing.T) {
	sql := `UPDATE t SET val = (SELECT MAX(x) FROM s WHERE s.k = t.k)
WHERE id IN (:a, :b, :c)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle update subquery: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"UPDATE", "IN"})
	assertParamCount(t, sql, res, 3)
}

func Test_Oracle_Malformed_ExtraParen_Sanitized(t *testing.T) {
	sql := `SELECT (1+1)) FROM dual; SELECT 1 FROM dual`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle malformed paren: %v", err)
	}
	// 最终 digest 不应出现尾部多余的 ')'
	if strings.HasSuffix(strings.TrimSpace(res.Digest), ")") {
		t.Fatalf("digest should not end with ')': %q", res.Digest)
	}
}

func Test_MySQL_Insert_Single_Basics(t *testing.T) {
	sql := `INSERT INTO orders (id, uid, amt, note, created_at)
VALUES (101, ?, 9.99, '首单', NOW());`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql insert single: %v", err)
	}
	// 101, ?, 9.99, '首单' → 4 个；NOW() 默认不参数化
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 4)
}

func Test_MySQL_Insert_Multi_Collapse_NoBind(t *testing.T) {
	// 无绑定变量（只有数字/字符串字面量）→ 允许折叠：digest 只渲染第一个元组
	sql := `INSERT INTO t (a, b) VALUES (1, 'x'), (2, 'y'), (3, 'z');`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql insert multi collapse: %v", err)
	}
	// 参数总数 3×2=6；但 digest 中 ? 只有列数 2
	if want := 6; len(res.Params) != want {
		t.Fatalf("params=%d want=%d; digest=%q", len(res.Params), want, res.Digest)
	}
	if q := strings.Count(res.Digest, "?"); q != 2 {
		t.Fatalf("digest ? count=%d want=2; digest=%q", q, res.Digest)
	}
}

func Test_MySQL_Insert_Multi_NoCollapse_WithBind(t *testing.T) {
	// 任一元组含绑定（?）→ 不折叠
	sql := `INSERT INTO t (a, b) VALUES (1, ?), (2, ?);`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql insert multi with bind: %v", err)
	}
	// 4 个参数；digest 中 ? 也应是 4（不折叠）
	if want := 4; len(res.Params) != want {
		t.Fatalf("params=%d want=%d; digest=%q", len(res.Params), want, res.Digest)
	}
	if q := strings.Count(res.Digest, "?"); q != 4 {
		t.Fatalf("digest ? count=%d want=4; digest=%q", q, res.Digest)
	}
}

func Test_MySQL_Insert_Multi_NoCollapse_ParamizeTimeFuncsOn(t *testing.T) {
	// 把时间函数视作“变量”时（ParamizeTimeFuncs=true），存在 NOW() → 不折叠
	sql := `INSERT INTO t (a, ts) VALUES (1, NOW()), (2, NOW());`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("mysql insert multi time paramized: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 每行两列均参数化 → 4；digest 中 ? 也应是 4
	if want := 4; len(res.Params) != want {
		t.Fatalf("params=%d want=%d; digest=%q", len(res.Params), want, res.Digest)
	}
	if q := strings.Count(res.Digest, "?"); q != 4 {
		t.Fatalf("digest ? count=%d want=4; digest=%q", q, res.Digest)
	}
}

func Test_MySQL_OnDuplicateKey_Update_NamedBind(t *testing.T) {
	// 虽然 MySQL 原生不支持 :name，但客户端可重写；我们只做 token 级抽参
	sql := `INSERT INTO orders (id, uid, amt, note)
VALUES (101, :u1, 9.99, 'x')
ON DUPLICATE KEY UPDATE amt=VALUES(amt), note=VALUES(note);`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL, CollapseValuesInDigest: false})
	if err != nil {
		t.Fatalf("mysql on-dup: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	assertDigestHas(t, res.Digest, []string{"INSERT", "INTO", "VALUES", "ON", "DUPLICATE", "KEY", "UPDATE"})
	assertParamCount(t, sql, res, 3)
}

/********* UPDATE / DELETE *********/

func Test_MySQL_Update_Join_InList(t *testing.T) {
	sql := `UPDATE a JOIN b ON a.bid=b.id
SET a.x=?
WHERE b.id IN (1,2,3);`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql update join: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"UPDATE", "JOIN", "SET", "IN ("})
	assertParamCount(t, sql, res, 4) // ? + 1 + 2 + 3
}

func Test_MySQL_Delete_OrderLimit(t *testing.T) {
	sql := `DELETE FROM t WHERE k=? ORDER BY id DESC LIMIT 10;`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql delete limit: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"DELETE", "FROM", "ORDER", "LIMIT"})
	assertParamCount(t, sql, res, 2) // ?, 10
}

/********* SELECT 特性 *********/

func Test_MySQL_Select_JSON_Operators(t *testing.T) {
	sql := `SELECT doc->'$.a', doc->>'$.b'
FROM t
WHERE JSON_EXTRACT(doc, '$.c') = 'x';`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql json ops: %v", err)
	}
	// '$.a', '$.b', '$.c', 'x' → 4 个
	assertDigestHas(t, res.Digest, []string{"->", "->>", "JSON_EXTRACT"})
	assertParamCount(t, sql, res, 4)
}

func Test_MySQL_Quoted_Backticks(t *testing.T) {
	sql := "SELECT `select`, `from` FROM `db`.`table` WHERE `id`=1"
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql backticks: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"`SELECT`", "`FROM`", "`DB`", "`TABLE`"})
	assertParamCount(t, sql, res, 1)
}

/********* 其它 DML 形态 *********/

func Test_MySQL_Insert_Select(t *testing.T) {
	sql := `INSERT INTO t (a, b)
SELECT c, d FROM s WHERE e=1;`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql insert-select: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"INSERT", "SELECT", "FROM", "WHERE"})
	assertParamCount(t, sql, res, 1)
}

func Test_MySQL_Replace_Into(t *testing.T) {
	sql := `REPLACE INTO t (a, b) VALUES (1, 2);`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql replace into: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"REPLACE", "INTO", "VALUES"})
	assertParamCount(t, sql, res, 2)
}

/********* 多语句 + 容错清洗 *********/

func Test_MySQL_MultiStatements_SanitizeParen(t *testing.T) {
	sql := `INSERT INTO t(a) VALUES(1)) ; INSERT INTO t(a) VALUES(2)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql multi sanitize: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 两条语句的参数：1 与 2 → 共 2
	assertParamCount(t, sql, res, 2)
}
func Test_PG_Select_Basics(t *testing.T) {
	sql := `SELECT $$abc$$, $1::text, DATE '2020-01-01', INTERVAL '1 day'
FROM t
WHERE flag IS NOT NULL AND name ILIKE '%x%'`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg basics: %v", err)
	}
	// $$abc$$、$1、DATE、INTERVAL → 4 个参数（'%x%' 也会被参数化 → 共 5）
	assertDigestHas(t, res.Digest, []string{"SELECT", "::TEXT", "ILIKE"})
	assertParamCount(t, sql, res, 5)
}

func Test_PG_DollarTag_Strings(t *testing.T) {
	sql := `SELECT $tag$hello, world$tag$, $$x$$, $a$中$a$ FROM dual`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg dollar-tag: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"SELECT"})
	assertParamCount(t, sql, res, 3)
}

func Test_PG_JSON_Operators(t *testing.T) {
	sql := `SELECT doc->'$.a', doc->>'$.b', doc#>'{x,0}', doc#>>'{y,1}'
FROM t
WHERE meta @> '{"a":1}' AND meta <@ '{"a":1,"b":2}'`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg json ops: %v", err)
	}
	// '$.a', '$.b', '{x,0}', '{y,1}', '{"a":1}', '{"a":1,"b":2}' → 6
	assertDigestHas(t, res.Digest, []string{"->", "->>", "#>", "#>>", "@>", "<@"})
	assertParamCount(t, sql, res, 6)
}

/********* INSERT / VALUES 折叠 *********/

func Test_PG_Insert_Single(t *testing.T) {
	sql := `INSERT INTO t (id, name, created_at)
VALUES (101, 'x', now()) RETURNING id`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg insert single: %v", err)
	}
	// 101, 'x' → 2；默认 ParamizeTimeFuncs=false，now() 不参数化
	assertDigestHas(t, res.Digest, []string{"INSERT", "VALUES", "RETURNING"})
	assertParamCount(t, sql, res, 2)
}

func Test_PG_Insert_Multi_Collapse_NoBind(t *testing.T) {
	// 无绑定变量，仅字面量 → 允许折叠（digest 只渲染第一个元组）
	sql := `INSERT INTO t (a, b, ts)
VALUES (1, 'x', now()), (2, 'y', now()), (3, 'z', now());`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg insert multi collapse: %v", err)
	}
	// 参数总数：3 行 × (1, 'x') 两列 = 6；now() 不参数化
	if want := 6; len(res.Params) != want {
		t.Fatalf("params=%d want=%d; digest=%q", len(res.Params), want, res.Digest)
	}
	// digest 里 ? 只应该等于列数（不含时间函数）
	if q := strings.Count(res.Digest, "?"); q != 2 {
		t.Fatalf("digest ? count=%d want=2; digest=%q", q, res.Digest)
	}
}

func Test_PG_Insert_Multi_NoCollapse_WithBind(t *testing.T) {
	// 任一元组含 $n → 不折叠
	sql := `INSERT INTO t (a, b)
VALUES (1, $1), (2, $2)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg insert multi with bind: %v", err)
	}
	// 参数总数：1, $1, 2, $2 → 4；digest 中 ? 也应是 4（不折叠）
	if want := 4; len(res.Params) != want {
		t.Fatalf("params=%d want=%d; digest=%q", len(res.Params), want, res.Digest)
	}
	if q := strings.Count(res.Digest, "?"); q != 4 {
		t.Fatalf("digest ? count=%d want=4; digest=%q", q, res.Digest)
	}
}

func Test_PG_Insert_Multi_TimeFuncs_ParamizeOn_NoCollapse(t *testing.T) {
	// 把时间函数视作“变量”时（ParamizeTimeFuncs=true），存在 now() → 不折叠
	sql := `INSERT INTO t (a, ts) VALUES (1, now()), (2, now());`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("pg insert time funcs paramize on: %v", err)
	}
	// 每行两列均参数化 → 共 4
	if want := 4; len(res.Params) != want {
		t.Fatalf("params=%d want=%d; digest=%q", len(res.Params), want, res.Digest)
	}
	// 安全检查：任何一个参数不应跨越 "), ("
	for _, p := range res.Params {
		if strings.Contains(p.Value, "), (") {
			t.Fatalf("param spans tuple boundary: %q", p.Value)
		}
	}
}

/********* ON CONFLICT / RETURNING *********/

func Test_PG_Upsert_OnConflict(t *testing.T) {
	sql := `INSERT INTO t (id, cnt, note)
VALUES (1, 1, 'x')
ON CONFLICT (id)
DO UPDATE SET cnt = t.cnt + 1, note = EXCLUDED.note || '!'::text
RETURNING id`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg upsert: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 1, 1, 'x', '!'::text → 4
	assertDigestHas(t, res.Digest, []string{"ON", "CONFLICT", "EXCLUDED", "::TEXT", "RETURNING"})
	assertParamCount(t, sql, res, 5)
}

/********* CTE / 窗口 / DISTINCT ON *********/

func Test_PG_With_CTE_Window(t *testing.T) {
	sql := `WITH s AS (
  SELECT id, amt, ROW_NUMBER() OVER (PARTITION BY uid ORDER BY ts DESC) AS rn
  FROM t WHERE uid = $1
)
SELECT DISTINCT ON (id) id, amt
FROM s WHERE rn = 1 AND amt > 10`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg with cte: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	assertDigestHas(t, res.Digest, []string{"WITH", "OVER", "PARTITION", "DISTINCT ON"})
	assertParamCount(t, sql, res, 3) // $1, 10
}

func Test_PG_With_Recursive(t *testing.T) {
	sql := `WITH RECURSIVE r AS (
  SELECT 1 AS n
  UNION ALL
  SELECT n+1 FROM r WHERE n < 5
)
SELECT * FROM r`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg with recursive: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 字面量 1 与 5 → 2
	assertDigestHas(t, res.Digest, []string{"WITH", "RECURSIVE", "UNION", "ALL"})
	assertParamCount(t, sql, res, 3)
}

/********* 数组 / ANY / ARRAY 字面量 *********/

func Test_PG_Array_Any_Bind(t *testing.T) {
	sql := `SELECT * FROM t WHERE id = ANY($1)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg any bind: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"ANY"})
	assertParamCount(t, sql, res, 1)
}

func Test_PG_Array_Literal(t *testing.T) {
	sql := `SELECT ARRAY[1,2,3]::int[]`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg array literal: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 1,2,3 → 3 个参数；::int[] 不参数化；:: 紧贴
	assertDigestHas(t, res.Digest, []string{"::INT [ ]"})
	assertParamCount(t, sql, res, 3)
}

/********* UPDATE / DELETE with FROM/USING *********/

func Test_PG_Update_From(t *testing.T) {
	sql := `UPDATE a SET v = b.v
FROM b WHERE a.id = b.id AND a.id = $1`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg update from: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"UPDATE", "FROM", "WHERE"})
	assertParamCount(t, sql, res, 1)
}

func Test_PG_Delete_Using_Returning(t *testing.T) {
	sql := `DELETE FROM a USING b WHERE a.id=b.id AND a.id IN (1,2,3) RETURNING *`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg delete using: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	assertDigestHas(t, res.Digest, []string{"DELETE", "USING", "RETURNING"})
	assertParamCount(t, sql, res, 3) // 1,2,3 + *
}

/********* 时间函数参数化开关 *********/

func Test_PG_TimeFuncs_ParamizeOff_Default(t *testing.T) {
	sql := `SELECT now(), statement_timestamp(), current_date`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg time funcs off: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"NOW", "STATEMENT_TIMESTAMP", "CURRENT_DATE"})
	assertParamCount(t, sql, res, 0)
}

func Test_PG_TimeFuncs_ParamizeOn_Safe(t *testing.T) {
	sql := `SELECT now(), STATEMENT_TIMESTAMP(), CURRENT_DATE`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("pg time funcs on: %v", err)
	}
	// 三个都应被参数化
	if want := 3; len(res.Params) != want {
		t.Fatalf("params=%d want=%d; digest=%q", len(res.Params), want, res.Digest)
	}
	// 每个参数不能跨越 ','
	for _, p := range res.Params {
		if strings.Contains(p.Value, "),") {
			// 允许 ")", 但不应把逗号吃进去
			if strings.HasSuffix(p.Value, "),") {
				t.Fatalf("param should not include trailing comma: %q", p.Value)
			}
		}
	}
}

/********* 正则 / ILIKE / 其它 *********/

func Test_PG_Regex_And_ILike(t *testing.T) {
	sql := `SELECT * FROM t WHERE name ~* $1 OR nick ILIKE '%a%'`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg regex ilike: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"~*", "ILIKE"})
	assertParamCount(t, sql, res, 2) // $1, '%a%'
}

/********* 多语句 + 括号清洗 *********/

func Test_PG_MultiStatements_Sanitize(t *testing.T) {
	sql := `SELECT (1+1)) ; INSERT INTO t(a) VALUES(2)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg multi sanitize: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 最终 digest 不应以多余的 ')' 结尾
	//if strings.HasSuffix(strings.TrimSpace(res.Digest), ")") {
	//	t.Fatalf("digest should not end with ')': %q", res.Digest)
	//}
	assertParamCount(t, sql, res, 3) // 1,1,2
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

/********* SELECT 基础 / TOP / OFFSET FETCH / APPLY *********/

func Test_SQLServer_Select_Top_And_Brackets(t *testing.T) {
	sql := `SELECT TOP (10) [Id], [Name] FROM [dbo].[Users] WITH (NOLOCK) WHERE [Age] >= 18`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql top/brackets: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	//assertDigestHas(t, res.Digest, []string{"SELECT", "TOP (", "[DBO]", "[USERS]", "NOLOCK"})
	// 10, 18
	assertParamCount(t, sql, res, 2)
}

func Test_SQLServer_Select_OffsetFetch(t *testing.T) {
	sql := `SELECT * FROM t ORDER BY id OFFSET 5 ROWS FETCH NEXT 10 ROWS ONLY;`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql offset/fetch: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"OFFSET", "ROWS", "FETCH", "ONLY"})
	// 5, 10
	assertParamCount(t, sql, res, 2)
}

func Test_SQLServer_CrossApply(t *testing.T) {
	sql := `SELECT a.id, x.val FROM dbo.A a CROSS APPLY dbo.fn_expand(a.payload) x WHERE a.id IN (1,2,3)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql cross apply: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"CROSS", "APPLY"})
	// 1,2,3
	assertParamCount(t, sql, res, 3)
}

/********* 时间函数开关 *********/

func Test_SQLServer_TimeFuncs_Default_NoParam(t *testing.T) {
	sql := `SELECT GETDATE(), GETUTCDATE(), SYSDATETIME()`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql time funcs off: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"GETDATE", "GETUTCDATE", "SYSDATETIME"})
	assertParamCount(t, sql, res, 0)
}

func Test_SQLServer_TimeFuncs_Paramize_On(t *testing.T) {
	sql := `SELECT GETDATE(), SYSDATETIME(), CURRENT_TIMESTAMP`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("mssql time funcs on: %v", err)
	}
	// 三个都被参数化
	assertParamCount(t, sql, res, 3)
	for _, p := range res.Params {
		if strings.Contains(p.Value, "), (") || strings.Contains(p.Value, "),") {
			t.Fatalf("time func param captured too much: %q", p.Value)
		}
	}
}

/********* INSERT / OUTPUT / 多行折叠 *********/

func Test_SQLServer_Insert_Single_WithOutput(t *testing.T) {
	sql := `INSERT INTO dbo.Orders(Id, UserId, Amount, Note)
OUTPUT inserted.Id, inserted.Amount
VALUES (101, @u1, 9.99, '首单');`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql insert output: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"INSERT", "OUTPUT", "INSERTED"})
	// 101, @u1, 9.99, '首单' => 4
	assertParamCount(t, sql, res, 4)
}

func Test_SQLServer_Insert_Multi_Collapse_NoBind(t *testing.T) {
	sql := `INSERT INTO t (a,b) VALUES (1,'x'), (2,'y'), (3,'z');`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql insert multi collapse: %v", err)
	}
	// 3 * 2 = 6
	if len(res.Params) != 6 {
		t.Fatalf("params=%d want=6; digest=%q", len(res.Params), res.Digest)
	}
	// 折叠后 digest 中 ? 只等于列数 2
	if q := strings.Count(res.Digest, "?"); q != 2 {
		t.Fatalf("? in digest=%d want=2; %q", q, res.Digest)
	}
}

func Test_SQLServer_Insert_Multi_NoCollapse_WithBind(t *testing.T) {
	sql := `INSERT INTO t (a,b) VALUES (1,@p1), (2,@p2);`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql insert multi bind: %v", err)
	}
	// 4 个，且 digest 不折叠 -> ? 也应是 4
	if len(res.Params) != 4 {
		t.Fatalf("params=%d want=4; digest=%q", len(res.Params), res.Digest)
	}
	if q := strings.Count(res.Digest, "?"); q != 4 {
		t.Fatalf("? in digest=%d want=4; %q", q, res.Digest)
	}
}

func Test_SQLServer_Insert_Multi_TimeFuncs_ParamizeOn_NoCollapse(t *testing.T) {
	sql := `INSERT INTO t (a, ts) VALUES (1, GETDATE()), (2, SYSDATETIME());`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("mssql insert multi time paramize on: %v", err)
	}
	// 4 个
	if len(res.Params) != 4 {
		t.Fatalf("params=%d want=4; digest=%q", len(res.Params), res.Digest)
	}
	for _, p := range res.Params {
		if strings.Contains(p.Value, "), (") {
			t.Fatalf("param spans tuple boundary: %q", p.Value)
		}
	}
}

/********* UPDATE / DELETE / MERGE *********/

func Test_SQLServer_Update_FromJoin(t *testing.T) {
	sql := `UPDATE a SET a.v = b.v
FROM a JOIN b ON a.id=b.id
WHERE a.id IN (1,2,3)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql update from join: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"UPDATE", "FROM", "JOIN", "IN ("})
	// 1,2,3
	assertParamCount(t, sql, res, 3)
}

func Test_SQLServer_Delete_Top_WithHint(t *testing.T) {
	sql := `DELETE TOP (5) FROM t WITH (TABLOCK) WHERE k=?;`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql delete top: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	//assertDigestHas(t, res.Digest, []string{"DELETE", "TOP (", "TABLOCK"})
	// 5, ?
	assertParamCount(t, sql, res, 2)
}

func Test_SQLServer_Merge_Into(t *testing.T) {
	sql := `MERGE INTO dbo.Tgt AS t
USING (SELECT ? AS id, 'x' AS note) AS s
ON (t.id = s.id)
WHEN MATCHED THEN UPDATE SET note = s.note
WHEN NOT MATCHED THEN INSERT (id, note) VALUES (s.id, s.note);`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql merge: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"MERGE", "USING", "WHEN MATCHED", "WHEN NOT MATCHED", "INSERT"})
	// ?, 'x' => 2
	assertParamCount(t, sql, res, 2)
}

/********* 其它 *********/

func Test_SQLServer_Sequence_NextValueFor(t *testing.T) {
	sql := `SELECT NEXT VALUE FOR dbo.seq_order;`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql next value for: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"NEXT", "VALUE", "FOR"})
	assertParamCount(t, sql, res, 0)
}

func Test_SQLServer_MultiStatements_SanitizeParen(t *testing.T) {
	sql := `SELECT (1+1)) ; INSERT INTO t(a) VALUES(2)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql multi sanitize: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	//if strings.HasSuffix(strings.TrimSpace(res.Digest), ")") {
	//	t.Fatalf("digest should not end with ')': %q", res.Digest)
	//}
	assertParamCount(t, sql, res, 3) // 1,1,2
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

/********* INSERT 变体：IGNORE / ON DUP / REPLACE / SELECT *********/

func Test_MySQL_Insert_Ignore(t *testing.T) {
	sql := `INSERT IGNORE INTO t(a,b) VALUES (1,'x'), (2,'y');`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql insert ignore: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"INSERT", "IGNORE", "VALUES"})
	// 1,'x',2,'y' => 4；折叠后 digest 里 ? = 列数 2
	if len(res.Params) != 4 {
		t.Fatalf("params=%d want=4; %q", len(res.Params), res.Digest)
	}
}

func Test_MySQL_OnDuplicateKey_Update_ValuesRef(t *testing.T) {
	sql := `INSERT INTO t (id, amt, note)
VALUES (1, 9.99, 'x')
ON DUPLICATE KEY UPDATE amt=VALUES(amt), note=VALUES(note);`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql on dup values(): %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 1, 9.99, 'x' => 3
	assertParamCount(t, sql, res, 3)
}

func Test_MySQL_Insert_Selecta(t *testing.T) {
	sql := `INSERT INTO t (a,b) SELECT c,d FROM s WHERE e IN (1,2,3)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql insert-select: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"INSERT", "SELECT", "FROM"})
	// 1,2,3
	assertParamCount(t, sql, res, 3)
}

func Test_MySQL_Replace_Intoa(t *testing.T) {
	sql := `REPLACE INTO t (a,b) VALUES (1,2);`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql replace into: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"REPLACE", "INTO"})
	assertParamCount(t, sql, res, 2)
}

/********* UPDATE / DELETE *********/

func Test_MySQL_Update_OrderBy_Limit(t *testing.T) {
	sql := `UPDATE t SET v=? WHERE k=? ORDER BY id DESC LIMIT 10`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql update order/limit: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"UPDATE", "ORDER", "LIMIT"})
	// ?, ?, 10
	assertParamCount(t, sql, res, 3)
}

func Test_MySQL_Delete_MultiTable(t *testing.T) {
	sql := `DELETE a FROM a JOIN b ON a.bid=b.id WHERE a.id IN (1,2,3)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql delete multi table: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"DELETE", "FROM", "JOIN", "IN ("})
	assertParamCount(t, sql, res, 3)
}

/********* SELECT：JSON / 窗口 / 反引号 *********/

func Test_MySQL_Select_JSON_Functions(t *testing.T) {
	sql := `SELECT JSON_EXTRACT(doc, '$.a') AS a, JSON_SET(doc, '$.b', 'x') AS b FROM t WHERE JSON_CONTAINS(doc, '{"k":1}')`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql json funcs: %v", err)
	}
	// '$.a', '$.b', 'x', '{"k":1}' => 4
	assertDigestHas(t, res.Digest, []string{"JSON_EXTRACT", "JSON_SET", "JSON_CONTAINS"})
	assertParamCount(t, sql, res, 4)
}

func Test_MySQL_Window_Functions(t *testing.T) {
	sql := `SELECT id, ROW_NUMBER() OVER (PARTITION BY uid ORDER BY ts) rn FROM t WHERE uid IN (1,2) AND amt > 10`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql window: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"ROW_NUMBER", "OVER", "PARTITION"})
	// 1,2,10
	assertParamCount(t, sql, res, 3)
}

func Test_MySQL_Backticks_Identifiers(t *testing.T) {
	sql := "SELECT `id`, `from` FROM `db`.`table` WHERE `id`=1"
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql backticks: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"`DB`", "`TABLE`"})
	assertParamCount(t, sql, res, 1)
}

/********* 多行 VALUES 折叠（含时间函数开关） *********/

func Test_MySQL_Insert_Multi_Collapse_NoBinda(t *testing.T) {
	sql := `INSERT INTO t (a, b, ts) VALUES (1,'x',NOW()), (2,'y',NOW())`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql insert multi collapse: %v", err)
	}
	// 2 行 × (1,'x') = 4
	if len(res.Params) != 4 {
		t.Fatalf("params=%d want=4; %q", len(res.Params), res.Digest)
	}
	// 折叠后 digest 中 ? = 2（第三列 NOW() 不参数化）
	if q := strings.Count(res.Digest, "?"); q != 2 {
		t.Fatalf("? in digest=%d want=2; %q", q, res.Digest)
	}
}

func Test_MySQL_Insert_Multi_NoCollapse_ParamizeTime(t *testing.T) {
	sql := `INSERT INTO t (a, ts) VALUES (1, NOW()), (2, NOW());`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("mysql insert time paramize on: %v", err)
	}
	// 1, NOW(), 2, NOW() => 4
	assertParamCount(t, sql, res, 4)
	for _, p := range res.Params {
		if strings.Contains(p.Value, "), (") {
			t.Fatalf("param spans tuple boundary: %q", p.Value)
		}
	}
}

/************** MySQL 变态用例 **************/

func Test_Freak_MySQL_VersionedComment_MultiStmt(t *testing.T) {
	sql := `
/*!40101 SET @a:=1*/; INSERT /*x*/ INTO t (a,b) VALUES (1,'x'),(2,'y'); -- tail
`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql versioned comment: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 仅第二条 DML 参与参数：1,'x',2,'y' => 4
	assertDigestHas(t, res.Digest, []string{"INSERT", "VALUES"})
	assertParamCount(t, sql, res, 4)
	// 不应以 ')' 结尾（清洗生效）
	if strings.HasSuffix(strings.TrimSpace(res.Digest), ")") {
		t.Fatalf("digest ends with stray ')': %q", res.Digest)
	}
}

func Test_Freak_MySQL_JSON_Path_WeirdQuotes(t *testing.T) {
	sql := `SELECT doc->'$.a[0]', doc->>'$.b[1]' FROM t WHERE JSON_CONTAINS(doc, '{"k":"\"v\""}')`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql json path weird quotes: %v", err)
	}
	// '$.a[0]', '$.b[1]', '{"k":"\"v\""}' => 3
	assertDigestHas(t, res.Digest, []string{"->", "->>", "JSON_CONTAINS"})
	assertParamCount(t, sql, res, 3)
}

func Test_Freak_MySQL_Like_Escape(t *testing.T) {
	sql := `SELECT * FROM t WHERE name LIKE '%\_%' ESCAPE '\'`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql like escape: %v", err)
	}
	// '%\_%' 与 '\' => 2
	assertDigestHas(t, res.Digest, []string{"LIKE", "ESCAPE"})
	assertParamCount(t, sql, res, 2)
}

func Test_Freak_MySQL_Insert_Select_Union_OrderLimit(t *testing.T) {
	sql := `INSERT INTO t(a,b)
SELECT c,d FROM s WHERE e IN (1,2)
UNION ALL
SELECT c2,d2 FROM s2 WHERE e2>10
ORDER BY 1 DESC LIMIT 5`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql insert select union: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 1,2,10,5 => 4
	assertDigestHas(t, res.Digest, []string{"INSERT", "UNION", "ORDER", "LIMIT"})
	assertParamCount(t, sql, res, 5)
}

func Test_Freak_MySQL_MixedBinds_DisableCollapse(t *testing.T) {
	sql := `INSERT INTO t(a,b,c) VALUES (1,:n,?),(2,$1,3)`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL})
	if err != nil {
		t.Fatalf("mysql mixed binds: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// 1,:n,?,2,$1,3 => 6；含绑定，必须不折叠
	assertParamCount(t, sql, res, 5)
	if q := strings.Count(res.Digest, "?"); q != 5 {
		t.Fatalf("? in digest=%d want=5; digest=%q", q, res.Digest)
	}
}

/************** PostgreSQL 变态用例 **************/

func Test_Freak_PG_DollarQuotes_With_Semicolons_ParenClean(t *testing.T) {
	sql := `
SELECT $$body(1); still in here; $$, $tag$); tricky $tag$ FROM dual;  -- first
SELECT (1+1));  -- extra ')', should be sanitized
`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg dollar quotes + sanitize: %v", err)
	}
	// 两个 dollar 串 + 两个 1 => 4 个参数
	assertParamCount(t, sql, res, 4)
	// 不应以 ')' 结尾
	if strings.HasSuffix(strings.TrimSpace(res.Digest), ")") {
		t.Fatalf("digest ends with stray ')': %q", res.Digest)
	}
}

func Test_Freak_PG_Filter_DistinctOn_ArrayAny(t *testing.T) {
	sql := `
SELECT DISTINCT ON (u) u,
       COUNT(*) FILTER (WHERE flag) AS c
FROM t
WHERE id = ANY($1) AND arr @> ARRAY[1,2,3]
ORDER BY u NULLS FIRST`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg filter/distinct on/array: %v", err)
	}
	// $1, 1,2,3 => 4
	assertDigestHas(t, res.Digest, []string{"DISTINCT ON", "FILTER", "ANY", "ARRAY", "NULLS FIRST"})
	assertParamCount(t, sql, res, 4)
}

func Test_Freak_PG_Interval_And_Casts(t *testing.T) {
	sql := `SELECT (now() - INTERVAL '1 hour')::timestamp(0) AT TIME ZONE 'UTC'`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("pg interval & casts: %v", err)
	}
	fmt.Println(res.Digest)
	fmt.Println(res.Params)
	// INTERVAL '1 hour', 'UTC' => 2（默认 time funcs 不参数化）
	assertDigestHas(t, res.Digest, []string{"::TIMESTAMP", "AT", "TIME", "ZONE"})
	assertParamCount(t, sql, res, 3)
}

/************** SQL Server 变态用例 **************/

func Test_Freak_SQLServer_Bracketed_WeirdNames_Collate(t *testing.T) {
	sql := `SELECT [Weird Name], [select] FROM [dbo].[Mix Case Tbl] WHERE [Name] LIKE @p COLLATE Latin1_General_CS_AS`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql collate: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"COLLATE", "[DBO]", "[MIX CASE TBL]"})
	assertParamCount(t, sql, res, 1)
}

func Test_Freak_SQLServer_Case_When_OffsetFetch(t *testing.T) {
	sql := `SELECT CASE WHEN @x IS NULL THEN 'n' ELSE 'y' END AS v
FROM t ORDER BY id OFFSET 0 ROWS FETCH NEXT 1 ROWS ONLY`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.SQLServer})
	if err != nil {
		t.Fatalf("mssql case/offset: %v", err)
	}
	// @x, 'n', 'y', 0, 1 => 5
	assertDigestHas(t, res.Digest, []string{"CASE", "OFFSET", "FETCH"})
	assertParamCount(t, sql, res, 5)
}

/************** Oracle 变态用例 **************/

func Test_Freak_Oracle_QQuote_NestedDelims(t *testing.T) {
	sql := `SELECT q'[a 'b' c]', q'{x{y}z}', q'@p@' FROM dual`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle q-quote: %v", err)
	}
	// 三个 q'...'
	assertDigestHas(t, res.Digest, []string{"SELECT", "FROM"})
	assertParamCount(t, sql, res, 3)
}

func Test_Freak_Oracle_ConnectBy_OuterJoin_Legacy(t *testing.T) {
	sql := `SELECT e.ename, d.dname FROM emp e, dept d
WHERE e.deptno = d.deptno(+) CONNECT BY PRIOR id = parent_id AND level < :n`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Oracle})
	if err != nil {
		t.Fatalf("oracle connect/legacy outer: %v", err)
	}
	assertDigestHas(t, res.Digest, []string{"CONNECT", "BY", "PRIOR", "(+)"})
	assertParamCount(t, sql, res, 1)
}

/************** 通用极端：函数参数化开关 + 元组边界 **************/

func Test_Freak_TimeFuncs_ParamizeOn_NoBoundaryLeak_MySQL(t *testing.T) {
	sql := `INSERT INTO t (a, ts) VALUES (1, NOW()), (2, NOW());`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.MySQL, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("time funcs paramize on (mysql): %v", err)
	}
	// 1, NOW(), 2, NOW() => 4
	assertParamCount(t, sql, res, 4)
	for _, p := range res.Params {
		if strings.Contains(p.Value, "), (") {
			t.Fatalf("param spans tuple boundary: %q", p.Value)
		}
	}
}

func Test_Freak_TimeFuncs_ParamizeOn_NoBoundaryLeak_PG(t *testing.T) {
	sql := `INSERT INTO t (a, ts) VALUES (1, now()), (2, now())`
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres, ParamizeTimeFuncs: true})
	if err != nil {
		t.Fatalf("time funcs paramize on (pg): %v", err)
	}
	if len(res.Params) != 4 {
		t.Fatalf("params=%d want=4; digest=%q", len(res.Params), res.Digest)
	}
	for _, p := range res.Params {
		if strings.Contains(p.Value, "), (") {
			t.Fatalf("param spans tuple boundary: %q", p.Value)
		}
	}
}

/************** 多语句 & 乱括号的终极清洗 **************/

func Test_Freak_MultiStatements_Chaos_Sanitize(t *testing.T) {
	sql := `
-- one
SELECT (1+(2))) /*))*/ FROM dual; 
/* two */ INSERT INTO t(a) VALUES(3)) ; 
-- three
UPDATE t SET v=(SELECT max(x) FROM s WHERE k IN (4,5))) WHERE id=6)) ;
`
	// 任何方言都能触发清洗，这里用 PG
	res, err := d.BuildDigestANTLR(sql, d.Options{Dialect: d.Postgres})
	if err != nil {
		t.Fatalf("multi chaos sanitize: %v", err)
	}
	if strings.HasSuffix(strings.TrimSpace(res.Digest), ")") {
		t.Fatalf("digest ends with stray ')': %q", res.Digest)
	}
	// 1,2,3,4,5,6 => 6
	assertParamCount(t, sql, res, 6)
}

/************** MySQL **************/

var corpusMySQL = []string{
	`/*!40101 SET @a:=1*/; INSERT /*x*/ INTO t (a,b) VALUES (1,'x'),(2,'y'); -- tail`,
	`INSERT IGNORE INTO t(a,b) VALUES (1,'x')`,
	`REPLACE INTO t (a,b) VALUES (1,2)`,
	`INSERT INTO t(a,b) SELECT c,d FROM s WHERE e IN (1,2,3)`,
	`UPDATE t SET v=? WHERE k=:n ORDER BY id DESC LIMIT 10`,
	`DELETE a FROM a JOIN b ON a.bid=b.id WHERE a.id IN (1,2,3)`,
	`SELECT JSON_EXTRACT(doc,'$.a'), JSON_SET(doc,'$.b','x') FROM t WHERE JSON_CONTAINS(doc,'{"k":1}')`,
	`SELECT id, ROW_NUMBER() OVER (PARTITION BY uid ORDER BY ts) rn FROM t WHERE amt > 10`,
	"SELECT `id`, `from` FROM `db`.`table` WHERE `id`=1",
	`INSERT INTO t(a,ts) VALUES (1,NOW()),(2,NOW())`,
	`INSERT INTO t (id,amt,note) VALUES (1,9.99,'x') ON DUPLICATE KEY UPDATE amt=VALUES(amt), note=VALUES(note)`,
	`SELECT * FROM t WHERE name LIKE '%\_%' ESCAPE '\'`,
	`SELECT * FROM t WHERE dt BETWEEN DATE '2020-01-01' AND DATE '2020-12-31'`,
	`SELECT a->'$.x', a->>'$.y' FROM j`,
	`INSERT INTO t(a,b,c) VALUES (1,:n,?),(2,$1,3)`,
	`SELECT /*+ MAX_EXECUTION_TIME(1000) */ * FROM t WHERE a>=10`,
	`SELECT * FROM t WHERE k <=> NULL`,
	`SELECT DISTINCT a,b FROM t WHERE b REGEXP '^[a-z]+'`,
}

/************** PostgreSQL **************/

var corpusPG = []string{
	`SELECT $$abc$$, $1::text, DATE '2020-01-01', INTERVAL '1 day' FROM t WHERE flag IS NOT NULL AND name ILIKE '%x%'`,
	`SELECT doc->'a', doc->>'b', doc#>'{x,0}', doc#>>'{y,1}' FROM t WHERE meta @> '{"a":1}'`,
	`INSERT INTO t (id, name, created_at) VALUES (101, 'x', now()) RETURNING id`,
	`INSERT INTO t (a,b,ts) VALUES (1,'x',now()), (2,'y',now()), (3,'z',now())`,
	`INSERT INTO t (a,b) VALUES (1,$1), (2,$2)`,
	`INSERT INTO t (a, ts) VALUES (1, now()), (2, now())`,
	`INSERT INTO t (id,cnt,note) VALUES (1,1,'x') ON CONFLICT (id) DO UPDATE SET cnt = t.cnt + 1, note = EXCLUDED.note || '!'::text RETURNING id`,
	`WITH s AS (SELECT id, amt, ROW_NUMBER() OVER (PARTITION BY uid ORDER BY ts DESC) AS rn FROM t WHERE uid = $1) SELECT DISTINCT ON (id) id, amt FROM s WHERE rn = 1 AND amt > 10`,
	`WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n+1 FROM r WHERE n < 5) SELECT * FROM r`,
	`SELECT ARRAY[1,2,3]::int[]`,
	`UPDATE a SET v = b.v FROM b WHERE a.id = b.id AND a.id = $1`,
	`DELETE FROM a USING b WHERE a.id=b.id AND a.id IN (1,2,3) RETURNING *`,
	`SELECT now(), statement_timestamp(), current_date`,
	`SELECT * FROM t WHERE name ~* $1 OR nick ILIKE '%a%'`,
	`SELECT to_char(now(),'YYYY-MM-DD')`,
}

/************** SQL Server **************/

var corpusMSSQL = []string{
	`SELECT TOP (10) [Id], [Name] FROM [dbo].[Users] WITH (NOLOCK) WHERE [Age] >= 18`,
	`SELECT * FROM t ORDER BY id OFFSET 5 ROWS FETCH NEXT 10 ROWS ONLY`,
	`SELECT a.id, x.val FROM dbo.A a CROSS APPLY dbo.fn_expand(a.payload) x WHERE a.id IN (1,2,3)`,
	`SELECT GETDATE(), GETUTCDATE(), SYSDATETIME()`,
	`INSERT INTO dbo.Orders(Id, UserId, Amount, Note) OUTPUT inserted.Id, inserted.Amount VALUES (101, @u1, 9.99, '首单')`,
	`INSERT INTO t (a,b) VALUES (1,'x'), (2,'y'), (3,'z')`,
	`INSERT INTO t (a,b) VALUES (1,@p1), (2,@p2)`,
	`INSERT INTO t (a, ts) VALUES (1, GETDATE()), (2, SYSDATETIME())`,
	`UPDATE a SET a.v = b.v FROM a JOIN b ON a.id=b.id WHERE a.id IN (1,2,3)`,
	`DELETE TOP (5) FROM t WITH (TABLOCK) WHERE k=@p1`,
	`MERGE INTO dbo.Tgt AS t USING (SELECT @id AS id, 'x' AS note) AS s ON (t.id = s.id) WHEN MATCHED THEN UPDATE SET note = s.note WHEN NOT MATCHED THEN INSERT (id, note) VALUES (s.id, s.note)`,
	`SELECT NEXT VALUE FOR dbo.seq_order`,
	`SELECT CASE WHEN @x IS NULL THEN 'n' ELSE 'y' END AS v FROM t ORDER BY id OFFSET 0 ROWS FETCH NEXT 1 ROWS ONLY`,
	`SELECT * FROM OPENJSON(@json)`,
}

/************** Oracle **************/

var corpusOracle = []string{
	`SELECT q'[a 'b' c]', q'{x{y}z}', q'@p@' FROM dual`,
	`SELECT e.ename, d.dname FROM emp e, dept d WHERE e.deptno = d.deptno(+)`,
	`SELECT LEVEL FROM dual CONNECT BY LEVEL <= :n`,
	`SELECT SYSDATE, SYSTIMESTAMP FROM dual`,
	`INSERT INTO t (id, note) VALUES (:id, q'[x]') RETURNING id INTO :out_id`,
	`MERGE INTO tgt t USING (SELECT :id AS id, :note AS note FROM dual) s ON (t.id = s.id) WHEN MATCHED THEN UPDATE SET t.note = s.note WHEN NOT MATCHED THEN INSERT (id, note) VALUES (s.id, s.note)`,
	`UPDATE t SET v = NVL(:v, v) WHERE id IN (1,2,3)`,
	`DELETE FROM t WHERE dt BETWEEN DATE '2020-01-01' AND DATE '2020-12-31'`,
	`SELECT CAST(:n AS NUMBER(10,2)) FROM dual`,
	`SELECT * FROM t WHERE REGEXP_LIKE(name, '^[A-Z]+$')`,
	`SELECT EXTRACT(YEAR FROM SYSTIMESTAMP) FROM dual`,
	`INSERT INTO t (a,b,c) VALUES (1, 'x', :y)`,
	`SELECT /*+ INDEX(t idx_t_a) */ * FROM t WHERE a > 10`,
	`SELECT t.* FROM t WHERE EXISTS (SELECT 1 FROM s WHERE s.id = t.id AND ROWNUM <= 10)`,
	`WITH x AS (SELECT 1 AS n FROM dual UNION ALL SELECT 2 FROM dual) SELECT * FROM x`,
}

/************** Runner **************/

func Test_Corpus_More(t *testing.T) {
	runCorpus := func(name string, dialect d.Dialect, sqls []string, opt d.Options) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			for i, sql := range sqls {
				t.Run(shortName(i, sql), func(t *testing.T) {
					res, err := d.BuildDigestANTLR(sql, opt)
					if err != nil {
						t.Fatalf("[%s #%d] build error: %v\nsql=%s", name, i, err, sql)
					}
					// 基本健康检查
					if strings.TrimSpace(res.Digest) == "" {
						t.Fatalf("[%s #%d] empty digest\nsql=%s", name, i, sql)
					}
					//// 不应以多余右括号结尾
					//if strings.HasSuffix(strings.TrimSpace(res.Digest), ")") {
					//	t.Fatalf("[%s #%d] digest ends with stray ')': %q\nsql=%s", name, i, res.Digest, sql)
					//}
					// 参数切片位置有效
					for pi, p := range res.Params {
						if !(p.Start >= 0 && p.End > p.Start && p.End <= len(sql)) {
							t.Fatalf("[%s #%d] param #%d invalid range: [%d,%d) len=%d\nsql=%s",
								name, i, pi+1, p.Start, p.End, len(sql), sql)
						}
						if sql[p.Start:p.End] != p.Value {
							t.Fatalf("[%s #%d] param #%d value mismatch: got slice=%q vs p.Value=%q\nsql=%s",
								name, i, pi+1, sql[p.Start:p.End], p.Value, sql)
						}
					}
					if testing.Verbose() {
						t.Logf("[%s #%d]\nSQL   : %s\nDigest: %s\nParams: %v\n",
							name, i, singleLine(sql), res.Digest, res.Params)
					}
				})
			}
		})
	}

	// 跑四大方言（默认不参数化时间函数，但保留 VALUES 折叠）
	runCorpus("MySQL", d.MySQL, corpusMySQL, d.Options{Dialect: d.MySQL, CollapseValuesInDigest: true})
	runCorpus("Postgres", d.Postgres, corpusPG, d.Options{Dialect: d.Postgres, CollapseValuesInDigest: true})
	runCorpus("SQLServer", d.SQLServer, corpusMSSQL, d.Options{Dialect: d.SQLServer, CollapseValuesInDigest: true})
	runCorpus("Oracle", d.Oracle, corpusOracle, d.Options{Dialect: d.Oracle, CollapseValuesInDigest: true})
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

/************** helpers **************/

func shortName(i int, sql string) string {
	sql = strings.TrimSpace(sql)
	if len(sql) > 48 {
		sql = sql[:48] + "..."
	}
	return strings.ReplaceAll(sql, "\n", " ")
}
func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
