package tests

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	d "tsql_digest_v4/internal/sqldigest_antlr"
)

//go test -v -count=1 . -run DML_Corpus_500_Plus
//# 可设置随机种子复现实验：
//DML_FUZZ_SEED=20240823 go test -v -count=1 . -run DML_Corpus_500_Plus

/*
 单文件说明：
 1) 生成四大方言（PG/MySQL/MSSQL/Oracle）各 ≥125 条复杂 DML（总数 ≥500），包含 INSERT/UPDATE/DELETE/MERGE/UPSERT 等。
 2) 自动注入“稳定性种子（edge cases）”，再做可控随机轻度 fuzz（注释/换行/逗号后断行等），默认固定随机种子 1337。
    可用环境变量 DML_FUZZ_SEED 覆盖以复现实验。
 3) 校验点：
    - digest 非空
    - 逐语句括号配平（对 digest 而非原 SQL）
    - 每个参数的 [Start,End) 切片和原 SQL 文本严格一致
    - 禁止参数穿越 tuple 边界（不包含 "), ("）
    - Verbose 模式下会打印每条的摘要
*/

func Test_DML_Corpus_500_Plus(t *testing.T) {
	cases := generateDMLCorpus500Plus()
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
		assertParensBalancede(t, res.Digest)

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
				t.Fatalf("[#%d %s] param spans tuple boundary: %q\nsql=\n%s",
					i, c.name, p.Value, c.sql)
			}
		}

		if testing.Verbose() {
			t.Logf("[#%d %s]\nDialect: %v\nSQL   : %s\nDigest: %s\nParams: %v\n",
				i, c.name, c.dialect, oneLinee(c.sql), res.Digest, res.Params)
		}
	}
}

/* ============================ 生成器（含稳定性种子 & 轻度 fuzz） ============================ */

type caseEntrye struct {
	name    string
	dialect d.Dialect
	sql     string
}

func generateDMLCorpus500Plus() []caseEntrye {
	r := randFromEnv()

	var out []caseEntrye

	// 原始种子
	seedPG := seedDML_PGe()
	seedMy := seedDML_MySQLe()
	seedMS := seedDML_MSSQLe()
	seedOR := seedDML_Oraclee()

	// 稳定性种子（Edge Pack）
	seedPG = append(seedPG, pgEdgeSQL...)
	seedMy = append(seedMy, myEdgeSQL...)
	seedMS = append(seedMS, msEdgeSQL...)
	seedOR = append(seedOR, orEdgeSQL...)

	// 目标覆盖：每方言 ≥125 条
	targetPG := maxInt(125, len(seedPG))
	targetMy := maxInt(125, len(seedMy))
	targetMS := maxInt(125, len(seedMS))
	targetOR := maxInt(125, len(seedOR))

	out = append(out, expandPGWithFuzz(seedPG, targetPG, r)...)
	out = append(out, expandMyWithFuzz(seedMy, targetMy, r)...)
	out = append(out, expandMSWithFuzz(seedMS, targetMS, r)...)
	out = append(out, expandORWithFuzz(seedOR, targetOR, r)...)

	return out
}

/* ---------------- PostgreSQL: 种子 + 扩展 + fuzz ---------------- */

