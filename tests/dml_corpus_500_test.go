package tests

import (
	"fmt"
	"strings"
	"testing"

	d "tsql_digest_v4/internal/sqldigest_antlr"
)

//go test -v -count=100 ./tests -run DML_Corpus_500

/*
  说明：
  - 本测试动态生成 500 条重度 DML 语句（PG/MySQL/MSSQL/Oracle 各 ~125）。
  - 每条都会过 BuildDigestANTLR，并做以下断言：
      * digest 非空
      * 括号配平（逐语句以分号分割）
      * 参数切片范围合法（与原 SQL 文本一致），且不跨 tuple 边界（"), ("）
  - Options：CollapseValuesInDigest=true（允许折叠 INSERT 多值），ParamizeTimeFuncs=true（把 NOW/SYSDATE 等作为参数）
*/

func Test_DML_Corpus_500(t *testing.T) {
	cases := generateDMLCorpus500()
	if len(cases) < 500 {
		t.Fatalf("generated %d cases, want >= 500", len(cases))
	}

	optBase := d.Options{
		CollapseValuesInDigest: true,
		ParamizeTimeFuncs:      true,
	}

	for i, c := range cases {
		opt := optBase
		opt.Dialect = c.dialect

		res, err := d.BuildDigestANTLR(c.sql, opt)
		if err != nil {
			t.Fatalf("[#%d %s] build error: %v\nsql=\n%s", i, c.name, err, c.sql)
		}
		if strings.TrimSpace(res.Digest) == "" {
			t.Fatalf("[#%d %s] empty digest\nsql=\n%s", i, c.name, c.sql)
		}
		assertParensBalanced(t, res.Digest)

		for pi, p := range res.Params {
			if !(p.Start >= 0 && p.End > p.Start && p.End <= len(c.sql)) {
				t.Fatalf("[#%d %s] param #%d invalid range: [%d,%d) len=%d\nsql=\n%s",
					i, c.name, pi+1, p.Start, p.End, len(c.sql), c.sql)
			}
			if c.sql[p.Start:p.End] != p.Value {
				t.Fatalf("[#%d %s] param #%d value mismatch: slice=%q vs p.Value=%q\nsql=\n%s",
					i, c.name, pi+1, c.sql[p.Start:p.End], p.Value, c.sql)
			}
			if strings.Contains(p.Value, "), (") {
				t.Fatalf("[#%d %s] param spans tuple boundary: %q\nsql=\n%s", i, c.name, p.Value, c.sql)
			}
		}

		if testing.Verbose() {
			t.Logf("[#%d %s]\nDialect: %v\nSQL   : %s\nDigest: %s\nParams: %v\n",
				i, c.name, c.dialect, oneLine(c.sql), res.Digest, res.Params)
		}
	}
}

/* ============================ 生成器 ============================ */

type caseEntry struct {
	name    string
	dialect d.Dialect
	sql     string
}

func generateDMLCorpus500() []caseEntry {
	var out []caseEntry

	// 种子各 16~20 条较复杂的语句（手工精挑），再扩展到 ~125/方言
	seedPG := seedDML_PG()
	seedMy := seedDML_MySQL()
	seedMS := seedDML_MSSQL()
	seedOR := seedDML_Oracle()

	out = append(out, expandPG(seedPG, 1250)...)
	out = append(out, expandMy(seedMy, 1250)...)
	out = append(out, expandMS(seedMS, 1250)...)
	out = append(out, expandOR(seedOR, 1250)...)

	return out
}

/* ---------------- PostgreSQL: 种子 + 扩展 ---------------- */

