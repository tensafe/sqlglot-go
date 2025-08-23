package tests

import (
	"fmt"
	d "github.com/tensafe/sqlglot-go/internal/sqldigest_antlr"
	"strings"
	"testing"
)

//go test -v -count=1 . -run Oracle_Complex_Corpus_Standalone_ORA

// 独立测试入口；函数名与辅助函数都带 _ORA 后缀，避免与现有用例/Helper 冲突。
func Test_Oracle_Complex_Corpus_Standalone_ORA(t *testing.T) {
	sqls := oraComplexSQLs()
	opt := d.Options{
		Dialect:                d.Oracle,
		CollapseValuesInDigest: true,
		ParamizeTimeFuncs:      true,
	}

	for i, sql := range sqls {
		res, err := d.BuildDigestANTLR(sql, opt)
		if err != nil {
			t.Fatalf("[ORA#%d] build error: %v\nsql=\n%s", i+1, err, sql)
		}
		if strings.TrimSpace(res.Digest) == "" {
			t.Fatalf("[ORA#%d] empty digest\nsql=\n%s", i+1, sql)
		}
		assertParensBalanced_ORA(t, res.Digest)
		fmt.Println(sql)
		fmt.Println(res.Digest)
		fmt.Println(res.Params)

		for pi, p := range res.Params {
			if !(p.Start >= 0 && p.End > p.Start && p.End <= len(sql)) {
				t.Fatalf("[ORA#%d] param #%d invalid range: [%d,%d) len=%d\nsql=\n%s",
					i+1, pi+1, p.Start, p.End, len(sql), sql)
			}
			if got := sql[p.Start:p.End]; got != p.Value {
				t.Fatalf("[ORA#%d] param #%d value mismatch: slice=%q vs p.Value=%q\nsql=\n%s",
					i+1, pi+1, got, p.Value, sql)
			}
			// 参数不应跨越 tuple 边界
			if strings.Contains(p.Value, "), (") {
				t.Fatalf("[ORA#%d] param spans tuple boundary: %q\nsql=\n%s",
					i+1, p.Value, sql)
			}
		}
		if strings.HasSuffix(strings.TrimSpace(res.Digest), ")") {
			// 额外保险：不允许以多余右括号结尾（单语句 digest）
			// 若是多语句 digest，可按分号拆分再判（此处复杂 SQL 基本为单语句或分号安全）
			// 你也可以换成逐语句检测：assertParensBalanced_ORA 已覆盖。
		}
	}
}

/* ------------------------------ 20 条 Oracle 重口味 SQL ------------------------------ */

