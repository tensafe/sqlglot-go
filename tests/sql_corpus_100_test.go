package tests

import (
	"strings"
	"testing"

	d "tsql_digest_v4/internal/sqldigest_antlr"
)

/*
  说明：
  - 每组 25 条，共 100 条。
  - 断言包括：括号配平、参数切片范围/值一致、避免跨 tuple 的参数拼接。
  - 若你的 Dialect 是字符串底层类型，这里用 “零值同型比较” 兜底。
*/

func Test_Corpus_100(t *testing.T) {
	run := func(name string, dialect d.Dialect, sqls []string, opt d.Options) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			for i, sql := range sqls {
				tcName := shortName(i, sql)
				t.Run(tcName, func(t *testing.T) {
					o := opt
					var zero d.Dialect
					if o.Dialect == zero {
						o.Dialect = dialect
					}
					res, err := d.BuildDigestANTLR(sql, o)
					if err != nil {
						t.Fatalf("[%s #%d] build error: %v\nsql=\n%s", name, i, err, sql)
					}
					if strings.TrimSpace(res.Digest) == "" {
						t.Fatalf("[%s #%d] empty digest\nsql=\n%s", name, i, sql)
					}
					assertParensBalanced(t, res.Digest)

					for pi, p := range res.Params {
						// 范围检查：都是整数 → 用 %d
						if !(p.Start >= 0 && p.End > p.Start && p.End <= len(sql)) {
							t.Fatalf("[%s #%d] param #%d invalid range: [%d,%d) len=%d\nsql=\n%s",
								name, i, pi+1, p.Start, p.End, len(sql), sql)
						}

						// 值检查：都是字符串 → 用 %q
						if sql[p.Start:p.End] != p.Value {
							t.Fatalf("[%s #%d] param #%d value mismatch: slice=%q vs p.Value=%q\nsql=\n%s",
								name, i, pi+1, sql[p.Start:p.End], p.Value, sql)
						}

						// 额外防御：字符串 → 用 %q
						if strings.Contains(p.Value, "), (") {
							t.Fatalf("[%s #%d] param spans tuple boundary: %q\nsql=\n%s",
								name, i, p.Value, sql)
						}
					}
					if testing.Verbose() {
						t.Logf("[%s #%d]\nSQL   : %s\nDigest: %s\nParams: %v\n",
							name, i, oneLine(sql), res.Digest, res.Params)
					}
				})
			}
		})
	}

	run("Postgres_25", d.Postgres, corpusPG25, d.Options{Dialect: d.Postgres, CollapseValuesInDigest: true})
	run("MySQL_25", d.MySQL, corpusMy25, d.Options{Dialect: d.MySQL, CollapseValuesInDigest: true})
	run("SQLServer_25", d.SQLServer, corpusMS25, d.Options{Dialect: d.SQLServer, CollapseValuesInDigest: true})
	run("Oracle_25", d.Oracle, corpusOR25, d.Options{Dialect: d.Oracle, CollapseValuesInDigest: true})
}

/* --------------------- PostgreSQL (01~25) --------------------- */