func seedDML_PG() []string {
	return []string{
		// INSERT … ON CONFLICT … DO UPDATE + RETURNING
		`WITH act AS (
  SELECT id FROM users WHERE status = 'active' AND created_at >= now() - INTERVAL '30 days'
)
INSERT INTO audit(user_id, action, at)
SELECT id, 'recheck', now() FROM act
ON CONFLICT (user_id, action) DO UPDATE
SET at = EXCLUDED.at
RETURNING user_id;`,

		// multi VALUES + RETURNING
		`INSERT INTO orders (id, uid, amt, note, created_at)
VALUES (DEFAULT, $1, $2, $3, now()),
       (DEFAULT, $4, $5, $6, now())
RETURNING id;`,

		// INSERT … SELECT with ON CONFLICT DO NOTHING
		`INSERT INTO dst (id, name)
SELECT s.id, s.name
FROM src s
WHERE NOT EXISTS (SELECT 1 FROM dst d WHERE d.id = s.id)
ON CONFLICT (id) DO NOTHING;`,

		// DELETE … RETURNING + archive
		`WITH mv AS (
  DELETE FROM sessions
  WHERE last_seen < now() - INTERVAL '90 days'
  RETURNING id, user_id, last_seen
)
INSERT INTO sessions_archive(id, user_id, last_seen, archived_at)
SELECT id, user_id, last_seen, now() FROM mv;`,

		// UPDATE … FROM + jsonb_set
		`UPDATE u
SET meta = jsonb_set(u.meta, '{flags,vip}', 'true', true),
    score = u.score + 10
FROM (
  SELECT id FROM users WHERE meta @> '{"country":"TW"}'
) t
WHERE u.id = t.id
RETURNING u.id;`,

		// UPDATE … FROM + regexp
		`UPDATE orders o
SET amt = o.amt * 1.05,
    updated_at = now()
FROM users u
WHERE o.user_id = u.id
  AND u.email ~* $1
RETURNING o.id;`,

		// DELETE … USING + NOT EXISTS
		`DELETE FROM cart_items ci
USING carts c
WHERE ci.cart_id = c.id
  AND c.user_id = $1
  AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.cart_id = c.id)
RETURNING ci.id;`,

		// DELETE 批次 with CTE LIMIT
		`DELETE FROM events e
USING (
  SELECT id FROM events WHERE ts < now() - INTERVAL '180 days' LIMIT 1000
) old
WHERE e.id = old.id;`,

		// UPSERT jsonb 合并
		`INSERT INTO t (id, payload)
VALUES ($1, jsonb_set('{}'::jsonb,'{x}',to_jsonb($2)))
ON CONFLICT (id) DO UPDATE SET payload = t.payload || EXCLUDED.payload;`,

		// UPDATE … FROM tx 子查询
		`UPDATE balances b
SET amount = b.amount - tx.amount
FROM (SELECT id, amount FROM tx WHERE id = $1) tx
WHERE b.user_id = $2;`,

		// DELETE … ANY/cidr
		`WITH s AS (
  SELECT id FROM users WHERE ip << $1::cidr
)
DELETE FROM sessions WHERE user_id IN (SELECT id FROM s) RETURNING id;`,

		// INSERT 聚合 + ON CONFLICT UPDATE
		`INSERT INTO agg (uid, total, updated_at)
SELECT uid, SUM(amt), now()
FROM orders WHERE ts >= $1
GROUP BY uid
ON CONFLICT (uid) DO UPDATE SET total = EXCLUDED.total, updated_at = EXCLUDED.updated_at;`,

		// UPDATE CASE 条件
		`UPDATE t SET tag = CASE WHEN note ILIKE '%promo%' THEN 'promo' ELSE tag END
WHERE id = ANY($1) RETURNING id;`,

		// DELETE USING users(banned)
		`DELETE FROM notes n
USING users u
WHERE n.uid = u.id AND u.status = 'banned'
RETURNING n.id;`,

		// INSERT with AT TIME ZONE
		`INSERT INTO timeline (uid, ts, msg)
SELECT $1, now() AT TIME ZONE 'UTC', CONCAT('hello-', $2)
ON CONFLICT DO NOTHING;`,
	}
}

