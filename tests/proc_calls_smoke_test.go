package tests

import (
	"fmt"
	"strings"
	"testing"
	d "tsql_digest_v4/internal/sqldigest_antlr"
)

// 仅测试“调用”，避免与现有大用例冲突：函数名、Helper 名都后缀 SP。
//go test -v -count=1 . -run StoredProcedures_Calls_Smoke

func Test_StoredProcedures_Calls_Smoke(t *testing.T) {
	cases := procCallCases()

	opt := d.Options{
		CollapseValuesInDigest: true,
		ParamizeTimeFuncs:      true,
	}

	for i, c := range cases {
		opt.Dialect = c.dialect

		res, err := d.BuildDigestANTLR(c.sql, opt)
		if err != nil {
			t.Fatalf("[#%d %s] build error: %v\nsql=\n%s", i, c.name, err, c.sql)
		}
		if strings.TrimSpace(res.Digest) == "" {
			t.Fatalf("[#%d %s] empty digest\nsql=\n%s", i, c.name, c.sql)
		}
		assertParensBalancedSP(t, res.Digest)

		// 参数切片与原文一致性
		for pi, p := range res.Params {
			if !(p.Start >= 0 && p.End > p.Start && p.End <= len(c.sql)) {
				t.Fatalf("[#%d %s] param #%d invalid range: [%d,%d) len=%d\nsql=\n%s",
					i, c.name, pi+1, p.Start, p.End, len(c.sql), c.sql)
			}
			if got := c.sql[p.Start:p.End]; got != p.Value {
				t.Fatalf("[#%d %s] param #%d value mismatch: slice=%q vs p.Value=%q\nsql=\n%s",
					i, c.name, pi+1, got, p.Value, c.sql)
			}
			if strings.Contains(p.Value, "), (") {
				t.Fatalf("[#%d %s] param spans tuple boundary: %q\nsql=\n%s",
					i, c.name, p.Value, c.sql)
			}
		}

		if testing.Verbose() {
			t.Logf("[#%d %s]\nDialect: %v\nSQL   : %s\nDigest: %s\nParams: %v\n",
				i, c.name, c.dialect, oneLineSP(c.sql), res.Digest, res.Params)
		}
	}
}

