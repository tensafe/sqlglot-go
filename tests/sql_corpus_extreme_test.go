package tests

import (
	"strings"
	"testing"

	d "github.com/tensafe/sqlglot-go/internal/sqldigest_antlr"
)

type caseItem struct {
	name    string
	dialect d.Dialect
	sql     string
	opt     d.Options // 可覆写；为空则用默认
}

func Test_Corpus_Extreme_Mixed(t *testing.T) {
	cases := []caseItem{
		// ---------- PostgreSQL ----------
		{
			"PG_jsonb_path_query",
			d.Postgres,
			`SELECT jsonb_path_query(js, '$.store.book[*] ? (@.price > 10)') FROM t WHERE meta @> '{"type":"book"}'`,
			d.Options{Dialect: d.Postgres},
		},
		{
			"PG_arrays_custom_operator_and_overlap",
			d.Postgres,
			`SELECT ARRAY[1,2,3] OPERATOR(pg_catalog.&&) ARRAY[3,4], (ts1, ts1 + INTERVAL '1 hour') OVERLAPS (ts2, ts2 + INTERVAL '30 minutes') FROM t`,
			d.Options{Dialect: d.Postgres},
		},
		{
			"PG_at_time_zone_chain_window_exclude",
			d.Postgres,
			`SELECT id,
			        (now() AT TIME ZONE 'UTC') AT TIME ZONE 'Asia/Taipei' AS local_ts,
			        SUM(v) OVER (PARTITION BY k ORDER BY ts
			                     ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW EXCLUDE TIES) AS rsum
			 FROM t WHERE ts >= TIMESTAMP '2024-01-01 00:00:00'`,
			d.Options{Dialect: d.Postgres, CollapseValuesInDigest: true},
		},
		{
			"PG_jsonb_set_path_ops",
			d.Postgres,
			`UPDATE t SET js = jsonb_set(js, '{a,0}', '"X"', true) WHERE js @> '{"a":[1]}'`,
			d.Options{Dialect: d.Postgres},
		},

		// ---------- MySQL 8 ----------
		{
			"MySQL_JSON_TABLE",
			d.MySQL,
			`SELECT jt.title, jt.price
			   FROM src,
			        JSON_TABLE(src.js, '$.store.book[*]'
			          COLUMNS ( title VARCHAR(100) PATH '$.title',
			                    price DECIMAL(10,2) PATH '$.price' )) AS jt
			  WHERE jt.price > 9.99`,
			d.Options{Dialect: d.MySQL},
		},
		{
			"MySQL_partition_generated_virtual",
			d.MySQL,
			`CREATE TABLE pt (
			   id INT,
			   dt DATE,
			   amt DECIMAL(10,2),
			   y INT AS (YEAR(dt)) VIRTUAL
			 ) PARTITION BY RANGE (y) (
			   PARTITION p2024 VALUES LESS THAN (2025),
			   PARTITION pmax  VALUES LESS THAN MAXVALUE
			 )`,
			d.Options{Dialect: d.MySQL},
		},
		{
			"MySQL_index_hints_optimizer_hints",
			d.MySQL,
			`SELECT /*+ SET_VAR(optimizer_switch='index_merge=on') */
			        /*+ SET_VAR(sort_buffer_size=262144) */
			        * FROM t USE INDEX FOR JOIN (idx_a) FORCE INDEX FOR ORDER BY (idx_a)
			 WHERE a > 10 ORDER BY a`,
			d.Options{Dialect: d.MySQL},
		},
		{
			"MySQL_window_range_interval",
			d.MySQL,
			`SELECT id,
			        SUM(v) OVER (PARTITION BY k ORDER BY ts
			            RANGE BETWEEN INTERVAL 1 DAY PRECEDING AND CURRENT ROW) AS rsum
			 FROM t WHERE ts >= '2024-01-01'`,
			d.Options{Dialect: d.MySQL},
		},

		// ---------- SQL Server ----------
		{
			"MSSQL_OPENJSON_cross_apply",
			d.SQLServer,
			`SELECT j.id, j.name, tag.value AS tag
			   FROM dbo.T t
			   CROSS APPLY OPENJSON(t.js, '$.items')
			        WITH ( id INT '$.id', name NVARCHAR(100) '$.name', tags NVARCHAR(MAX) AS JSON ) AS j
			   CROSS APPLY OPENJSON(j.tags) AS tag`,
			d.Options{Dialect: d.SQLServer},
		},
		{
			"MSSQL_window_rows_between",
			d.SQLServer,
			`SELECT id,
			        SUM(v) OVER (PARTITION BY k ORDER BY ts ROWS BETWEEN 1 PRECEDING AND 1 FOLLOWING) AS rsum
			   FROM t WHERE ts >= '2024-01-01'`,
			d.Options{Dialect: d.SQLServer},
		},
		{
			"MSSQL_at_time_zone",
			d.SQLServer,
			`SELECT (SYSDATETIMEOFFSET() AT TIME ZONE 'UTC') AT TIME ZONE 'Taipei Standard Time' AS local_ts`,
			d.Options{Dialect: d.SQLServer},
		},
		{
			"MSSQL_json_value_query",
			d.SQLServer,
			`SELECT JSON_VALUE(js, '$.a') AS a, JSON_QUERY(js, '$.b') AS b FROM t WHERE JSON_VALUE(js,'$.a') = 'x'`,
			d.Options{Dialect: d.SQLServer},
		},

		// ---------- Oracle ----------
		{
			"Oracle_JSON_value_exists",
			d.Oracle,
			`SELECT JSON_VALUE(js, '$.a') AS a
			   FROM t
			  WHERE JSON_EXISTS(js, '$.a?(@ > 1)')`,
			d.Options{Dialect: d.Oracle},
		},
		{
			"Oracle_connect_by",
			d.Oracle,
			`SELECT id, parent_id, LEVEL AS lv FROM tree START WITH parent_id IS NULL CONNECT BY PRIOR id = parent_id`,
			d.Options{Dialect: d.Oracle},
		},
		{
			"Oracle_match_recognize",
			d.Oracle,
			`SELECT * FROM sales
			  MATCH_RECOGNIZE (
			    PARTITION BY prod
			    ORDER BY tstamp
			    MEASURES FIRST(A.tstamp) AS start_t, LAST(A.tstamp) AS end_t
			    PATTERN (A+)
			    DEFINE  A AS A.amount > 100
			  )`,
			d.Options{Dialect: d.Oracle},
		},
		{
			"Oracle_window_range_interval",
			d.Oracle,
			`SELECT id,
			        SUM(v) OVER (PARTITION BY k ORDER BY ts
			           RANGE BETWEEN INTERVAL '1' DAY PRECEDING AND CURRENT ROW) AS rsum
			   FROM t WHERE ts >= DATE '2024-01-01'`,
			d.Options{Dialect: d.Oracle},
		},

		// ---------- Extra mixed/edge ----------
		{
			"PG_jsonb_and_arrays_mashup",
			d.Postgres,
			`SELECT jsonb_path_query(js, '$.items[*] ? (@.qty > 0)') AS it,
			        ARRAY[1,2] OPERATOR(pg_catalog.@>) ARRAY[1] AS arr_superset`,
			d.Options{Dialect: d.Postgres},
		},
		{
			"MySQL_JSON_TABLE_join_update",
			d.MySQL,
			`UPDATE t
			    JOIN JSON_TABLE(t.js, '$.users[*]' COLUMNS(uid INT PATH '$.id', nick VARCHAR(50) PATH '$.name')) jt
			       ON t.uid = jt.uid
			   SET t.nick = jt.nick
			 WHERE t.status = 'active'`,
			d.Options{Dialect: d.MySQL},
		},
		{
			"MSSQL_openjson_with_schema_nested",
			d.SQLServer,
			`SELECT u.id, p2.value AS phone
			   FROM users u
			   CROSS APPLY OPENJSON(u.profile)
			        WITH ( phones NVARCHAR(MAX) AS JSON ) as j
			   CROSS APPLY OPENJSON(j.phones) AS p2`,
			d.Options{Dialect: d.SQLServer},
		},
		{
			"Oracle_json_and_window",
			d.Oracle,
			`SELECT JSON_VALUE(js, '$.name') AS name,
			        AVG(amt) OVER (PARTITION BY JSON_VALUE(js,'$.group') ORDER BY ts
			           ROWS BETWEEN 2 PRECEDING AND CURRENT ROW) AS avg_amt
			   FROM t WHERE JSON_EXISTS(js, '$.name')`,
			d.Options{Dialect: d.Oracle},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			opt := tc.opt
			var zero d.Dialect
			if opt.Dialect == zero {
				opt = d.Options{Dialect: tc.dialect}
			}
			res, err := d.BuildDigestANTLR(tc.sql, opt)
			if err != nil {
				t.Fatalf("[%s] build error: %v\nsql=%s", tc.name, err, tc.sql)
			}
			// 基本健康检查
			if strings.TrimSpace(res.Digest) == "" {
				t.Fatalf("[%s] empty digest\nsql=%s", tc.name, tc.sql)
			}
			//// 不应以多余右括号结尾
			//if strings.HasSuffix(strings.TrimSpace(res.Digest), ")") {
			//	t.Fatalf("[%s] digest ends with stray ')': %q\nsql=%s", tc.name, res.Digest, tc.sql)
			//}
			strings.HasSuffix(strings.TrimSpace(res.Digest), ")")
			// 参数切片位置有效且不跨 tuple 边界
			for i, p := range res.Params {
				if !(p.Start >= 0 && p.End > p.Start && p.End <= len(tc.sql)) {
					t.Fatalf("[%s] param #%d invalid range: [%d,%d) len=%d\nsql=%s",
						tc.name, i+1, p.Start, p.End, len(tc.sql), tc.sql)
				}
				if tc.sql[p.Start:p.End] != p.Value {
					t.Fatalf("[%s] param #%d value mismatch: slice=%q vs p.Value=%q\nsql=%s",
						tc.name, i+1, tc.sql[p.Start:p.End], p.Value, tc.sql)
				}
				if strings.Contains(p.Value, "), (") {
					t.Fatalf("[%s] param spans tuple boundary: %q\nsql=%s", tc.name, p.Value, tc.sql)
				}
			}
			if testing.Verbose() {
				t.Logf("[%s]\nDialect: %v\nSQL   : %s\nDigest: %s\nParams: %v\n",
					tc.name, tc.dialect, oneLine(tc.sql), res.Digest, res.Params)
			}
		})
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// 在 tests 公共 helper 里加：
func assertParensBalanced(t *testing.T, digest string) {
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
		// 任何位置出现负值 => 存在“多余右括号”
		if minBal < 0 {
			t.Fatalf("digest has stray ')' (minBal=%d): %q", minBal, stmt)
		}
		// 结束时余额必须为 0（配平）。允许以 ')' 结尾，因为可能正好配平。
		if bal != 0 {
			t.Fatalf("digest parens not balanced (bal=%d): %q", bal, stmt)
		}
	}
}