func expandPG(seed []string, target int) []caseEntry {
	var out []caseEntry
	// 先放入种子
	for i, s := range seed {
		out = append(out, caseEntry{
			name:    fmt.Sprintf("PG_seed_%02d", i+1),
			dialect: d.Postgres,
			sql:     s,
		})
	}

	need := target - len(seed)
	if need <= 0 {
		return out
	}

	// 构造多种模板循环扩展
	for i := 0; i < need; i++ {
		switch i % 5 {
		case 0:
			// INSERT 多行 VALUES（行数 2~5），含 now()
			rows := 2 + (i % 4)
			sql := fmt.Sprintf(`INSERT INTO logs(uid, level, msg, created_at)
VALUES
  %s
RETURNING id;`, pgValuesRows(rows, i))
			out = append(out, caseEntry{
				name:    fmt.Sprintf("PG_values_%drows_%03d", rows, i),
				dialect: d.Postgres,
				sql:     sql,
			})
		case 1:
			// INSERT … SELECT + ON CONFLICT UPDATE
			sql := fmt.Sprintf(`INSERT INTO dst (id, name, note)
SELECT s.id, upper(s.name), concat('m-', %d)
FROM src s
WHERE s.k BETWEEN %d AND %d
ON CONFLICT (id) DO UPDATE SET note = EXCLUDED.note;`, i, i, i+100)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("PG_insert_select_upsert_%03d", i),
				dialect: d.Postgres,
				sql:     sql,
			})
		case 2:
			// UPDATE … FROM + generate_series
			sql := fmt.Sprintf(`WITH r AS (SELECT generate_series(%d, %d) AS id)
UPDATE t SET cnt = t.cnt + 1, updated_at = now()
FROM r WHERE t.id = r.id
RETURNING t.id;`, 1000+i, 1000+i+20)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("PG_update_from_series_%03d", i),
				dialect: d.Postgres,
				sql:     sql,
			})
		case 3:
			// DELETE … USING 子查询 + LIMIT
			sql := fmt.Sprintf(`WITH old AS (
  SELECT id FROM events WHERE ts < now() - INTERVAL '%d days' ORDER BY ts LIMIT %d
)
DELETE FROM events e USING old WHERE e.id = old.id
RETURNING e.id;`, 30+(i%60), 50+((i*7)%200))
			out = append(out, caseEntry{
				name:    fmt.Sprintf("PG_delete_batch_%03d", i),
				dialect: d.Postgres,
				sql:     sql,
			})
		default:
			// UPSERT jsonb merge
			sql := fmt.Sprintf(`INSERT INTO prefs(uid, js)
VALUES (%d, jsonb_build_object('k', %d))
ON CONFLICT (uid) DO UPDATE SET js = prefs.js || EXCLUDED.js;`, 100+i, i%9)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("PG_upsert_json_%03d", i),
				dialect: d.Postgres,
				sql:     sql,
			})
		}
	}
	return out
}

func pgValuesRows(n, base int) string {
	var b strings.Builder
	for j := 0; j < n; j++ {
		if j > 0 {
			b.WriteString(",\n  ")
		}
		fmt.Fprintf(&b, "(%d, %d, 'm-%d', now())", 10+(base+j)%100, 1+((base+j)%5), base+j)
	}
	return b.String()
}

/* ---------------- MySQL: 种子 + 扩展 ---------------- */