var corpusPG25 = []string{
	// [PG-01]
	`WITH u AS (
  SELECT id, meta::jsonb AS jb FROM users WHERE status = 'active'
), x AS (
  SELECT u.id, kv.key, kv.value
  FROM u CROSS JOIN LATERAL jsonb_each_text(u.jb->'tags') AS kv
)
SELECT id,
       COUNT(*) FILTER (WHERE key ILIKE '%vip%') AS vip_cnt,
       ARRAY_AGG(value ORDER BY value) AS vals
FROM x GROUP BY id;`,

	// [PG-02]
	`SELECT id, jsonb_path_query(js, '$.store.book[*] ? (@.price > 10)') AS hot
FROM src WHERE js @> '{"type":"book"}';`,

	// [PG-03]
	`SELECT DISTINCT ON (uid) uid, amt,
       SUM(amt) OVER (PARTITION BY uid ORDER BY ts
                      ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW EXCLUDE TIES) AS rsum
FROM t ORDER BY uid, ts NULLS LAST;`,

	// [PG-04]
	`SELECT id FROM t WHERE id = ANY($1) AND arr OPERATOR(pg_catalog.@>) ARRAY[1,2];`,

	// [PG-05]
	`SELECT (ts1, ts1 + INTERVAL '1 hour') OVERLAPS (ts2, ts2 + INTERVAL '30 minutes') AS hit,
       ts1 AT TIME ZONE 'UTC' AT TIME ZONE 'Asia/Taipei' AS local_ts
FROM times;`,

	// [PG-06]
	`WITH RECURSIVE r(n) AS (
  SELECT 1 UNION ALL SELECT n+1 FROM r WHERE n < 100
)
SELECT SUM(n) FROM r;`,

	// [PG-07]
	`WITH j AS (
  SELECT id, jsonb_path_query(js, '$.items[*] ? (@.qty > 0)') AS it FROM src
)
UPDATE tgt t SET js = jsonb_set(t.js, '{mark}', '"hot"', true)
FROM (SELECT id FROM j GROUP BY id HAVING COUNT(*) > 2) hot
WHERE t.id = hot.id RETURNING t.id;`,

	// [PG-08]
	`SELECT * FROM users u
WHERE (u.email ~* '^[a-z0-9._%+-]+@example\.com$'
   OR u.nick ILIKE '%test%')
  AND u.ip << '10.0.0.0/8'::cidr;`,

	// [PG-09]
	`SELECT id, amt,
       SUM(amt) OVER w AS rsum
FROM orders
WINDOW w AS (PARTITION BY uid ORDER BY ts RANGE BETWEEN INTERVAL '1 day' PRECEDING AND CURRENT ROW);`,

	// [PG-10]
	`SELECT u.id, e->>'name' AS name
FROM users u
CROSS JOIN LATERAL jsonb_array_elements(u.meta->'profiles') AS e;`,

	// [PG-11]
	`SELECT u.id,
       CASE WHEN EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id AND o.amt > 100)
            THEN 'vip'
            ELSE (SELECT coalesce(min(note),'none') FROM notes n WHERE n.uid = u.id)
       END AS tag
FROM users u;`,

	// [PG-12]
	`INSERT INTO t(id,cnt,note) VALUES ($1, 1, $$hello$$)
ON CONFLICT (id) DO UPDATE
SET cnt = t.cnt + 1, note = EXCLUDED.note || '!'::text
RETURNING id;`,

	// [PG-13]
	`SELECT region, product, SUM(amt)
FROM sales
GROUP BY GROUPING SETS ((region, product), (region), ());`,

	// [PG-14]
	`SELECT d::date AS day, COALESCE(SUM(amt),0) AS total
FROM generate_series($1::date, $2::date, interval '1 day') d
LEFT JOIN orders o ON o.ts::date = d::date
GROUP BY d ORDER BY d;`,

	// [PG-15]
	`SELECT uid,
       COUNT(*) FILTER (WHERE amt > 10) AS big,
       COUNT(*) FILTER (WHERE amt <=10) AS small
FROM orders GROUP BY uid HAVING COUNT(*) > 5;`,

	// [PG-16]
	`SELECT arr ? 'x' AS has_x, arr ?| ARRAY['a','b'] AS has_any
FROM dicts;`,

	// [PG-17]
	`SELECT id, unnest(tags) AS tag FROM tag_table;`,

	// [PG-18]
	`SELECT percentile_cont(0.9) WITHIN GROUP (ORDER BY amt) OVER (PARTITION BY uid) AS p90
FROM orders;`,

	// [PG-19]
	`SELECT regexp_replace(to_char(now()::timestamp(0), 'YYYY-MM-DD'), '-', '') AS ymd;`,

	// [PG-20]
	`WITH a AS (SELECT * FROM t WHERE flag), b AS (SELECT * FROM a ORDER BY ts DESC)
SELECT * FROM b LIMIT 50 OFFSET 100;`,

	// [PG-21]
	`WITH MATERIALIZED s AS (SELECT * FROM big WHERE k = $1)
SELECT count(*) FROM s;`,

	// [PG-22]
	`SELECT to_tsvector('simple', body) @@ plainto_tsquery('simple', $1) AS hit FROM docs;`,

	// [PG-23]
	`SELECT jsonb_build_object('id', id, 'name', coalesce(name,'N/A')) FROM u;`,

	// [PG-24]
	`SELECT EXTRACT(epoch FROM (now() AT TIME ZONE 'UTC'))::bigint AS ts;`,

	// [PG-25]
	`SELECT 'COPY t FROM STDIN WITH CSV'::text;`,
}

/* --------------------- MySQL 8.0 (26~50) --------------------- */

