package sqlglot

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

/*
Bench overview
--------------
- BenchmarkSignature_Serial:    单线程串行，覆盖四方言 × 两组常用选项
- BenchmarkSignature_Parallel:  并行压测（b.RunParallel），模拟多 goroutine 抢解析

数据集
------
- 每个方言 6~8 条复杂 SQL（DML/函数/特殊字面量/注释）
- 通过轻量“噪声变体”（空白/注释/换行）扩大多样性，但不破坏语义
*/

var (
	sinkDigest string
	sinkParams []ExParam
)

func BenchmarkSignature_Serial(b *testing.B) {
	b.ReportAllocs()
	r := rand.New(rand.NewSource(42))

	cases := []struct {
		name string
		d    Dialect
		set  []string
	}{
		{"MySQL", MySQL, corpusMySQL()},
		{"Postgres", Postgres, corpusPostgres()},
		{"SQLServer", SQLServer, corpusMSSQL()},
		{"Oracle", Oracle, corpusOracle()},
	}

	opts := []Options{
		{CollapseValuesInDigest: false, ParamizeTimeFuncs: false}, // baseline
		{CollapseValuesInDigest: true, ParamizeTimeFuncs: true},   // common prod setup
	}

	for _, c := range cases {
		// 为每个方言生成轻噪声变体，扩大数据集多样性
		set := withNoiseVariants(r, c.set, 3)

		for _, opt := range opts {
			opt := opt // capture
			name := fmt.Sprintf("%s/Collapse=%t/Time=%t", c.name, opt.CollapseValuesInDigest, opt.ParamizeTimeFuncs)

			b.Run(name, func(b *testing.B) {
				opt.Dialect = c.d
				benchSignatureSet(b, set, opt)
			})
		}
	}
}

func BenchmarkSignature_Parallel(b *testing.B) {
	b.ReportAllocs()
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// 用混合方言的大集合，模拟生产多租户场景
	var mega []string
	mega = append(mega, corpusMySQL()...)
	mega = append(mega, corpusPostgres()...)
	mega = append(mega, corpusMSSQL()...)
	mega = append(mega, corpusOracle()...)
	mega = withNoiseVariants(r, mega, 2)

	opt := Options{
		Dialect:                MySQL, // 仅用于演示；下面每次调用会循环切换方言
		CollapseValuesInDigest: true,
		ParamizeTimeFuncs:      true,
	}

	dialects := []Dialect{MySQL, Postgres, SQLServer, Oracle}

	b.ResetTimer()
	idx := 0
	b.RunParallel(func(pb *testing.PB) {
		localIdx := 0
		for pb.Next() {
			s := mega[(idx+localIdx)%len(mega)]
			d := dialects[(idx+localIdx)%len(dialects)]
			localIdx++

			opt.Dialect = d
			dig, params, err := Signature(s, opt)
			if err != nil {
				b.Fatalf("Signature error: %v\nsql=%s", err, s)
			}
			// 防止被编译器优化掉
			sinkDigest = dig
			sinkParams = params
		}
	})
}

/* --------------------------------- Helpers --------------------------------- */

func benchSignatureSet(b *testing.B, set []string, opt Options) {
	b.Helper()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sql := set[i%len(set)]
		dig, params, err := Signature(sql, opt)
		if err != nil {
			b.Fatalf("Signature error: %v\nsql=%s", err, sql)
		}
		sinkDigest = dig
		sinkParams = params
	}
}

// 生成轻噪声变体：仅做“安全操作”（前后注释/空白、逗号后换行、扩展连续空格）
// 不在字符串字面量内插入任何内容，避免破坏 SQL。
func withNoiseVariants(r *rand.Rand, in []string, per int) []string {
	if per <= 0 {
		return append([]string(nil), in...)
	}
	out := make([]string, 0, len(in)*(per+1))
	for _, s := range in {
		out = append(out, s)
		for i := 0; i < per; i++ {
			out = append(out, mutateNoise(r, s))
		}
	}
	return out
}

func mutateNoise(r *rand.Rand, s string) string {
	t := s
	// 1) 头尾注释/空白
	if r.Intn(2) == 0 {
		t = "/*bench*/ " + t
	}
	if r.Intn(2) == 0 {
		t = t + " /*tail*/"
	}
	// 2) 逗号后随机换行
	if strings.Contains(t, ", ") && r.Intn(2) == 0 {
		t = strings.ReplaceAll(t, ", ", ",\n")
	}
	// 3) 扩充连续空格（不进字面量）
	if r.Intn(3) == 0 {
		t = strings.ReplaceAll(t, "  ", "     ")
	}
	// 4) 随机在 SELECT/INSERT 前加一个换行
	if r.Intn(3) == 0 {
		t = strings.Replace(t, "SELECT", "\nSELECT", 1)
		t = strings.Replace(t, "INSERT", "\nINSERT", 1)
	}
	return t
}