func procCallCases() []caseEntry {
	var out []caseEntry

	// -------- PostgreSQL: CALL / SELECT fn(...) / mixed casts ----------
	pg := []string{
		`CALL pr_mark_vip($1, true);`,
		`SELECT fn_upsert_user(1001, 'alice', '{"tier":"gold"}'::jsonb);`,
		`SELECT * FROM fn_top_orders($1, 10);`,
		`SELECT fn_safe_div($1, $2);`,
		`SELECT fn_match(ARRAY[$1,$2], $3);`,
		`SELECT fn_upsert_user(1002, DEFAULT, '{}'::jsonb);`,
		`CALL pr_mark_vip($1, FALSE);`,
		`SELECT fn_top_orders($1::bigint, 5::int);`,
		// 带时间函数（安全参数化）+ AT TIME ZONE
		`SELECT fn_upsert_user(1003, 'bob', jsonb_build_object('t', now() AT TIME ZONE 'UTC'));`,
		// 多语句（分号切割）
		`CALL pr_mark_vip(1004, true); SELECT fn_safe_div(10, 2);`,
		// dollar-quoted 实参（整体应被参数化）
		`SELECT fn_upsert_user(1005, 'qq', $$ {"x":1, "y":"a;b"} $$::jsonb);`,
	}
	for i, s := range pg {
		out = append(out, caseEntry{fmt.Sprintf("PG_call_%02d", i+1), d.Postgres, s})
	}

	// -------- MySQL: CALL / 函数 / 用户变量 OUT ----------
	my := []string{
		`CALL pr_upsert_user(?, ?, @rc); SELECT @rc;`,
		`SELECT fn_json_tag_count(JSON_OBJECT('tags', JSON_ARRAY('a','b')));`,
		`CALL pr_rank_user(?);`,
		`CALL pr_safe_div(?, ?, @q); SELECT @q;`,
		`CALL pr_delete_by_tags(JSON_ARRAY('promo','vip'));`,
		`CALL pr_dyn_upd('prefs', 42, JSON_OBJECT('touch', true));`,
		// NOW() 等时间函数作为实参
		`CALL pr_upsert_user(1001, 'alice', @n); SET @n := NOW();`,
		// 版本注释 + CALL
		`/*!40101 SET @a:=1*/; CALL pr_rank_user(1);`,
		// 混合 JSON_TABLE 实参
		`CALL pr_delete_by_tags(JSON_ARRAY('a', 'b', JSON_EXTRACT('{"x":["c"]}', '$.x[0]')));`,
		// 多语句
		`CALL pr_safe_div(1, 0, @q); SELECT IFNULL(@q, -1);`,
	}
	for i, s := range my {
		out = append(out, caseEntry{fmt.Sprintf("My_call_%02d", i+1), d.MySQL, s})
	}

	// -------- SQL Server: EXEC / OUTPUT / TVP-ish 调用伪造 ----------
	ms := []string{
		`DECLARE @rows INT; EXEC dbo.pr_upsert_user @Id=1, @Name=N'alice', @Rows=@rows OUTPUT; SELECT @rows;`,
		`EXEC dbo.pr_set_flags @Id=1, @Js=N'{"vip":true,"ban":false}';`,
		`DECLARE @t dbo.IdName; INSERT INTO @t VALUES (1,N'a'),(2,N'b'); EXEC dbo.pr_bulk_upsert @T=@t;`,
		`EXEC dbo.pr_dyn_update @Table=N'Notes', @Id=10, @Note=N'hello';`,
		`EXEC dbo.pr_transfer @From=1, @To=2, @Amt=100.00;`,
		// 时间函数作为参数
		`EXEC dbo.pr_dyn_update @Table=N'Logs', @Id=1, @Note=CONVERT(nvarchar(30), SYSUTCDATETIME(), 126);`,
		// 变量 + JSON_VALUE
		`DECLARE @j nvarchar(max)=N'{"vip":1}'; EXEC dbo.pr_set_flags @Id=2, @Js=@j;`,
		// 多语句
		`DECLARE @rc INT; EXEC dbo.pr_upsert_user @Id=2,@Name=N'bob',@Rows=@rc OUTPUT; SELECT COALESCE(@rc,0);`,
	}
	for i, s := range ms {
		out = append(out, caseEntry{fmt.Sprintf("MS_call_%02d", i+1), d.SQLServer, s})
	}

	// -------- Oracle: CALL / 匿名块 BEGIN..END 调用 ----------
	or := []string{
		`BEGIN pr_upsert_user(1, 'ALICE', :n); END;`,
		`SELECT fn_tag_count('{"tags":["a","b"]}') FROM dual;`,
		`DECLARE q NUMBER:=0; BEGIN pr_safe_div(1, 0, q); NULL; END;`,
		`BEGIN pr_touch_users; END;`,
		`BEGIN pr_dyn_update('NOTES', 10, 'hello'); END;`,
		`BEGIN pr_merge_gtt; END;`,
		// 时间函数作为参数
		`BEGIN pr_dyn_update('LOGS', 1, TO_CHAR(SYSTIMESTAMP, 'YYYY-MM-DD"T"HH24:MI:SS.FF3TZH:TZM')); END;`,
		// JSON_VALUE 相关实参
		`SELECT fn_tag_count('{"tags":["x","y","z"]}') FROM dual;`,
		// 多语句（两个匿名块 + 查询）
		`BEGIN pr_upsert_user(2, 'BOB', :n); END; SELECT fn_tag_count('{"tags":["a"]}') FROM dual;`,
	}
	for i, s := range or {
		out = append(out, caseEntry{fmt.Sprintf("OR_call_%02d", i+1), d.Oracle, s})
	}

	return out
}

/* ------------------------------ 辅助断言（避免与既有重名） ------------------------------ */

func oneLineSP(s string) string { return strings.Join(strings.Fields(s), " ") }

// 逐“语句”（以 ; 分割）做括号配平。只判定多余右括号与总平衡，不尝试修复。
func assertParensBalancedSP(t *testing.T, digest string) {
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