var corpusMy25 = []string{
	// [MY-26]
	`WITH base AS (
  SELECT u.id AS uid,
         (SELECT SUM(o.amt) FROM orders o WHERE o.user_id = u.id AND o.state IN ('paid','done')) AS sum_amt
  FROM users u WHERE u.flag = 1
)
SELECT b.uid, jt.title, jt.price
FROM base b
JOIN JSON_TABLE(
  (SELECT js FROM src WHERE uid = b.uid LIMIT 1),
  '$.store.book[*]' COLUMNS (
    title VARCHAR(100) PATH '$.title',
    price DECIMAL(10,2) PATH '$.price'
  )
) AS jt
WHERE b.sum_amt > 100;`,

	// [MY-27]
	`SELECT uid, ts, amt,
       SUM(amt) OVER (PARTITION BY uid ORDER BY ts ROWS BETWEEN 2 PRECEDING AND CURRENT ROW) AS rsum
FROM t;`,

	// [MY-28]
	`SELECT JSON_SET(js, '$.flag', true) AS j2
FROM t WHERE JSON_EXTRACT(js, '$.name') REGEXP '^[A-Z].+';`,

	// [MY-29]
	`SELECT t.uid, t.v FROM (
  SELECT a.uid, a.v FROM a WHERE a.v > 0
  UNION ALL
  SELECT b.uid, b.v FROM b WHERE EXISTS (SELECT 1 FROM b2 WHERE b2.id = b.id AND b2.flag = 1)
) AS t WHERE t.uid IN (SELECT id FROM users WHERE status = 'ok');`,

	// [MY-30]
	`SELECT /*+ SET_VAR(optimizer_switch='index_merge=on') */
       x.uid, x.cnt
FROM (
  SELECT u.id AS uid,
         (SELECT COUNT(*) FROM orders o USE INDEX (idx_user_state)
          WHERE o.user_id=u.id AND o.state='paid') AS cnt
  FROM users u FORCE INDEX FOR JOIN (idx_users_flag) WHERE u.flag=1
) AS x WHERE x.cnt >= 10;`,

	// [MY-31]
	`SELECT region, product, SUM(amt) FROM sales
GROUP BY region, product WITH ROLLUP;`,

	// [MY-32]
	`SELECT uid, JSON_ARRAYAGG(note ORDER BY ts) AS j
FROM notes GROUP BY uid;`,

	// [MY-33]
	`SELECT CONVERT_TZ(NOW(), 'UTC', 'Asia/Taipei') AS local_ts;`,

	// [MY-34]
	`SELECT uid, ts, PERCENT_RANK() OVER (PARTITION BY uid ORDER BY amt) AS pr FROM t;`,

	// [MY-35]
	`SELECT uid, ANY_VALUE(name) AS name, SUM(amt) AS total FROM u GROUP BY uid;`,

	// [MY-36]
	`SELECT jt.id, jt.tag
FROM t,
JSON_TABLE(t.js, '$.items[*]' COLUMNS (
  id INT PATH '$.id',
  tags JSON PATH '$.tags'
)) j1
JOIN JSON_TABLE(j1.tags, '$[*]' COLUMNS (tag VARCHAR(50) PATH '$')) jt;`,

	// [MY-37]
	`SELECT REGEXP_REPLACE(name, '[^a-zA-Z0-9]', '') AS cleaned FROM users;`,

	// [MY-38]
	`SELECT uid, NTH_VALUE(amt, 3) OVER (PARTITION BY uid ORDER BY ts) AS third_amt FROM t;`,

	// [MY-39]
	`WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM r WHERE n<100)
SELECT SUM(n) FROM r;`,

	// [MY-40]
	`SELECT id,
  CASE WHEN (SELECT COUNT(*) FROM orders o WHERE o.user_id = u.id AND o.amt > 100) > 0
       THEN 'vip' ELSE 'normal' END AS tag
FROM users u;`,

	// [MY-41]
	`SELECT JSON_OVERLAPS(js, JSON_OBJECT('a', JSON_ARRAY(1,2))) AS ov FROM t;`,

	// [MY-42]
	`SELECT id, amt, SUM(amt) OVER w AS s FROM t WINDOW w AS (ORDER BY ts ROWS 3 PRECEDING);`,

	// [MY-43]
	`SELECT id FROM geo WHERE ST_Distance_Sphere(point(lon,lat), point(121.5,25.0)) < 1000;`,

	// [MY-44]
	`SELECT JSON_MERGE_PATCH(js, '{"flag":true}') FROM t;`,

	// [MY-45]
	`SELECT uid, amt, RANK() OVER (PARTITION BY uid ORDER BY amt DESC) AS rk FROM orders;`,

	// [MY-46]
	`SELECT uid, COUNT(*) AS c FROM events GROUP BY uid
HAVING COUNT(*) > (SELECT AVG(c) FROM (SELECT COUNT(*) AS c FROM events GROUP BY uid) s);`,

	// [MY-47]
	`SELECT DATE_FORMAT(STR_TO_DATE(dt_str, '%Y/%m/%d'), '%Y-%m-%d') FROM raw_dates;`,

	// [MY-48]
	`SELECT JSON_REMOVE(js, JSON_UNQUOTE(JSON_SEARCH(js, 'one', 'delme'))) FROM t;`,

	// [MY-49]
	`WITH s AS (SELECT id FROM u WHERE flag=1)
UPDATE t JOIN s ON t.id=s.id SET t.note='x';`,

	// [MY-50]
	`SELECT id, SUM(amt) OVER (ORDER BY ts RANGE BETWEEN INTERVAL 1 DAY PRECEDING AND CURRENT ROW) FROM t;`,
}