func seedDML_PGe() []string {
	return []string{
		`WITH act AS (
  SELECT id FROM users WHERE status = 'active' AND created_at >= now() - INTERVAL '30 days'
)
INSERT INTO audit(user_id, action, at)
SELECT id, 'recheck', now() FROM act
ON CONFLICT (user_id, action) DO UPDATE
SET at = EXCLUDED.at
RETURNING user_id;`,

		`INSERT INTO orders (id, uid, amt, note, created_at)
VALUES (DEFAULT, $1, $2, $3, now()),
       (DEFAULT, $4, $5, $6, now())
RETURNING id;`,

		`INSERT INTO dst (id, name)
SELECT s.id, s.name
FROM src s
WHERE NOT EXISTS (SELECT 1 FROM dst d WHERE d.id = s.id)
ON CONFLICT (id) DO NOTHING;`,

		`WITH mv AS (
  DELETE FROM sessions
  WHERE last_seen < now() - INTERVAL '90 days'
  RETURNING id, user_id, last_seen
)
INSERT INTO sessions_archive(id, user_id, last_seen, archived_at)
SELECT id, user_id, last_seen, now() FROM mv;`,

		`UPDATE u
SET meta = jsonb_set(u.meta, '{flags,vip}', 'true', true),
    score = u.score + 10
FROM (
  SELECT id FROM users WHERE meta @> '{"country":"TW"}'
) t
WHERE u.id = t.id
RETURNING u.id;`,

		`UPDATE orders o
SET amt = o.amt * 1.05,
    updated_at = now()
FROM users u
WHERE o.user_id = u.id
  AND u.email ~* $1
RETURNING o.id;`,

		`DELETE FROM cart_items ci
USING carts c
WHERE ci.cart_id = c.id
  AND c.user_id = $1
  AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.cart_id = c.id)
RETURNING ci.id;`,

		`DELETE FROM events e
USING (
  SELECT id FROM events WHERE ts < now() - INTERVAL '180 days' LIMIT 1000
) old
WHERE e.id = old.id;`,

		`INSERT INTO t (id, payload)
VALUES ($1, jsonb_set('{}'::jsonb,'{x}',to_jsonb($2)))
ON CONFLICT (id) DO UPDATE SET payload = t.payload || EXCLUDED.payload;`,

		`UPDATE balances b
SET amount = b.amount - tx.amount
FROM (SELECT id, amount FROM tx WHERE id = $1) tx
WHERE b.user_id = $2;`,

		`WITH s AS (SELECT id FROM users WHERE ip << $1::cidr)
DELETE FROM sessions WHERE user_id IN (SELECT id FROM s) RETURNING id;`,

		`INSERT INTO agg (uid, total, updated_at)
SELECT uid, SUM(amt), now()
FROM orders WHERE ts >= $1
GROUP BY uid
ON CONFLICT (uid) DO UPDATE SET total = EXCLUDED.total, updated_at = EXCLUDED.updated_at;`,

		`UPDATE t SET tag = CASE WHEN note ILIKE '%promo%' THEN 'promo' ELSE tag END
WHERE id = ANY($1) RETURNING id;`,

		`DELETE FROM notes n
USING users u
WHERE n.uid = u.id AND u.status = 'banned'
RETURNING n.id;`,

		`INSERT INTO timeline (uid, ts, msg)
SELECT $1, now() AT TIME ZONE 'UTC', CONCAT('hello-', $2)
ON CONFLICT DO NOTHING;`,
	}
}

func expandPGWithFuzz(seed []string, target int, r *rand.Rand) []caseEntrye {
	var out []caseEntrye
	for i, s := range seed {
		out = append(out, caseEntrye{
			name:    fmt.Sprintf("PG_seed_%02d", i+1),
			dialect: d.Postgres,
			sql:     pgFuzz(s, r),
		})
	}
	need := target - len(seed)
	if need <= 0 {
		return out
	}
	for i := 0; i < need; i++ {
		var sql string
		switch i % 5 {
		case 0:
			rows := 2 + (i % 4)
			sql = fmt.Sprintf(`INSERT INTO logs(uid, level, msg, created_at)
VALUES
  %s
RETURNING id;`, pgValuesRows(rows, i))
		case 1:
			sql = fmt.Sprintf(`INSERT INTO dst (id, name, note)
SELECT s.id, upper(s.name), concat('m-', %d)
FROM src s
WHERE s.k BETWEEN %d AND %d
ON CONFLICT (id) DO UPDATE SET note = EXCLUDED.note;`, i, i, i+100)
		case 2:
			sql = fmt.Sprintf(`WITH r AS (SELECT generate_series(%d, %d) AS id)
UPDATE t SET cnt = t.cnt + 1, updated_at = now()
FROM r WHERE t.id = r.id
RETURNING t.id;`, 1000+i, 1000+i+20)
		case 3:
			sql = fmt.Sprintf(`WITH old AS (
  SELECT id FROM events WHERE ts < now() - INTERVAL '%d days' ORDER BY ts LIMIT %d
)
DELETE FROM events e USING old WHERE e.id = old.id
RETURNING e.id;`, 30+(i%60), 50+((i*7)%200))
		default:
			sql = fmt.Sprintf(`INSERT INTO prefs(uid, js)
VALUES (%d, jsonb_build_object('k', %d))
ON CONFLICT (uid) DO UPDATE SET js = prefs.js || EXCLUDED.js;`, 100+i, i%9)
		}
		out = append(out, caseEntrye{
			name:    fmt.Sprintf("PG_auto_%03d", i),
			dialect: d.Postgres,
			sql:     pgFuzz(sql, r),
		})
	}
	return out
}

func pgValuesRowse(n, base int) string {
	var b strings.Builder
	for j := 0; j < n; j++ {
		if j > 0 {
			b.WriteString(",\n  ")
		}
		fmt.Fprintf(&b, "(%d, %d, 'm-%d', now())", 10+(base+j)%100, 1+((base+j)%5), base+j)
	}
	return b.String()
}

/* ---------------- MySQL: 种子 + 扩展 + fuzz ---------------- */