func seedDML_MySQL() []string {
	return []string{
		// UPDATE with CTE + JOIN
		`WITH act AS (
  SELECT id FROM users WHERE flag = 1 AND started_at >= NOW() - INTERVAL 30 DAY
)
UPDATE sessions s
JOIN act a ON s.user_id = a.id
SET s.ttl = DATE_ADD(NOW(), INTERVAL 7 DAY)
WHERE s.status = 'open';`,

		// INSERT multi VALUES
		`INSERT INTO orders (uid, amt, note, created_at)
VALUES (?, ?, '首单', NOW()),
       (?, ?, CONCAT('促销-', ?), NOW());`,

		// INSERT … SELECT + ON DUP KEY UPDATE
		`INSERT INTO dst (id, name)
SELECT s.id, s.name FROM src s
WHERE NOT EXISTS (SELECT 1 FROM dst d WHERE d.id = s.id)
ON DUPLICATE KEY UPDATE name = VALUES(name);`,

		// UPDATE join + JSON_TABLE 提取
		`UPDATE users u
JOIN JSON_TABLE(u.profile, '$.flags'
  COLUMNS(vip BOOL PATH '$.vip', ban BOOL PATH '$.ban')) jt
SET u.is_vip = jt.vip, u.is_ban = jt.ban
WHERE u.id = ?;`,

		// DELETE multi-table with LEFT JOIN
		`DELETE a FROM cart_items a
JOIN carts c ON a.cart_id = c.id
LEFT JOIN orders o ON o.cart_id = c.id
WHERE c.user_id = ? AND o.id IS NULL;`,

		// UPDATE join + regexp
		`UPDATE orders o
JOIN users u ON u.id = o.user_id
SET o.amt = o.amt * 1.05, o.updated_at = NOW()
WHERE u.email REGEXP ?;`,

		// INSERT audit + DUP KEY UPDATE
		`INSERT INTO audit (user_id, action, at)
SELECT id, 'recheck', NOW()
FROM users WHERE JSON_EXTRACT(meta,'$.country') = 'TW'
ON DUPLICATE KEY UPDATE at = VALUES(at);`,

		// REPLACE
		`REPLACE INTO kv (k, v) VALUES (?, JSON_SET('{}', '$.x', ?));`,

		// DELETE with CTE
		`WITH r AS (SELECT id FROM users WHERE flag=1)
DELETE t FROM tokens t JOIN r ON r.id = t.user_id;`,

		// UPDATE with scalar subselect
		`UPDATE t
JOIN (SELECT ? AS k, ? AS v) s
ON t.k = s.k
SET t.v = s.v;`,
	}
}

func expandMy(seed []string, target int) []caseEntry {
	var out []caseEntry
	for i, s := range seed {
		out = append(out, caseEntry{
			name:    fmt.Sprintf("My_seed_%02d", i+1),
			dialect: d.MySQL,
			sql:     s,
		})
	}
	need := target - len(seed)
	if need <= 0 {
		return out
	}
	for i := 0; i < need; i++ {
		switch i % 5 {
		case 0:
			// INSERT 多行 + ON DUPLICATE KEY
			rows := 2 + (i % 4)
			sql := fmt.Sprintf(`INSERT INTO logs(uid, lvl, msg, created_at)
VALUES
  %s
ON DUPLICATE KEY UPDATE msg = VALUES(msg);`, myValuesRows(rows, i))
			out = append(out, caseEntry{
				name:    fmt.Sprintf("My_values_dup_%drows_%03d", rows, i),
				dialect: d.MySQL,
				sql:     sql,
			})
		case 1:
			// UPDATE JOIN + JSON_SET
			sql := fmt.Sprintf(`UPDATE profile p
JOIN users u ON u.id = p.uid
SET p.js = JSON_SET(p.js, '$.touch', NOW()), p.ver = p.ver + 1
WHERE u.flag = %d;`, i%3)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("My_update_json_%03d", i),
				dialect: d.MySQL,
				sql:     sql,
			})
		case 2:
			// DELETE 多表 + 条件
			sql := fmt.Sprintf(`DELETE n FROM notes n
LEFT JOIN users u ON u.id = n.uid
WHERE u.status = 'banned' AND n.id %% %d = 0;`, 7+(i%5))
			out = append(out, caseEntry{
				name:    fmt.Sprintf("My_delete_join_%03d", i),
				dialect: d.MySQL,
				sql:     sql,
			})
		case 3:
			// INSERT … SELECT
			sql := fmt.Sprintf(`INSERT INTO dst (id, name, tag)
SELECT s.id, UPPER(s.name), CONCAT('m-', %d) FROM src s
WHERE s.k BETWEEN %d AND %d;`, i, i, i+200)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("My_insert_select_%03d", i),
				dialect: d.MySQL,
				sql:     sql,
			})
		default:
			// REPLACE + JSON_MERGE_PATCH
			sql := fmt.Sprintf(`REPLACE INTO prefs(uid, js)
VALUES (%d, JSON_MERGE_PATCH(COALESCE((SELECT js FROM prefs WHERE uid=%d),'{}'), '{"mark":true}'));`, 100+i, 100+i)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("My_replace_json_%03d", i),
				dialect: d.MySQL,
				sql:     sql,
			})
		}
	}
	return out
}

