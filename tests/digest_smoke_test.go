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