func seedDML_MySQLe() []string {
	return []string{
		`WITH act AS (
  SELECT id FROM users WHERE flag = 1 AND started_at >= NOW() - INTERVAL 30 DAY
)
UPDATE sessions s
JOIN act a ON s.user_id = a.id
SET s.ttl = DATE_ADD(NOW(), INTERVAL 7 DAY)
WHERE s.status = 'open';`,

		`INSERT INTO orders (uid, amt, note, created_at)
VALUES (?, ?, '首单', NOW()),
       (?, ?, CONCAT('促销-', ?), NOW());`,

		`INSERT INTO dst (id, name)
SELECT s.id, s.name FROM src s
WHERE NOT EXISTS (SELECT 1 FROM dst d WHERE d.id = s.id)
ON DUPLICATE KEY UPDATE name = VALUES(name);`,

		`UPDATE users u
JOIN JSON_TABLE(u.profile, '$.flags'
  COLUMNS(vip BOOL PATH '$.vip', ban BOOL PATH '$.ban')) jt
SET u.is_vip = jt.vip, u.is_ban = jt.ban
WHERE u.id = ?;`,

		`DELETE a FROM cart_items a
JOIN carts c ON a.cart_id = c.id
LEFT JOIN orders o ON o.cart_id = c.id
WHERE c.user_id = ? AND o.id IS NULL;`,
	}
}

func expandMyWithFuzz(seed []string, target int, r *rand.Rand) []caseEntrye {
	var out []caseEntrye
	for i, s := range seed {
		out = append(out, caseEntrye{
			name:    fmt.Sprintf("My_seed_%02d", i+1),
			dialect: d.MySQL,
			sql:     myFuzz(s, r),
		})
	}
	need := target - len(seed)
	if need <= 0 {
		return out
	}
	for i := 0; i < need; i++ {
		var sql string
		switch i % 5 {
		case 0:
			rows := 2 + (i % 4)
			sql = fmt.Sprintf(`INSERT INTO logs(uid, lvl, msg, created_at)
VALUES
  %s
ON DUPLICATE KEY UPDATE msg = VALUES(msg);`, myValuesRowse(rows, i))
		case 1:
			sql = fmt.Sprintf(`UPDATE profile p
JOIN users u ON u.id = p.uid
SET p.js = JSON_SET(p.js, '$.touch', NOW()), p.ver = p.ver + 1
WHERE u.flag = %d;`, i%3)
		case 2:
			sql = fmt.Sprintf(`DELETE n FROM notes n
LEFT JOIN users u ON u.id = n.uid
WHERE u.status = 'banned' AND n.id %% %d = 0;`, 7+(i%5))
		case 3:
			sql = fmt.Sprintf(`INSERT INTO dst (id, name, tag)
SELECT s.id, UPPER(s.name), CONCAT('m-', %d) FROM src s
WHERE s.k BETWEEN %d AND %d;`, i, i, i+200)
		default:
			sql = fmt.Sprintf(`REPLACE INTO prefs(uid, js)
VALUES (%d, JSON_MERGE_PATCH(COALESCE((SELECT js FROM prefs WHERE uid=%d),'{}'), '{"mark":true}'));`, 100+i, 100+i)
		}
		out = append(out, caseEntrye{
			name:    fmt.Sprintf("My_auto_%03d", i),
			dialect: d.MySQL,
			sql:     myFuzz(sql, r),
		})
	}
	return out
}

func myValuesRowse(n, base int) string {
	var b strings.Builder
	for j := 0; j < n; j++ {
		if j > 0 {
			b.WriteString(",\n  ")
		}
		fmt.Fprintf(&b, "(%d, %d, CONCAT('m-', %d), NOW())", 10+(base+j)%100, 1+((base+j)%5), base+j)
	}
	return b.String()
}

/* ---------------- SQL Server: 种子 + 扩展 + fuzz ---------------- */

func seedDML_MSSQLe() []string {
	return []string{
		`WITH act AS (
  SELECT Id FROM dbo.Users WHERE Flag = 1 AND StartedAt >= DATEADD(day, -30, SYSUTCDATETIME())
)
UPDATE s
SET s.TTL = DATEADD(day, 7, SYSUTCDATETIME())
FROM dbo.Sessions s
JOIN act a ON s.UserId = a.Id
WHERE s.Status = 'open';`,

		`INSERT INTO dbo.Orders (UserId, Amount, Note, CreatedAt)
OUTPUT INSERTED.Id
VALUES (@u1, @a1, N'首单', SYSUTCDATETIME()),
       (@u2, @a2, N'促销-'+CAST(@x AS nvarchar(50)), SYSUTCDATETIME());`,

		`MERGE INTO dbo.Dst AS d
USING (SELECT @id AS Id, @name AS Name) AS s
ON (d.Id = s.Id)
WHEN MATCHED THEN UPDATE SET d.Name = s.Name
WHEN NOT MATCHED THEN INSERT (Id, Name) VALUES (s.Id, s.Name)
OUTPUT $action, inserted.Id;`,

		`UPDATE u
SET u.IsVip = JSON_VALUE(u.Profile,'$.vip'),
    u.IsBan = JSON_VALUE(u.Profile,'$.ban')
FROM dbo.Users u
WHERE u.Id = @id;`,

		`DELETE a
FROM dbo.CartItems a
JOIN dbo.Carts c ON a.CartId=c.Id
LEFT JOIN dbo.Orders o ON o.CartId=c.Id
WHERE c.UserId=@uid AND o.Id IS NULL;`,
	}
}