func myValuesRows(n, base int) string {
	var b strings.Builder
	for j := 0; j < n; j++ {
		if j > 0 {
			b.WriteString(",\n  ")
		}
		fmt.Fprintf(&b, "(%d, %d, CONCAT('m-', %d), NOW())", 10+(base+j)%100, 1+((base+j)%5), base+j)
	}
	return b.String()
}

/* ---------------- SQL Server: 种子 + 扩展 ---------------- */

func seedDML_MSSQL() []string {
	return []string{
		// UPDATE with CTE + JOIN
		`WITH act AS (
  SELECT Id FROM dbo.Users WHERE Flag = 1 AND StartedAt >= DATEADD(day, -30, SYSUTCDATETIME())
)
UPDATE s
SET s.TTL = DATEADD(day, 7, SYSUTCDATETIME())
FROM dbo.Sessions s
JOIN act a ON s.UserId = a.Id
WHERE s.Status = 'open';`,

		// INSERT multi VALUES + OUTPUT
		`INSERT INTO dbo.Orders (UserId, Amount, Note, CreatedAt)
OUTPUT INSERTED.Id
VALUES (@u1, @a1, N'首单', SYSUTCDATETIME()),
       (@u2, @a2, N'促销-'+CAST(@x AS nvarchar(50)), SYSUTCDATETIME());`,

		// MERGE 基本
		`MERGE INTO dbo.Dst AS d
USING (SELECT @id AS Id, @name AS Name) AS s
ON (d.Id = s.Id)
WHEN MATCHED THEN UPDATE SET d.Name = s.Name
WHEN NOT MATCHED THEN INSERT (Id, Name) VALUES (s.Id, s.Name)
OUTPUT $action, inserted.Id;`,

		// UPDATE JSON_VALUE
		`UPDATE u
SET u.IsVip = JSON_VALUE(u.Profile,'$.vip'),
    u.IsBan = JSON_VALUE(u.Profile,'$.ban')
FROM dbo.Users u
WHERE u.Id = @id;`,

		// DELETE … JOIN
		`DELETE a
FROM dbo.CartItems a
JOIN dbo.Carts c ON a.CartId=c.Id
LEFT JOIN dbo.Orders o ON o.CartId=c.Id
WHERE c.UserId=@uid AND o.Id IS NULL;`,
	}
}