/* --------------------- SQL Server (51~75) --------------------- */

var corpusMS25 = []string{
	// [MS-51]
	`SELECT j.id, j.name, tag.value AS tag
FROM dbo.T t
CROSS APPLY OPENJSON(t.js, '$.items')
  WITH (id INT '$.id', name NVARCHAR(100) '$.name', tags NVARCHAR(MAX) AS JSON) AS j
CROSS APPLY OPENJSON(j.tags) AS tag;`,

	// [MS-52]
	`SELECT u.Id, s.value AS tag
FROM dbo.Users u
CROSS APPLY STRING_SPLIT(u.Tags, ',') s;`,

	// [MS-53]
	`SELECT * FROM (
  SELECT Category, Month, Amount FROM Sales
) src PIVOT (SUM(Amount) FOR Month IN ([Jan],[Feb],[Mar])) p;`,

	// [MS-54]
	`SELECT Product, Attr, Val FROM
( SELECT Product, Color, Size FROM Items ) p
UNPIVOT ( Val FOR Attr IN (Color, Size) ) u;`,

	// [MS-55]
	`SELECT Id, SUM(Val) OVER (PARTITION BY K ORDER BY Ts ROWS BETWEEN 2 PRECEDING AND CURRENT ROW) AS rsum
FROM T ORDER BY Id OFFSET 10 ROWS FETCH NEXT 20 ROWS ONLY;`,

	// [MS-56]
	`SELECT (SYSDATETIMEOFFSET() AT TIME ZONE 'UTC') AT TIME ZONE 'Taipei Standard Time' AS local_ts;`,

	// [MS-57]
	`SELECT TRY_CONVERT(INT, JSON_VALUE(js,'$.a')) AS a FROM t WHERE ISJSON(js)=1;`,

	// [MS-58]
	`SELECT a.Id, x.cnt
FROM dbo.A a
CROSS APPLY (SELECT COUNT(*) AS cnt FROM dbo.B b WHERE b.AId = a.Id AND b.Flag=1) x;`,

	// [MS-59]
	`MERGE INTO dbo.Tgt AS t
USING (SELECT @id AS id, @note AS note) AS s
ON (t.id = s.id)
WHEN MATCHED THEN UPDATE SET note = s.note
WHEN NOT MATCHED THEN INSERT (id, note) VALUES (s.id, s.note);`,

	// [MS-60]
	`SELECT u.Id, x.c
FROM dbo.Users u
OUTER APPLY (
  SELECT COUNT(*) AS c
  FROM dbo.Sessions s
  WHERE s.UserId = u.Id
    AND EXISTS(SELECT 1 FROM dbo.White w WHERE w.Ip = s.Ip)
    AND NOT EXISTS(SELECT 1 FROM dbo.Black b WHERE b.Ip = s.Ip)
) x
WHERE u.Flag = 1;`,

	// [MS-61]
	`SELECT id, name FROM dbo.Users FOR JSON PATH;`,

	// [MS-62]
	`SELECT JSON_MODIFY(js, '$.mark', 1) FROM t WHERE JSON_VALUE(js,'$.a')='x';`,

	// [MS-63]
	`SELECT PERCENTILE_DISC(0.9) WITHIN GROUP (ORDER BY amt)
       OVER (PARTITION BY uid) AS p90 FROM orders;`,

	// [MS-64]
	`SELECT a.Id, f.Val
FROM dbo.A a
CROSS APPLY dbo.fn_expand(a.Payload) f;`,

	// [MS-65]
	`SELECT Region, Product, SUM(Amount)
FROM Sales GROUP BY CUBE (Region, Product);`,

	// [MS-66]
	`SELECT CONVERT(VARCHAR(64), HASHBYTES('SHA2_256', CAST(Id AS VARBINARY(16))), 2) AS sha FROM T;`,

	// [MS-67]
	`SELECT * FROM T FOR SYSTEM_TIME AS OF '2024-01-01T00:00:00Z';`,

	// [MS-68]
	`SELECT * FROM (
  SELECT u.Id, ROW_NUMBER() OVER (PARTITION BY u.GroupId ORDER BY u.Ts DESC) AS rn
  FROM dbo.Users u
) s WHERE s.rn <= 3;`,

	// [MS-69]
	`SELECT u.Id, j2.value AS phone
FROM Users u
CROSS APPLY OPENJSON(u.Profile) WITH (phones NVARCHAR(MAX) AS JSON) j
CROSS APPLY OPENJSON(j.phones) j2;`,

	// [MS-70]
	`SELECT a.Id,
       (SELECT MAX(Price) FROM dbo.Orders o WHERE o.UserId = a.Id) AS maxp,
       CASE WHEN @p IS NULL THEN 'n' ELSE 'y' END AS tag
FROM dbo.Activity a;`,

	// [MS-71]
	`SELECT TOP (10) WITH TIES * FROM T ORDER BY Score DESC;`,

	// [MS-72]
	`SELECT a.Id,
       SUM(a.Val) OVER (PARTITION BY a.GroupId ORDER BY a.Ts ROWS 2 PRECEDING) AS s
FROM dbo.Activity a
ORDER BY a.Id OFFSET 5 ROWS FETCH NEXT 10 ROWS ONLY;`,

	// [MS-73]
	`SELECT * FROM A
CROSS APPLY fnA(A.Col) fa
OUTER APPLY fnB(fa.Col) fb;`,

	// [MS-74]
	`SELECT 'BEGIN TRAN; SET TRANSACTION ISOLATION LEVEL SNAPSHOT' AS note;`,

	// [MS-75]
	`SELECT NEXT VALUE FOR dbo.seq_order AS next_id;`,
}