func expandMSWithFuzz(seed []string, target int, r *rand.Rand) []caseEntrye {
	var out []caseEntrye
	for i, s := range seed {
		out = append(out, caseEntrye{
			name:    fmt.Sprintf("MS_seed_%02d", i+1),
			dialect: d.SQLServer,
			sql:     msFuzz(s, r),
		})
	}
	need := target - len(seed)
	if need <= 0 {
		return out
	}
	for i := 0; i < need; i++ {
		var sql string
		switch i % 5 {
		case 0:
			sql = fmt.Sprintf(`MERGE dbo.Bal AS b
USING (SELECT %d AS Uid, %d AS Amt) s
ON (b.Uid=s.Uid)
WHEN MATCHED THEN UPDATE SET b.Amount = b.Amount + s.Amt
WHEN NOT MATCHED THEN INSERT (Uid, Amount) VALUES (s.Uid, s.Amt)
OUTPUT $action, inserted.Uid;`, 100+i, 5+(i%20))
		case 1:
			sql = fmt.Sprintf(`UPDATE o
SET o.Amount = o.Amount * 1.05, o.UpdatedAt = SYSUTCDATETIME()
FROM dbo.Orders o
JOIN dbo.Users u ON u.Id = o.UserId
WHERE u.Email LIKE '%%%d%%';`, i)
		case 2:
			sql = fmt.Sprintf(`DELETE TOP (%d) FROM dbo.Events WHERE Ts < DATEADD(day, -%d, SYSUTCDATETIME());`, 100+((i*7)%500), 30+(i%90))
		case 3:
			sql = fmt.Sprintf(`INSERT INTO dbo.Dst (Id, Name, Tag)
OUTPUT INSERTED.Id
SELECT s.Id, UPPER(s.Name), CONCAT(N'm-', %d) FROM dbo.Src s
WHERE s.K BETWEEN %d AND %d;`, i, i, i+300)
		default:
			sql = fmt.Sprintf(`UPDATE p
SET p.Json = JSON_MODIFY(p.Json, '$.touch', SYSUTCDATETIME()), p.Ver = p.Ver + 1
FROM dbo.Profile p
WHERE p.Uid = %d;`, 1000+i)
		}
		out = append(out, caseEntrye{
			name:    fmt.Sprintf("MS_auto_%03d", i),
			dialect: d.SQLServer,
			sql:     msFuzz(sql, r),
		})
	}
	return out
}

/* ---------------- Oracle: 种子 + 扩展 + fuzz ---------------- */

func seedDML_Oraclee() []string {
	return []string{
		`MERGE INTO dst d
USING (SELECT :id AS id, :name AS name FROM dual) s
ON (d.id = s.id)
WHEN MATCHED THEN UPDATE SET d.name = s.name
WHEN NOT MATCHED THEN INSERT (id, name) VALUES (s.id, s.name);`,

		`INSERT INTO orders (id, uid, amt, note, created_at)
VALUES (orders_seq.NEXTVAL, :u1, :a1, '首单', SYSTIMESTAMP),
       (orders_seq.NEXTVAL, :u2, :a2, '促销-'||:s, SYSTIMESTAMP);`,

		`INSERT INTO dst (id, name)
SELECT id, UPPER(name) FROM src
WHERE NOT EXISTS (SELECT 1 FROM dst d WHERE d.id = src.id);`,

		`UPDATE users
SET is_vip = JSON_VALUE(profile, '$.vip'),
    is_ban = JSON_VALUE(profile, '$.ban')
WHERE id = :id;`,

		`DELETE FROM cart_items a
WHERE EXISTS (
  SELECT 1 FROM carts c
  WHERE c.id = a.cart_id AND c.user_id = :uid
    AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.cart_id = c.id)
);`,
	}
}