func expandMS(seed []string, target int) []caseEntry {
	var out []caseEntry
	for i, s := range seed {
		out = append(out, caseEntry{
			name:    fmt.Sprintf("MS_seed_%02d", i+1),
			dialect: d.SQLServer,
			sql:     s,
		})
	}
	need := target - len(seed)
	if need <= 0 {
		return out
	}
	for i := 0; i < need; i++ {
		switch i % 5 {
		case 0:
			// MERGE 变体（累加或插入）
			sql := fmt.Sprintf(`MERGE dbo.Bal AS b
USING (SELECT %d AS Uid, %d AS Amt) s
ON (b.Uid=s.Uid)
WHEN MATCHED THEN UPDATE SET b.Amount = b.Amount + s.Amt
WHEN NOT MATCHED THEN INSERT (Uid, Amount) VALUES (s.Uid, s.Amt)
OUTPUT $action, inserted.Uid;`, 100+i, 5+(i%20))
			out = append(out, caseEntry{
				name:    fmt.Sprintf("MS_merge_bal_%03d", i),
				dialect: d.SQLServer,
				sql:     sql,
			})
		case 1:
			// UPDATE … JOIN + 正则 LIKE
			sql := fmt.Sprintf(`UPDATE o
SET o.Amount = o.Amount * 1.05, o.UpdatedAt = SYSUTCDATETIME()
FROM dbo.Orders o
JOIN dbo.Users u ON u.Id = o.UserId
WHERE u.Email LIKE '%%%d%%';`, i)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("MS_update_join_%03d", i),
				dialect: d.SQLServer,
				sql:     sql,
			})
		case 2:
			// DELETE TOP 批次
			sql := fmt.Sprintf(`DELETE TOP (%d) FROM dbo.Events WHERE Ts < DATEADD(day, -%d, SYSUTCDATETIME());`, 100+((i*7)%500), 30+(i%90))
			out = append(out, caseEntry{
				name:    fmt.Sprintf("MS_delete_top_%03d", i),
				dialect: d.SQLServer,
				sql:     sql,
			})
		case 3:
			// INSERT … SELECT + OUTPUT
			sql := fmt.Sprintf(`INSERT INTO dbo.Dst (Id, Name, Tag)
OUTPUT INSERTED.Id
SELECT s.Id, UPPER(s.Name), CONCAT(N'm-', %d) FROM dbo.Src s
WHERE s.K BETWEEN %d AND %d;`, i, i, i+300)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("MS_insert_select_%03d", i),
				dialect: d.SQLServer,
				sql:     sql,
			})
		default:
			// UPDATE JSON_MODIFY
			sql := fmt.Sprintf(`UPDATE p
SET p.Json = JSON_MODIFY(p.Json, '$.touch', SYSUTCDATETIME()), p.Ver = p.Ver + 1
FROM dbo.Profile p
WHERE p.Uid = %d;`, 1000+i)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("MS_update_json_%03d", i),
				dialect: d.SQLServer,
				sql:     sql,
			})
		}
	}
	return out
}

/* ---------------- Oracle: 种子 + 扩展 ---------------- */

func seedDML_Oracle() []string {
	return []string{
		// MERGE
		`MERGE INTO dst d
USING (SELECT :id AS id, :name AS name FROM dual) s
ON (d.id = s.id)
WHEN MATCHED THEN UPDATE SET d.name = s.name
WHEN NOT MATCHED THEN INSERT (id, name) VALUES (s.id, s.name);`,

		// INSERT multi VALUES（注意 Oracle 也支持多 values, 12c 以上）
		`INSERT INTO orders (id, uid, amt, note, created_at)
VALUES (orders_seq.NEXTVAL, :u1, :a1, '首单', SYSTIMESTAMP),
       (orders_seq.NEXTVAL, :u2, :a2, '促销-'||:s, SYSTIMESTAMP);`,

		// INSERT … SELECT
		`INSERT INTO dst (id, name)
SELECT id, UPPER(name) FROM src
WHERE NOT EXISTS (SELECT 1 FROM dst d WHERE d.id = src.id);`,

		// UPDATE JSON_VALUE
		`UPDATE users
SET is_vip = JSON_VALUE(profile, '$.vip'),
    is_ban = JSON_VALUE(profile, '$.ban')
WHERE id = :id;`,

		// DELETE 相关子查询
		`DELETE FROM cart_items a
WHERE EXISTS (
  SELECT 1 FROM carts c
  WHERE c.id = a.cart_id AND c.user_id = :uid
    AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.cart_id = c.id)
);`,
	}
}