/* --------------------- Oracle (76~100) --------------------- */

var corpusOR25 = []string{
	// [OR-76]
	`WITH base AS (
  SELECT id, js, amt, ts FROM t WHERE JSON_EXISTS(js, '$.a?(@ > 1)')
)
SELECT id,
       JSON_VALUE(js, '$.name') AS nm,
       ROW_NUMBER() OVER (PARTITION BY JSON_VALUE(js,'$.grp') ORDER BY ts DESC) AS rn
FROM base;`,

	// [OR-77]
	`SELECT id, parent_id, LEVEL AS lv
FROM tree START WITH parent_id IS NULL
CONNECT BY PRIOR id = parent_id;`,

	// [OR-78]
	`SELECT * FROM sales
MATCH_RECOGNIZE (
  PARTITION BY prod
  ORDER BY tstamp
  MEASURES FIRST(A.tstamp) AS s, LAST(A.tstamp) AS e
  PATTERN (A+)
  DEFINE A AS A.amount > 100
);`,

	// [OR-79]
	`SELECT jt.id, jt.name
FROM src,
JSON_TABLE(src.js, '$.users[*]'
  COLUMNS ( id NUMBER PATH '$.id', name VARCHAR2(100) PATH '$.name' )
) jt;`,

	// [OR-80]
	`MERGE INTO tgt t
USING (SELECT :id AS id, :note AS note FROM dual) s
ON (t.id = s.id)
WHEN MATCHED THEN UPDATE SET t.note = s.note
WHEN NOT MATCHED THEN INSERT (id, note) VALUES (s.id, s.note);`,

	// [OR-81]
	`SELECT deptno, LISTAGG(ename, ',') WITHIN GROUP (ORDER BY ename) AS names
FROM emp GROUP BY deptno;`,

	// [OR-82]
	`SELECT id,
       SUM(amt) OVER (PARTITION BY k ORDER BY ts
          RANGE BETWEEN INTERVAL '1' DAY PRECEDING AND CURRENT ROW) AS rsum
FROM t WHERE ts >= DATE '2024-01-01';`,

	// [OR-83]
	`SELECT * FROM users WHERE REGEXP_LIKE(name, '^[A-Z]') AND NVL(flag,0)=1;`,

	// [OR-84]
	`SELECT REPLACE(SUBSTR(name, 1, INSTR(name,'@')-1), '.', '_') AS local FROM users;`,

	// [OR-85]
	`SELECT region, product, sales FROM sales
MODEL RETURN UPDATED ROWS
PARTITION BY (region)
DIMENSION BY (product)
MEASURES (sales)
RULES ( sales['ALL'] = SUM(sales)[ANY] );`,

	// [OR-86]
	`SELECT a.*, f.val
FROM a CROSS APPLY TABLE(fn_expand(a.payload)) f;`,

	// [OR-87]
	`SELECT * FROM (
  SELECT deptno, job, sal FROM emp
) src PIVOT (SUM(sal) FOR job IN ('CLERK','ANALYST','MANAGER')) p;`,

	// [OR-88]
	`SELECT deptno, attr, val FROM (
  SELECT deptno, c1, c2 FROM t
) UNPIVOT ( val FOR attr IN (c1, c2) );`,

	// [OR-89]
	`SELECT * FROM users u
WHERE ROWNUM <= 100
  AND EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id AND o.amt > 10);`,

	// [OR-90]
	`SELECT REGEXP_REPLACE(TO_CHAR(SYSDATE, 'YYYY-MM-DD'), '-', '') AS ymd FROM dual;`,

	// [OR-91]
	`SELECT id, amt,
       LAG(amt,1,0) OVER (PARTITION BY uid ORDER BY ts) AS prev_amt
FROM orders;`,

	// [OR-92]
	`SELECT deptno,
       MAX(sal) KEEP (DENSE_RANK LAST ORDER BY ts) AS last_max
FROM emp GROUP BY deptno;`,

	// [OR-93]
	`SELECT x.col
FROM t,
     XMLTABLE('/root/item' PASSING t.xml
       COLUMNS col VARCHAR2(100) PATH 'name') x;`,

	// [OR-94]
	`SELECT JSON_MERGEPATCH(js, '{"flag":true}') FROM t;`,

	// [OR-95]
	`SELECT * FROM ticks
MATCH_RECOGNIZE (
  PARTITION BY sym
  ORDER BY ts
  MEASURES MATCH_NUMBER() AS m
  PATTERN (A+ B* C?)
  DEFINE A AS A.price > PREV(A.price),
         B AS B.price <= PREV(B.price),
         C AS C.price > 100
);`,

	// [OR-96]
	`SELECT /*+ QUERY_PARTITION_HASH(t) */ COUNT(*) FROM t PARTITION (P2024);`,

	// [OR-97]
	`SELECT TO_CHAR(CAST(JSON_VALUE(js,'$.amt') AS NUMBER(10,2)), 'FM9999990D00', 'NLS_NUMERIC_CHARACTERS=.,') FROM t;`,

	// [OR-98]
	`SELECT id FROM tree
START WITH id = :root
CONNECT BY PRIOR id = parent_id AND PRIOR active = 1;`,

	// [OR-99]
	`SELECT 'MERGE with RETURNING' FROM dual;`,

	// [OR-100]
	`WITH a AS (SELECT id FROM t WHERE flag=1),
     b AS (SELECT id FROM t WHERE flag=2),
     c AS (SELECT id FROM a UNION ALL SELECT id FROM b)
SELECT * FROM c WHERE ROWNUM <= 100;`,
}

/* --------------------- helpers --------------------- */

//func shortName(i int, sql string) string {
//	sql = strings.TrimSpace(sql)
//	if len(sql) > 72 {
//		sql = sql[:72] + "..."
//	}
//	return strings.ReplaceAll(sql, "\n", " ")
//}
//
//func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
//
//// 括号配平：逐语句（以 ; 分隔）检查，允许合法以 ')' 结尾
//func assertParensBalanced(t *testing.T, digest string) {
//	t.Helper()
//	for _, stmt := range strings.Split(digest, ";") {
//		stmt = strings.TrimSpace(stmt)
//		if stmt == "" {
//			continue
//		}
//		bal := 0
//		minBal := 0
//		for _, r := range stmt {
//			switch r {
//			case '(':
//				bal++
//			case ')':
//				bal--
//				if bal < minBal {
//					minBal = bal
//				}
//			}
//		}
//		if minBal < 0 {
//			t.Fatalf("digest has stray ')': %q", stmt)
//		}
//		if bal != 0 {
//			t.Fatalf("digest parens not balanced (bal=%d): %q", bal, stmt)
//		}
//	}
//}