func expandORWithFuzz(seed []string, target int, r *rand.Rand) []caseEntrye {
	var out []caseEntrye
	for i, s := range seed {
		out = append(out, caseEntrye{
			name:    fmt.Sprintf("OR_seed_%02d", i+1),
			dialect: d.Oracle,
			sql:     orFuzz(s, r),
		})
	}
	need := target - len(seed)
	if need <= 0 {
		return out
	}
	for i := 0; i < need; i++ {
		var sql string
		switch i % 5 {
		case 0:
			sql = fmt.Sprintf(`MERGE INTO bal b
USING (SELECT %d AS uid, %d AS amt FROM dual) s
ON (b.uid = s.uid)
WHEN MATCHED THEN UPDATE SET b.amount = b.amount + s.amt
WHEN NOT MATCHED THEN INSERT (uid, amount) VALUES (s.uid, s.amt);`, 200+i, 3+(i%15))
		case 1:
			sql = fmt.Sprintf(`UPDATE orders o
SET o.amt = o.amt * 1.05, o.updated_at = SYSTIMESTAMP
WHERE EXISTS (SELECT 1 FROM users u WHERE u.id = o.user_id AND REGEXP_LIKE(u.email, '%d'));`, i)
		case 2:
			sql = fmt.Sprintf(`DELETE FROM events WHERE ROWNUM <= %d AND ts < DATE '2024-01-01' + %d;`, 50+((i*13)%300), (i % 365))
		case 3:
			sql = fmt.Sprintf(`INSERT /*+ APPEND */ INTO notes(id, uid, note, created_at)
SELECT notes_seq.NEXTVAL, u.id, q'[批次-%d]', SYSTIMESTAMP
FROM users u WHERE u.flag = MOD(%d, 3);`, i, i)
		default:
			sql = fmt.Sprintf(`UPDATE prefs p SET p.js = JSON_MERGEPATCH(p.js, '{"touch":true}')
WHERE p.uid = %d`, 3000+i)
		}
		out = append(out, caseEntrye{
			name:    fmt.Sprintf("OR_auto_%03d", i),
			dialect: d.Oracle,
			sql:     orFuzz(sql, r),
		})
	}
	return out
}

/* ============================ Fuzz helpers ============================ */

func randFromEnv() *rand.Rand {
	seed := int64(1337)
	if v := os.Getenv("DML_FUZZ_SEED"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			seed = n
		}
	}
	return rand.New(rand.NewSource(seed))
}

// 安全插注释/换行：仅在关键词与空白相邻处插入；避免字符串/$$/q”/JSON 路径内部（启发式，保持轻度）
var kwSpots = []string{"INSERT", "UPDATE", "DELETE", "MERGE", "SELECT", "VALUES", "FROM", "WHERE", "SET", "JOIN", "USING", "ON"}

func injectCommentsWhitespace(sql string, r *rand.Rand, lineStyle string) string {
	// 1) 逗号后偶尔换行
	sql = regexp.MustCompile(`,\s*`).ReplaceAllStringFunc(sql, func(s string) string {
		if r.Float64() < 0.2 {
			return ",\n  "
		}
		return s
	})
	// 2) 在部分关键词后插入块注释
	for _, kw := range kwSpots {
		re := regexp.MustCompile(`\b` + kw + `\b`)
		sql = re.ReplaceAllStringFunc(sql, func(s string) string {
			if r.Float64() < 0.15 {
				return s + " /*fuzz*/"
			}
			return s
		})
	}
	// 3) 结尾 10% 概率追加行注释
	if r.Float64() < 0.10 {
		if strings.HasSuffix(strings.TrimSpace(sql), ";") {
			sql = strings.TrimSpace(sql) + " " + lineStyle + " fuzz\n"
		} else {
			sql = strings.TrimSpace(sql) + "; " + lineStyle + " fuzz\n"
		}
	}
	return sql
}

