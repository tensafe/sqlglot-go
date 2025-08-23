package tests

import (
	"strings"
	"testing"

	d "github.com/tensafe/sqlglot-go/internal/sqldigest_antlr"
)

type nestedItem struct {
	name    string
	dialect d.Dialect
	sql     string
	opt     d.Options
}

func Test_Corpus_Nested_Heavy(t *testing.T) {
	items := []nestedItem{
		/* ============================ PostgreSQL ============================ */
		{
			name:    "PG_DeepNested_Lateral_JSON_Array_Windows",
			dialect: d.Postgres,
			sql: `
WITH base AS (
  SELECT u.id, u.nick, u.meta::jsonb AS jb, u.created_at
  FROM users u
  WHERE u.status = 'active'
),
exp AS (
  SELECT b.id,
         x.key AS tag_key,
         x.value AS tag_val,
         b.created_at
  FROM base b
  CROSS JOIN LATERAL jsonb_each_text(b.jb->'tags') AS x
  WHERE EXISTS (
    SELECT 1
    FROM accounts a
    WHERE a.user_id = b.id
      AND a.state IN ('ok','vip')
      AND a.last_login >= (now() - INTERVAL '7 days')
  )
),
agg AS (
  SELECT id,
         COUNT(*) FILTER (WHERE tag_key ILIKE '%vip%') AS vip_cnt,
         COUNT(*) AS all_cnt,
         ARRAY_AGG(tag_val ORDER BY tag_val) AS vals
  FROM exp
  GROUP BY id
)
SELECT a.id,
       (SELECT COUNT(*) FROM orders o WHERE o.user_id = a.id AND o.amt > 10) AS big_orders,
       COALESCE(a.vip_cnt, 0) AS vip_cnt,
       a.all_cnt,
       a.vals OPERATOR(pg_catalog.@>) ARRAY['gold'] AS has_gold,
       SUM(a.all_cnt) OVER (ORDER BY a.id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW EXCLUDE TIES) AS running
FROM agg a
WHERE a.id = ANY($1)
ORDER BY a.id NULLS LAST
LIMIT 50 OFFSET 10
			`,
			opt: d.Options{Dialect: d.Postgres, CollapseValuesInDigest: true},
		},
		{
			name:    "PG_Subquery_In_Select_Overlaps_TimeZone",
			dialect: d.Postgres,
			sql: `
SELECT u.id,
       (SELECT avg(amt) FROM orders o WHERE o.user_id = u.id AND o.ts AT TIME ZONE 'UTC' >= NOW() - INTERVAL '1 day') AS avg1d,
       (ts1, ts1 + INTERVAL '1 hour') OVERLAPS (ts2, ts2 + INTERVAL '30 minutes') AS hit
FROM users u
WHERE EXISTS (
  SELECT 1
  FROM (SELECT * FROM sessions s WHERE s.user_id = u.id ORDER BY started_at DESC LIMIT 5) s5
  WHERE s5.ip << '10.0.0.0/8'::cidr
)
			`,
			opt: d.Options{Dialect: d.Postgres},
		},
		{
			name:    "PG_JSONB_Path_Nest_Update_From_Derived",
			dialect: d.Postgres,
			sql: `
WITH j AS (
  SELECT id, jsonb_path_query(js, '$.store.book[*] ? (@.price > 10)') AS x
  FROM src
  WHERE js @> '{"type":"book"}'
)
UPDATE tgt t
SET js = jsonb_set(t.js, '{mark}', '"hot"', true)
FROM (
  SELECT id FROM j GROUP BY id HAVING COUNT(*) > 2
) hot
WHERE t.id = hot.id
RETURNING t.id
			`,
			opt: d.Options{Dialect: d.Postgres},
		},

		/* ============================ MySQL 8.0 ============================ */
		{
			name:    "MySQL_DeepNested_Derived_JSONTABLE_Correlated",
			dialect: d.MySQL,
			sql: `
SELECT d.uid, d.sum_amt, jt.title, jt.price
FROM (
  SELECT u.id AS uid,
         (SELECT SUM(o.amt) FROM orders o WHERE o.user_id = u.id AND o.state IN ('paid','done')) AS sum_amt
  FROM users u
  WHERE u.flag = 1 AND EXISTS (
    SELECT 1 FROM sessions s WHERE s.user_id = u.id AND s.started_at >= NOW() - INTERVAL 3 DAY
  )
) AS d
JOIN JSON_TABLE(
      (SELECT src.js FROM src WHERE src.uid = d.uid LIMIT 1),
      '$.store.book[*]'
      COLUMNS (
        title VARCHAR(100) PATH '$.title',
        price DECIMAL(10,2) PATH '$.price'
      )
) AS jt
WHERE d.sum_amt > 100
ORDER BY d.uid, jt.price DESC
LIMIT 100
			`,
			opt: d.Options{Dialect: d.MySQL},
		},
		{
			name:    "MySQL_Subquery_In_From_Union_In_Exists_Window",
			dialect: d.MySQL,
			sql: `
SELECT t.uid,
       SUM(t.v) OVER (PARTITION BY t.uid ORDER BY t.ts ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) AS rsum
FROM (
  SELECT a.uid, a.v, a.ts
  FROM a
  WHERE a.v > 0
  UNION ALL
  SELECT b.uid, b.v, b.ts
  FROM b
  WHERE EXISTS (
    SELECT 1 FROM b2 WHERE b2.id = b.id AND b2.flag = 1
  )
) AS t
WHERE t.uid IN (SELECT id FROM users WHERE status = 'ok')
			`,
			opt: d.Options{Dialect: d.MySQL},
		},
		{
			name:    "MySQL_OptimizerHints_IndexHints_DeepDerived",
			dialect: d.MySQL,
			sql: `
SELECT /*+ SET_VAR(optimizer_switch='index_merge=on') */
       x.uid, x.cnt
FROM (
  SELECT u.id AS uid,
         (SELECT COUNT(*) FROM orders o USE INDEX (idx_user_state)
           WHERE o.user_id = u.id AND o.state = 'paid') AS cnt
  FROM users u FORCE INDEX FOR JOIN (idx_users_flag)
  WHERE u.flag = 1
) AS x
WHERE x.cnt >= 10
ORDER BY x.uid
			`,
			opt: d.Options{Dialect: d.MySQL},
		},

		/* ============================ SQL Server ============================ */
		{
			name:    "MSSQL_CrossApply_OpenJson_Nested_Aggregates",
			dialect: d.SQLServer,
			sql: `
WITH base AS (
  SELECT u.Id, u.Profile
  FROM dbo.Users AS u WITH (NOLOCK)
  WHERE EXISTS (
    SELECT 1 FROM dbo.Sessions s WITH (NOLOCK)
    WHERE s.UserId = u.Id AND s.StartedAt >= DATEADD(day, -7, SYSUTCDATETIME())
  )
),
exp AS (
  SELECT b.Id,
         j.value AS item
  FROM base b
  CROSS APPLY OPENJSON(b.Profile, '$.items') AS j
),
tagged AS (
  SELECT e.Id, jt.value AS tag
  FROM exp e
  CROSS APPLY OPENJSON(e.item, '$.tags') AS jt
)
SELECT t.Id,
       SUM(CASE WHEN t.tag = 'vip' THEN 1 ELSE 0 END) AS vip_tags,
       COUNT(*) AS total_tags
FROM tagged t
GROUP BY t.Id
HAVING SUM(CASE WHEN t.tag = 'vip' THEN 1 ELSE 0 END) >= 2
ORDER BY t.Id
			`,
			opt: d.Options{Dialect: d.SQLServer},
		},
		{
			name:    "MSSQL_Subquery_SelectList_Window_OffsetFetch",
			dialect: d.SQLServer,
			sql: `
SELECT a.Id,
       (SELECT MAX(Price) FROM dbo.Orders o WHERE o.UserId = a.Id) AS max_price,
       SUM(a.Val) OVER (PARTITION BY a.GroupId ORDER BY a.Ts ROWS BETWEEN 2 PRECEDING AND CURRENT ROW) AS rsum
FROM dbo.Activity a
WHERE a.State = @p AND a.Id IN (SELECT Id FROM dbo.Users WHERE Flag = 1)
ORDER BY a.Id
OFFSET 10 ROWS FETCH NEXT 20 ROWS ONLY
			`,
			opt: d.Options{Dialect: d.SQLServer},
		},

		/* ============================ Oracle ============================ */
		{
			name:    "Oracle_JSONValue_Exists_Analytic_Subquery",
			dialect: d.Oracle,
			sql: `
WITH base AS (
  SELECT id, js, amt, ts
  FROM t
  WHERE JSON_EXISTS(js, '$.a?(@ > 1)')
),
rnk AS (
  SELECT b.id,
         JSON_VALUE(b.js, '$.name') AS nm,
         b.amt,
         b.ts,
         ROW_NUMBER() OVER (PARTITION BY JSON_VALUE(b.js,'$.grp') ORDER BY b.ts DESC) AS rn
  FROM base b
)
SELECT r.id, r.nm, r.amt
FROM rnk r
WHERE r.rn = 1
  AND EXISTS (
    SELECT 1 FROM orders o
    WHERE o.user_id = r.id
      AND o.note LIKE q'[VIP%]'
  )
			`,
			opt: d.Options{Dialect: d.Oracle},
		},
		{
			name:    "Oracle_ConnectBy_With_Subquery_Start_MatchRecognize",
			dialect: d.Oracle,
			sql: `
WITH roots AS (
  SELECT id FROM tree WHERE parent_id IS NULL AND ROWNUM <= 100
)
SELECT t.*
FROM tree t
START WITH t.id IN (SELECT id FROM roots)
CONNECT BY PRIOR t.id = t.parent_id
AND EXISTS (
  SELECT 1 FROM metrics m WHERE m.node_id = t.id AND m.val > :minv
);
SELECT * FROM sales
MATCH_RECOGNIZE (
  PARTITION BY prod
  ORDER BY tstamp
  MEASURES FIRST(A.tstamp) AS s, LAST(A.tstamp) AS e
  PATTERN (A+ B*)
  DEFINE A AS A.amount > 100, B AS B.amount <= 10
)
			`,
			opt: d.Options{Dialect: d.Oracle},
		},

		/* ============================ Extra 混合套娃 ============================ */
		{
			name:    "Mixed_CTE_Union_All_Subqueries_ManyLevels",
			dialect: d.Postgres,
			sql: `
WITH u AS (
  SELECT id, status FROM users WHERE created_at >= TIMESTAMP '2024-01-01 00:00:00'
),
o AS (
  SELECT user_id, SUM(amt) AS total
  FROM orders WHERE state IN ('paid','done')
  GROUP BY user_id
),
topu AS (
  SELECT u.id, COALESCE(o.total,0) AS tot
  FROM u LEFT JOIN o ON o.user_id = u.id
  WHERE EXISTS (SELECT 1 FROM sessions s WHERE s.user_id = u.id AND s.started_at >= now() - INTERVAL '30 days')
),
final AS (
  SELECT id, tot FROM topu WHERE tot >= 100
  UNION ALL
  SELECT id, tot FROM topu WHERE tot BETWEEN 50 AND 99
)
SELECT f.id,
       (SELECT COUNT(*) FROM messages m WHERE m.user_id = f.id AND m.body ~* 'promo') AS msgcnt,
       f.tot
FROM final f
WHERE f.id NOT IN (SELECT banned_id FROM banned)
ORDER BY f.tot DESC, f.id
LIMIT 200
			`,
			opt: d.Options{Dialect: d.Postgres, CollapseValuesInDigest: true},
		},

		/* ============================ 进一步：子表 / 派生表反复嵌套 ============================ */
		{
			name:    "MySQL_TripleDerived_AntiJoin_ScalarSubselect",
			dialect: d.MySQL,
			sql: `
SELECT z.uid, z.maxv
FROM (
  SELECT y.uid,
         (SELECT MAX(v) FROM meter m WHERE m.uid = y.uid AND m.ts > y.mints) AS maxv
  FROM (
    SELECT x.uid, MIN(x.ts) AS mints
    FROM (
      SELECT a.uid, a.ts FROM a WHERE a.flag = 1
      UNION ALL
      SELECT b.uid, b.ts FROM b WHERE b.kind IN (1,2,3)
    ) AS x
    GROUP BY x.uid
  ) AS y
) AS z
LEFT JOIN black bl ON bl.uid = z.uid
WHERE bl.uid IS NULL
			`,
			opt: d.Options{Dialect: d.MySQL},
		},
		{
			name:    "MSSQL_DeepApply_Subquery_Exists_NotExists",
			dialect: d.SQLServer,
			sql: `
SELECT u.Id,
       x.cnt,
       (SELECT TOP(1) o.Price FROM dbo.Orders o WHERE o.UserId = u.Id ORDER BY o.Price DESC) AS maxp
FROM dbo.Users u
OUTER APPLY (
  SELECT COUNT(*) AS cnt
  FROM dbo.Sessions s
  WHERE s.UserId = u.Id
    AND EXISTS (SELECT 1 FROM dbo.IpWhite w WHERE w.Ip = s.Ip)
    AND NOT EXISTS (SELECT 1 FROM dbo.IpBlack b WHERE b.Ip = s.Ip)
) AS x
WHERE u.Flag = 1
			`,
			opt: d.Options{Dialect: d.SQLServer},
		},
		{
			name:    "Oracle_Subquery_In_Case_And_Exists_UnionDerived",
			dialect: d.Oracle,
			sql: `
SELECT u.id,
       CASE
         WHEN EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id AND o.amt > 100) THEN 'vip'
         ELSE (SELECT NVL(MIN(note),'none') FROM notes n WHERE n.uid = u.id)
       END AS tag
FROM users u
WHERE u.status = 'ok'
  AND u.id IN (
    SELECT uid FROM (
      SELECT uid FROM a WHERE v > 0
      UNION ALL
      SELECT uid FROM b WHERE v > 0
    )
  )
			`,
			opt: d.Options{Dialect: d.Oracle},
		},
	}

	for _, it := range items {
		it := it
		t.Run(it.name, func(t *testing.T) {
			opt := it.opt
			// 统一兜底：若未显式设置方言，则赋值为用例指定的方言
			var zero d.Dialect
			if opt.Dialect == zero {
				opt.Dialect = it.dialect
			}

			res, err := d.BuildDigestANTLR(it.sql, opt)
			if err != nil {
				t.Fatalf("[%s] build error: %v\nsql=\n%s", it.name, err, it.sql)
			}
			// 基本健康检查
			if strings.TrimSpace(res.Digest) == "" {
				t.Fatalf("[%s] empty digest\nsql=\n%s", it.name, it.sql)
			}
			assertParensBalanced(t, res.Digest)

			// 参数范围与值对应校验，避免跨元组串联
			for i, p := range res.Params {
				if !(p.Start >= 0 && p.End > p.Start && p.End <= len(it.sql)) {
					t.Fatalf("[%s] param #%d invalid range: [%d,%d) of len=%d\nsql=\n%s",
						it.name, i+1, p.Start, p.End, len(it.sql), it.sql)
				}
				if it.sql[p.Start:p.End] != p.Value {
					t.Fatalf("[%s] param #%d value mismatch: slice=%q vs p.Value=%q\nsql=\n%s",
						it.name, i+1, it.sql[p.Start:p.End], p.Value, it.sql)
				}
				if strings.Contains(p.Value, "), (") {
					t.Fatalf("[%s] param spans tuple boundary: %q\nsql=\n%s", it.name, p.Value, it.sql)
				}
			}

			if testing.Verbose() {
				t.Logf("[%s]\nDialect: %v\nSQL:%s\nDigest:\n%s\nParams: %v\n", it.name, it.dialect, it.sql, res.Digest, res.Params)
			}
		})
	}
}

///* ============================ helpers ============================ */
//
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
//			t.Fatalf("digest has stray ')' (minBal=%d): %q", minBal, stmt)
//		}
//		if bal != 0 {
//			t.Fatalf("digest parens not balanced (bal=%d): %q", bal, stmt)
//		}
//	}
//}