/* --------------------------------- Corpora --------------------------------- */

// MySQL
func corpusMySQL() []string {
	return []string{
		`/*!40101 SET @a:=1*/; INSERT /*x*/ INTO t (a,b) VALUES (1,'x'),(2,'y'); -- tail`,
		`INSERT INTO t (a, ts) VALUES (1, NOW()), (2, NOW()), (3, CURRENT_TIMESTAMP(3));`,
		`SELECT JSON_EXTRACT('{"x":[1,2,3]}', '$.x[1]') AS v;`,
		`UPDATE users u JOIN JSON_TABLE(:js, '$[*]' COLUMNS(tag VARCHAR(50) PATH '$')) jt
         ON JSON_CONTAINS(u.tags, JSON_QUOTE(jt.tag), '$')
         SET u.touch = NOW();`,
		`DELETE x FROM x WHERE JSON_CONTAINS(x.tags, JSON_QUOTE(:t), '$');`,
		`INSERT INTO notes(txt) VALUES (CONCAT('a', 'b', $1));`,
		`INSERT INTO t(a,b) VALUES (1,'x'),(2,'y'),(3,'z'),(4,'w');`,
	}
}

// Postgres
func corpusPostgres() []string {
	return []string{
		`SELECT $$abc$$, $1::text, DATE '2020-01-01' FROM t LIMIT 10 OFFSET 5;`,
		`INSERT INTO t(a,b) VALUES (1,'x'), (2,'y'), (3, $$d;d$$);`,
		`SELECT fn_upsert_user(1001, 'alice', '{"tier":"gold"}'::jsonb);`,
		`SELECT jsonb_path_query(meta, '$.x[*]') FROM users WHERE id = $1;`,
		`UPDATE t SET ts = now() AT TIME ZONE 'UTC' WHERE id IN (SELECT id FROM t2);`,
		`DELETE FROM t WHERE (a,b) IN (SELECT a,b FROM t2 WHERE c > 0);`,
		`SELECT * FROM t WHERE note ~* $2 AND a BETWEEN 1 AND 10;`,
	}
}

// SQL Server
func corpusMSSQL() []string {
	return []string{
		`DECLARE @rc INT; EXEC dbo.pr_upsert_user @Id=1, @Name=N'alice', @Rows=@rc OUTPUT; SELECT @rc;`,
		`SELECT CONVERT(nvarchar(30), SYSUTCDATETIME(), 126) AT TIME ZONE 'Asia/Taipei';`,
		`SELECT * FROM OPENJSON(@j) WITH (vip bit '$.vip', ban bit '$.ban');`,
		`UPDATE u SET u.Rank = r.rn FROM users u
         JOIN (SELECT id, ROW_NUMBER() OVER (ORDER BY score DESC) rn FROM users) r ON r.id=u.id;`,
		`DELETE FROM sessions WHERE last_seen < DATEADD(day, -90, SYSUTCDATETIME());`,
		`INSERT INTO t(a,b) OUTPUT INSERTED.id VALUES (1,N'x'),(2,N'y');`,
	}
}

// Oracle
func corpusOracle() []string {
	return []string{
		`INSERT INTO notes(txt) VALUES (q'[hello;]') RETURNING id INTO :out;`,
		`SELECT FROM_TZ(SYSTIMESTAMP, 'UTC') AT TIME ZONE 'Asia/Taipei' AS ts_local FROM dual;`,
		`MERGE INTO dst d
         USING (SELECT :id AS id, :js AS js FROM dual) s
         ON (d.id = s.id)
         WHEN MATCHED THEN UPDATE SET d.meta = JSON_MERGEPATCH(COALESCE(d.meta, '{}'), s.js)
         WHEN NOT MATCHED THEN INSERT (id, meta) VALUES (s.id, s.js);`,
		`UPDATE users u
         SET (u.is_vip, u.is_ban) = (
           SELECT jt.vip, jt.ban FROM JSON_TABLE(u.profile, '$'
             COLUMNS (vip NUMBER(1) PATH '$.vip', ban NUMBER(1) PATH '$.ban'))) 
         WHERE JSON_EXISTS(u.profile, '$.vip') OR JSON_EXISTS(u.profile, '$.ban');`,
		`DELETE FROM t WHERE id IN (
           SELECT id FROM tree START WITH parent_id IS NULL CONNECT BY PRIOR id = parent_id
         );`,
		`INSERT INTO orders (id, uid, amt, note, created_at)
         VALUES (orders_seq.NEXTVAL, :u, :a, q'{首单}', SYSTIMESTAMP)
         RETURNING id INTO :new_id;`,
	}
}