func oraComplexSQLs() []string {
	return []string{
		// 1) MERGE + JSON_MERGEPATCH + SYSTIMESTAMP + 条件删除
		`MERGE INTO dst d
USING (SELECT :id AS id, :name AS name, :js AS js FROM dual) s
ON (d.id = s.id)
WHEN MATCHED THEN UPDATE SET
  d.name = s.name,
  d.meta = JSON_MERGEPATCH(COALESCE(d.meta, '{}'), s.js),
  d.updated_at = SYSTIMESTAMP
  DELETE WHERE d.deleted = 1
WHEN NOT MATCHED THEN INSERT (id, name, meta, created_at)
  VALUES (s.id, s.name, s.js, SYSTIMESTAMP);`,

		// 2) INSERT ALL + q'{}' 特殊引号
		`INSERT ALL
  INTO notes(id, txt) VALUES (notes_seq.NEXTVAL, q'{含分号;与引号''与右括号)}')
  INTO logs(uid, msg, ts) VALUES (:u, 'hello', SYSTIMESTAMP)
SELECT 1 FROM dual;`,

		// 3) UPDATE + LISTAGG WITHIN GROUP
		`UPDATE dept d
SET d.last_names = (
  SELECT LISTAGG(e.ename, ',') WITHIN GROUP (ORDER BY e.ename)
  FROM emp e WHERE e.deptno = d.deptno
)
WHERE EXISTS (SELECT 1 FROM emp e WHERE e.deptno = d.deptno);`,

		// 4) DELETE + 层级查询 CONNECT BY
		`DELETE FROM t
WHERE id IN (
  SELECT id FROM tree
  START WITH parent_id IS NULL
  CONNECT BY PRIOR id = parent_id
);`,

		// 5) MATCH_RECOGNIZE 模式识别
		`SELECT * FROM ticks
MATCH_RECOGNIZE (
  PARTITION BY sym
  ORDER BY ts
  MEASURES MATCH_NUMBER() AS match_no, LAST(A.price) AS a_last
  PATTERN (A+ B* C?)
  DEFINE
    A AS A.price > PREV(A.price),
    B AS B.price <= PREV(B.price),
    C AS C.price > 100
);`,

		// 6) MODEL 子句
		`SELECT region, product, sales
FROM sales
MODEL RETURN UPDATED ROWS
PARTITION BY (region)
DIMENSION BY (product)
MEASURES (sales)
RULES UPSERT ( sales['ALL'] = SUM(sales)[ANY] );`,

		// 7) PIVOT 旋转
		`SELECT *
FROM (
  SELECT deptno, job, sal FROM emp
) src
PIVOT (SUM(sal) FOR job IN ('CLERK','ANALYST','MANAGER'));`,

		// 8) UNPIVOT 反旋转
		`SELECT deptno, job, sal
FROM pivot_emp
UNPIVOT (sal FOR job IN (clerk AS 'CLERK', analyst AS 'ANALYST', manager AS 'MANAGER'));`,

		// 9) 窗口函数（累计和 + LAG/LEAD）
		`SELECT id,
       SUM(v) OVER (PARTITION BY k ORDER BY ts ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS run_sum,
       LAG(v,1) OVER (PARTITION BY k ORDER BY ts) AS prev_v,
       LEAD(v,1) OVER (PARTITION BY k ORDER BY ts) AS next_v
FROM t;`,

		// 10) UPDATE + JSON_TABLE 派生列更新
		`UPDATE users u
SET (u.is_vip, u.is_ban) = (
  SELECT jt.vip, jt.ban
  FROM JSON_TABLE(u.profile, '$'
    COLUMNS (vip NUMBER(1) PATH '$.vip', ban NUMBER(1) PATH '$.ban')
  ) jt
)
WHERE JSON_EXISTS(u.profile, '$.vip') OR JSON_EXISTS(u.profile, '$.ban');`,

		// 11) XMLTABLE 解析插入
		`INSERT INTO x(col)
SELECT xt.col
FROM src s,
     XMLTABLE('/root/item' PASSING s.xml
       COLUMNS col VARCHAR2(100) PATH 'name') xt;`,

		// 12) 递归 CTE + SEARCH/CYCLE
		`WITH t (id, parent_id, lvl) AS (
  SELECT id, parent_id, 1 FROM tree WHERE parent_id IS NULL
  UNION ALL
  SELECT c.id, c.parent_id, t.lvl+1
  FROM tree c JOIN t ON c.parent_id = t.id
)
SEARCH DEPTH FIRST BY id SET ord
CYCLE id SET is_cycle TO 1 DEFAULT 0
SELECT id, parent_id, lvl, ord, is_cycle FROM t;`,

		// 13) 正则函数族
		`SELECT REGEXP_SUBSTR(email, '[^@]+', 1, 1) AS local,
       REGEXP_REPLACE(email, '[^a-z0-9@._-]', '') AS cleaned
FROM users
WHERE REGEXP_LIKE(email, :pat);`,

		// 14) 时区/间隔运算
		`SELECT FROM_TZ(SYSTIMESTAMP, 'UTC') AT TIME ZONE 'Asia/Taipei' AS ts_local,
       (CURRENT_DATE + INTERVAL '7' DAY) AS next_week
FROM dual;`,

		// 15) DELETE ... RETURNING INTO
		`DELETE FROM sessions
WHERE last_seen < SYSTIMESTAMP - INTERVAL '90' DAY
RETURNING id INTO :id;`,

		// 16) INSERT ... RETURNING INTO（序列）
		`INSERT INTO orders (id, uid, amt, note, created_at)
VALUES (orders_seq.NEXTVAL, :u, :a, q'{首单}', SYSTIMESTAMP)
RETURNING id INTO :new_id;`,

		// 17) UPDATE 可更新内联视图 + 分析列
		`UPDATE (
  SELECT o.amt, SUM(o.amt) OVER (PARTITION BY o.uid) AS total_amt
  FROM orders o
  WHERE o.ts >= DATE '2024-01-01'
)
SET amt = ROUND(amt / total_amt * 100, 2);`,

		// 18) MERGE + 条件删除（库存示例）
		`MERGE INTO inv d
USING (SELECT :sku AS sku, :q AS q FROM dual) s
ON (d.sku = s.sku)
WHEN MATCHED THEN UPDATE SET d.q = d.q + s.q
  DELETE WHERE d.q <= 0
WHEN NOT MATCHED THEN INSERT (sku, q) VALUES (s.sku, s.q);`,

		// 19) GROUPING SETS/ROLLUP/CUBE 族（用 GROUPING SETS）
		`SELECT region, product, channel, SUM(amt) AS s
FROM sales
GROUP BY GROUPING SETS ((region, product, channel), (region, product), (region), ());`,

		// 20) q'...' 多种分隔符混合
		`SELECT q'[a(b)c]'   AS t1,
       q'<json {"x":[1,2,3]}>' AS t2,
       q'{右括号)也在这}'     AS t3
FROM dual;`,
	}
}

/* ------------------------------ 辅助断言（仅本文件使用） ------------------------------ */

// 逐“语句”（以 ; 分割）做括号配平。只判定多余右括号与总平衡，不尝试修复。
func assertParensBalanced_ORA(t *testing.T, digest string) {
	t.Helper()
	for _, stmt := range strings.Split(digest, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		bal := 0
		minBal := 0
		for _, r := range stmt {
			switch r {
			case '(':
				bal++
			case ')':
				bal--
				if bal < minBal {
					minBal = bal
				}
			}
		}
		if minBal < 0 {
			t.Fatalf("digest has stray ')': %q", stmt)
		}
		if bal != 0 {
			t.Fatalf("digest parens not balanced (bal=%d): %q", bal, stmt)
		}
	}
}