func pgFuzz(sql string, r *rand.Rand) string {
	sql = injectCommentsWhitespace(sql, r, "--")
	// 10% 把 now() 换成 CURRENT_TIMESTAMP(3)
	if r.Float64() < 0.10 {
		sql = strings.ReplaceAll(sql, "now()", "CURRENT_TIMESTAMP(3)")
	}
	// 20% 把 "),(" 强制断行
	if r.Float64() < 0.20 {
		sql = strings.ReplaceAll(sql, "),(", "),\n(")
	}
	return sql
}
func myFuzz(sql string, r *rand.Rand) string {
	sql = injectCommentsWhitespace(sql, r, "--")
	// 10% 把 NOW() 换成 CURRENT_TIMESTAMP(3)
	if r.Float64() < 0.10 {
		sql = strings.ReplaceAll(sql, "NOW()", "CURRENT_TIMESTAMP(3)")
	}
	if r.Float64() < 0.20 {
		sql = strings.ReplaceAll(sql, "),(", "),\n(")
	}
	// 5% 在头部注入 MySQL 版本注释
	if r.Float64() < 0.05 {
		sql = "/*!40101 SET @a:=1*/; " + sql
	}
	return sql
}
func msFuzz(sql string, r *rand.Rand) string {
	sql = injectCommentsWhitespace(sql, r, "--")
	if r.Float64() < 0.20 {
		sql = strings.ReplaceAll(sql, "),(", "),\n(")
	}
	return sql
}
func orFuzz(sql string, r *rand.Rand) string {
	sql = injectCommentsWhitespace(sql, r, "--")
	// 10% 把 SYSTIMESTAMP 替换为 CURRENT_TIMESTAMP(3)
	if r.Float64() < 0.10 {
		sql = strings.ReplaceAll(sql, "SYSTIMESTAMP", "CURRENT_TIMESTAMP(3)")
	}
	if r.Float64() < 0.20 {
		sql = strings.ReplaceAll(sql, "),(", "),\n(")
	}
	return sql
}

/* ============================ 稳定性种子（Edge Pack） ============================ */

var pgEdgeSQL = []string{
	`/*head*/ SELECT /* in */ id, $$a;b;c$$ AS body, $tag$ (x)->> 'y' $tag$ AS j
FROM t -- tail
WHERE js @> '{"k":1}'::jsonb AND arr ?| ARRAY['x','y'] AND hstore_col ? 'k';`,
	`INSERT INTO t(a,b) VALUES ($1, E'it\'s ok'); /*x*/ ; UPDATE t SET b = $$ok$$ WHERE a = $1 RETURNING a;`,
	`SELECT 1::int = ANY(ARRAY[1,2,$1]) AND 2 = ALL(ARRAY[2,2]);`,
	`SELECT (((((SUM(CASE WHEN name ~* $1 THEN 1 ELSE 0 END)) FILTER (WHERE flag))))))
FROM u;`,
	`INSERT INTO logs(uid, msg, ts) VALUES ($1, 'a', now()), ($2, 'b', now());`,
	`INSERT INTO logs(uid, msg, ts) VALUES (10, 'x', '2024-01-01'), (11, 'y', '2024-01-02');`,
	`SELECT $a$hello; it's ok$a$ AS x;`,
	`SELECT jsonb_path_query(js, '$.a ? (@ > 1)') @> to_jsonb($1);`,
	`SELECT arr OPERATOR(pg_catalog.@>) ARRAY[1,2] AND id = ANY($1);`,
	`WITH old AS (
  DELETE FROM sess WHERE ts < now() - INTERVAL '7 days' RETURNING id, user_id
)
INSERT INTO sess_arc(id, uid) SELECT id, user_id FROM old;`,
	`INSERT INTO tz(ts_local) VALUES ((now() AT TIME ZONE 'UTC') AT TIME ZONE 'Asia/Taipei'));`,
	`INSERT INTO t(ts) VALUES (CURRENT_TIMESTAMP(3)), (LOCALTIMESTAMP(6));`,
	`UPDATE u SET note = E'\u263A smile', nick = E'it\\'s ok' WHERE id = $1;`,
	`DELETE FROM a WHERE EXISTS (SELECT 1 FROM b WHERE b.k = a.k AND b.v > (SELECT AVG(v) FROM b WHERE k = a.k));`,
	`SELECT tags ?& ARRAY['red','hot'] FROM x;`,
	"INSERT INTO notes(txt) VALUES (E'line1\tline2\r\nline3');",
	`SELECT u.id, e->>'n' FROM u CROSS JOIN LATERAL jsonb_array_elements(u.j->'x') e;`,
	`SELECT SUM(v) OVER (ORDER BY ts ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW EXCLUDE CURRENT ROW) FROM t;`,
	`SELECT (a,b) OVERLAPS (c,d) FROM r;`,
	`UPDATE t SET hs = hs || hstore('k', $1) WHERE id=$2;`,
}