func expandOR(seed []string, target int) []caseEntry {
	var out []caseEntry
	for i, s := range seed {
		out = append(out, caseEntry{
			name:    fmt.Sprintf("OR_seed_%02d", i+1),
			dialect: d.Oracle,
			sql:     s,
		})
	}
	need := target - len(seed)
	if need <= 0 {
		return out
	}
	for i := 0; i < need; i++ {
		switch i % 5 {
		case 0:
			// MERGE 累加
			sql := fmt.Sprintf(`MERGE INTO bal b
USING (SELECT %d AS uid, %d AS amt FROM dual) s
ON (b.uid = s.uid)
WHEN MATCHED THEN UPDATE SET b.amount = b.amount + s.amt
WHEN NOT MATCHED THEN INSERT (uid, amount) VALUES (s.uid, s.amt);`, 200+i, 3+(i%15))
			out = append(out, caseEntry{
				name:    fmt.Sprintf("OR_merge_bal_%03d", i),
				dialect: d.Oracle,
				sql:     sql,
			})
		case 1:
			// UPDATE 正则 + 时间
			sql := fmt.Sprintf(`UPDATE orders o
SET o.amt = o.amt * 1.05, o.updated_at = SYSTIMESTAMP
WHERE EXISTS (SELECT 1 FROM users u WHERE u.id = o.user_id AND REGEXP_LIKE(u.email, '%d'));`, i)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("OR_update_regex_%03d", i),
				dialect: d.Oracle,
				sql:     sql,
			})
		case 2:
			// DELETE 批次（ROWNUM）
			sql := fmt.Sprintf(`DELETE FROM events WHERE ROWNUM <= %d AND ts < DATE '2024-01-01' + %d;`, 50+((i*13)%300), (i % 365))
			out = append(out, caseEntry{
				name:    fmt.Sprintf("OR_delete_rownum_%03d", i),
				dialect: d.Oracle,
				sql:     sql,
			})
		case 3:
			// INSERT … SELECT + q'[]' 字面串
			sql := fmt.Sprintf(`INSERT /*+ APPEND */ INTO notes(id, uid, note, created_at)
SELECT notes_seq.NEXTVAL, u.id, q'[批次-%d]', SYSTIMESTAMP
FROM users u WHERE u.flag = MOD(%d, 3);`, i, i)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("OR_insert_select_%03d", i),
				dialect: d.Oracle,
				sql:     sql,
			})
		default:
			// UPDATE JSON_MERGEPATCH
			sql := fmt.Sprintf(`UPDATE prefs p SET p.js = JSON_MERGEPATCH(p.js, '{"touch":true}')
WHERE p.uid = %d`, 3000+i)
			out = append(out, caseEntry{
				name:    fmt.Sprintf("OR_update_json_%03d", i),
				dialect: d.Oracle,
				sql:     sql,
			})
		}
	}
	return out
}

/* ============================ 工具函数 & 断言 ============================ */

//func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
//
//// 括号配平：逐语句（以 ; 分割），允许合法以 ')' 结尾
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

/* ============================ 说明 ============================

为什么用“生成器”而不是手写 500 条？
- 便于稳定跑通、快速扩容/缩容数量，以及控制多样性（多值 INSERT/UPSET/DELETE 批次/UPDATE JOIN/JSON 操作等）。
- 生成的 SQL 在语法风格上更统一，更适合你的签名与参数抽取冒烟。

需要更“奇葩”的 DML？比如：
- PG: INSERT … ON CONFLICT … WHERE 子句、DELETE … RETURNING JSON 构造、UPDATE … FROM LATERAL
- MySQL: INSERT … ON DUP KEY with PARTITION、UPDATE IGNORE、DELETE QUICK、REPLACE SELECT
- MSSQL: MERGE with HOLDLOCK、DELETE OUTPUT DELETED、UPDATE with WITH (INDEX=…)
- Oracle: INSERT ALL/ FIRST、MERGE LOG ERRORS、UPDATE RETURNING INTO
都可以在此生成器里按模式继续扩展。
*/
