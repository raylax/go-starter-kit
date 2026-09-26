package tests

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestAccountStoreAccessRules(t *testing.T) {
	for _, tc := range []struct {
		name, file, source string
		allowed            bool
	}{
		{"直接读取", "profile.go", `func f() { q.queries.GetUser(ctx, id) }`, true},
		{"读取方法值", "profile.go", `func f() { read := q.queries.GetUser; _ = read }`, true},
		{"事务加锁读取", "authorization.go", `func f() { q.queries.LockUser(ctx, id) }`, true},
		{"经受控写入入口", "registration.go", `func f() { q.CreatePendingUser(ctx, p) }`, true},
		{"直接写入", "registration.go", `func f() { q.queries.CreatePendingUser(ctx, p) }`, false},
		{"写入方法值", "registration.go", `func f() { write := q.queries.CreatePendingUser; _ = write }`, false},
		{"取出查询对象", "registration.go", `func f() { raw := q.queries; raw.CreatePendingUser(ctx, p) }`, false},
		{"向函数传递查询对象", "registration.go", `func f() { write(q.queries) }`, false},
		{"重新绑定事务", "registration.go", `func f() { q.queries.WithTx(tx).CreatePendingUser(ctx, p) }`, false},
		{"未知读取方法", "profile.go", `func f() { q.queries.GetFutureUser(ctx, id) }`, false},
		{"另建生成查询", "registration.go", `import dbq "example/internal/db/sqlc"; func f() { dbq.New(db) }`, false},
		{"匿名嵌入查询类型", "service.go", `import "example/internal/db/sqlc"; type store struct { *sqlc.Queries }`, false},
		{"点导入", "service.go", `import . "example/internal/db/sqlc"; func f() { New(db) }`, false},
		{"store 内写入", "store.go", `func f() { q.queries.CreatePendingUser(ctx, p) }`, true},
		{"store 内状态转换", "store_flows.go", `func f() { q.queries.FinishFlow(ctx, p) }`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), tc.file, "package account\n"+tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			violations := accountStoreViolations(tc.file, file)
			if (len(violations) == 0) != tc.allowed {
				t.Fatalf("访问规则结果错误: %v", violations)
			}
		})
	}
}