var myEdgeSQL = []string{
	`/*!40101 SET @a:=1*/; INSERT /*x*/ INTO t (a,b) VALUES (1,'x'),(2,'y'); -- tail`,
	`UPDATE u SET nick = _utf8mb4'Álfa' COLLATE utf8mb4_0900_ai_ci,
  js = JSON_SET(js, '$.k', JSON_EXTRACT(js, '$.x')) WHERE id = ?;`,
	`INSERT INTO tz(uid, ts_local) SELECT id, CONVERT_TZ(NOW(),'UTC','Asia/Taipei') FROM u
ON DUPLICATE KEY UPDATE ts_local = VALUES(ts_local);`,
	`INSERT INTO it(id, tag)
SELECT j.id, t.tag FROM j,
JSON_TABLE(j.js, '$.tags[*]' COLUMNS(tag VARCHAR(20) PATH '$')) t;`,
	`UPDATE sums s JOIN (
  SELECT id, SUM(v) OVER (ORDER BY ts RANGE BETWEEN INTERVAL 1 DAY PRECEDING AND CURRENT ROW) AS sv
  FROM t
) x ON x.id = s.id SET s.sum = x.sv;`,
	`REPLACE INTO kv(k,v) VALUES (?, JSON_MERGE_PATCH(COALESCE((SELECT v FROM kv WHERE k=?),'{}'), '{"touch":true}'));`,
	`DELETE FROM o WHERE uid IN (SELECT uid FROM (SELECT uid, COUNT(*) c FROM o GROUP BY uid HAVING c > ?) s);`,
	`INSERT INTO m(v) VALUES (-1.23e-4), (1e6); UPDATE u SET email = REGEXP_REPLACE(email, '[^a-z0-9@._-]', '') WHERE id=?;`,
	`INSERT INTO logs(uid, msg, ts) VALUES (10,'x','2024-01-01'), (11,'y','2024-01-02'), (12,'z','2024-01-03');`,
	`INSERT INTO logs(uid, msg, ts) VALUES (?, 'x', NOW()), (?, 'y', NOW());`,
	`UPDATE t SET js = JSON_REMOVE(js, JSON_UNQUOTE(JSON_SEARCH(js, 'one', ?))) WHERE id=?;`,
	`DELETE FROM p PARTITION (p2023) WHERE ts < '2024-01-01';`,
	`UPDATE g SET base = base + 1 WHERE id=?; -- 虚拟列由表达式计算，不直接更新`,
	`DELETE FROM logs WHERE created_at < STR_TO_DATE(?, '%Y-%m-%d');`,
	`SELECT JSON_OVERLAPS(js, JSON_OBJECT('a', JSON_ARRAY(1,2))) FROM t;`,
	`SELECT /*+ SET_VAR(sort_buffer_size=262144) */ * FROM u FORCE INDEX (idx_flag) WHERE flag=?;`,
	`(SELECT * FROM a WHERE k=?) UNION ALL (SELECT * FROM b WHERE k=?) ORDER BY ts DESC LIMIT 50;`,
	`INSERT INTO notes(txt) VALUES ('🚀火箭');`,
	`DELETE x FROM x
JOIN JSON_TABLE(x.js, '$.items[*]' COLUMNS(id INT PATH '$.id', tags JSON PATH '$.tags')) j
JOIN JSON_TABLE(j.tags, '$[*]' COLUMNS(tag VARCHAR(20) PATH '$')) t
ON t.tag = ?;`,
}

var msEdgeSQL = []string{
	`SELECT j.id, tag.value
FROM dbo.T t
CROSS APPLY OPENJSON(t.Js,'$.items')
WITH (id INT '$.id', tags NVARCHAR(MAX) AS JSON) j
CROSS APPLY OPENJSON(j.tags) tag;`,
	`MERGE dbo.Dst AS d
USING (SELECT @id AS Id, @name AS Name) s
ON (d.Id=s.Id)
WHEN MATCHED THEN UPDATE SET d.Name=s.Name
WHEN NOT MATCHED THEN INSERT(Id,Name) VALUES(s.Id,s.Name)
OUTPUT $action, inserted.Id, deleted.Id;`,
	`DELETE TOP (1000) FROM dbo.Logs WHERE Ts < DATEADD(day, -@n, SYSUTCDATETIME());`,
	`UPDATE u SET Score = TRY_CONVERT(INT, JSON_VALUE(Profile,'$.score')) WHERE ISJSON(Profile)=1 AND Id=@id;`,
	`SELECT * FROM dbo.U WITH (INDEX(idx_flag)) WHERE Flag = @f;`,
	`SELECT a.Id, x.cnt FROM dbo.A a
CROSS APPLY (SELECT COUNT(*) AS cnt FROM dbo.B b WHERE b.AId=a.Id AND b.Flag=1) x;`,
	`INSERT INTO Tz(ts_local) VALUES ((SYSUTCDATETIME() AT TIME ZONE 'UTC') AT TIME ZONE 'Taipei Standard Time'));`,
	`SELECT * FROM (
  SELECT Cat, Mo, Amt FROM S
) p PIVOT (SUM(Amt) FOR Mo IN ([Jan],[Feb])) AS pv
UNPIVOT (Val FOR Attr IN ([Jan],[Feb])) AS uv;`,
	`UPDATE u SET TagsCount = (SELECT COUNT(*) FROM STRING_SPLIT(u.Tags, ','));`,
	`INSERT INTO dbo.Orders (UserId, Amount) OUTPUT INSERTED.Id VALUES (@u, @a);`,
	`UPDATE t SET Js = JSON_MODIFY(Js, '$.flags.vip', 1) WHERE Id=@id;`,
	`UPDATE a SET Mark = CASE WHEN EXISTS(SELECT 1 FROM b WHERE b.K=a.K) THEN 1 ELSE 0 END;`,
	`SELECT * FROM T ORDER BY Ts OFFSET 100 ROWS FETCH NEXT 50 ROWS ONLY;`,
	`SELECT ((((((((((((@p1+@p2)))))))))))) AS x;`,
	`UPDATE g SET Base = Base + 1 WHERE Id=@id;`,
	`INSERT INTO #tmp(id,v) VALUES (1,'x'),(2,'y'); DELETE FROM #tmp WHERE id=@id;`,
	`SELECT SUM(v) OVER (PARTITION BY k ORDER BY ts ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS s FROM t;`,
	`UPDATE t SET N = TRY_PARSE('0xFF' AS INT USING 'en-US') WHERE Id=@id;`,
	`INSERT INTO t(v) VALUES (@v); SELECT SCOPE_IDENTITY();`,
}

var orEdgeSQL = []string{
	`INSERT INTO "Weird Table" ("User Id","Note") VALUES (:u, q'[a;b;c]');`,
	`UPDATE t SET note = q'{it's ok}', note2 = q'(a(b)c)';`,
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
	`MERGE INTO dst d
USING (SELECT :id AS id, :name AS name FROM dual) s
ON (d.id = s.id)
WHEN MATCHED THEN UPDATE SET d.name = REGEXP_REPLACE(s.name, '[^A-Za-z0-9]', '')
WHEN NOT MATCHED THEN INSERT (id, name) VALUES (s.id, s.name);`,
	`UPDATE u SET is_vip = JSON_VALUE(profile,'$.vip')
WHERE JSON_EXISTS(profile, '$.vip?(@==true)') AND id=:id;`,
	`INSERT ALL
  INTO a(id,v) VALUES (:id, 'x')
  INTO b(id,v) VALUES (:id, 'y')
SELECT 1 FROM dual;`,
	`DELETE FROM t WHERE id IN (
  SELECT id FROM tree START WITH parent_id IS NULL CONNECT BY PRIOR id = parent_id
);`,
	`UPDATE d SET names = (SELECT LISTAGG(e.ename, ',') WITHIN GROUP (ORDER BY e.ename) FROM emp e WHERE e.deptno=d.deptno);`,
	`SELECT region, product, sales FROM sales
MODEL RETURN UPDATED ROWS
PARTITION BY (region)
DIMENSION BY (product)
MEASURES (sales)
RULES ( sales['ALL'] = SUM(sales)[ANY] );`,
	`UPDATE g SET base = base + 1 WHERE id=:id;`,
	`INSERT INTO tz(ts_local) VALUES (FROM_TZ(SYSTIMESTAMP, 'UTC') AT TIME ZONE 'Asia/Taipei');`,
	`INSERT INTO notes(txt) VALUES (q'[含分号;与引号''与)] ]');`,
	`DELETE FROM u WHERE REGEXP_LIKE(email, :pat) AND NVL(flag,0)=0;`,
	`INSERT INTO x(col)
SELECT xt.col FROM t,
XMLTABLE('/root/item' PASSING t.xml COLUMNS col VARCHAR2(100) PATH 'name') xt;`,
	`MERGE INTO bal b
USING (SELECT :uid AS uid, :amt AS amt FROM dual) s
ON (b.uid = s.uid)
WHEN MATCHED THEN UPDATE SET b.amount=b.amount+s.amt
WHEN NOT MATCHED THEN INSERT (uid, amount) VALUES (s.uid, s.amt);`,
	`UPDATE z SET mark_dt = DATE '2024-01-01' WHERE id=:id;`,
	`SELECT * FROM (
  SELECT deptno, job, sal FROM emp
) src PIVOT (SUM(sal) FOR job IN ('CLERK','ANALYST','MANAGER')) p;`,
	`INSERT INTO notes(txt) VALUES ('🙂表情');`,
	`UPDATE d SET last_sal = (
  SELECT MAX(sal) KEEP (DENSE_RANK LAST ORDER BY ts) FROM emp WHERE emp.deptno=d.deptno
);`,
}

/* ============================ 断言/工具 ============================ */

func oneLinee(s string) string { return strings.Join(strings.Fields(s), " ") }

// 括号配平断言：逐语句（以 ; 分割），确保没有多余右括号且左右数量一致
func assertParensBalancede(t *testing.T, digest string) {
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
