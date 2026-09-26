//go:build integration

package migrations_test

import (
	"errors"
	"testing"

	"github.com/example/go-starter-kit/tests/integration/testutil"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestBaselineStringIDsAndReferences(t *testing.T) {
	pool, _ := testutil.Database(t)
	ctx := t.Context()
	// 基线中的资源、引用及租约标识均为 TEXT，且不由数据库生成。
	rows, err := pool.Query(ctx, `SELECT table_name, column_name, data_type, column_default
        FROM information_schema.columns
        WHERE table_schema='public' AND table_name <> 'goose_db_version'
          AND (column_name='id' OR right(column_name, 3)='_id' OR column_name='claimed_by')`)
	if err != nil {
		t.Fatal(err)
	}
	var checked int
	for rows.Next() {
		var table, name, dataType string
		var defaultValue *string
		if err := rows.Scan(&table, &name, &dataType, &defaultValue); err != nil {
			t.Fatal(err)
		}
		if dataType != "text" || (name == "id" && defaultValue != nil) {
			t.Fatalf("%s.%s 的标识类型或生成规则错误", table, name)
		}
		checked++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("基线未创建资源标识列")
	}
	const user, account, session, flow = "user_custom", "account_custom", "session_custom", "flow_custom"
	for _, fixture := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO users(id,display_name,status) VALUES($1,'迁移用户','active')", []any{user}},
		{"INSERT INTO accounts(id,user_id,provider_id,provider_namespace,provider_account_id) VALUES($1,$2,'github','github','seed-account')", []any{account, user}},
		{"INSERT INTO user_sessions(id,user_id,token_hash,auth_method,auth_source_id,auth_version,idle_expires_at,absolute_expires_at) VALUES($1,$2,$3,'oauth',$4,1,now()+interval '1 hour',now()+interval '1 day')", []any{session, user, []byte("session-digest"), account}},
		{"INSERT INTO auth_flows(id,purpose,user_id,session_id,account_id,reauthentication_id,claimed_by,expires_at) VALUES($1,'reauthenticate',$2,$3,$4,$1,$1,now()+interval '1 hour')", []any{flow, user, session, account}},
		{"INSERT INTO auth_verifications(id,user_id,purpose,token_hash,email,auth_version,session_id,reauthentication_id,expires_at) VALUES($1,$2,'change_email',$3,'test@example.com',1,$4,$5,now()+interval '1 hour')", []any{"verification_custom", user, []byte("verification-digest"), session, flow}},
		{"INSERT INTO audit_events(id,action,outcome,actor_type,resource_type,session_id) VALUES($1,'migration.test','success','system','session',$2)", []any{"audit_custom", session}},
		{"INSERT INTO projects(id,owner_id,name) VALUES($1,$2,'迁移项目')", []any{"project_custom", user}},
		{"INSERT INTO tasks(id,owner_id,title) VALUES($1,$2,'迁移任务')", []any{"task_custom", user}},
		{"INSERT INTO mail_outbox(id,kind,recipient,subject,body,lease_id,expires_at) VALUES($1,'test','test@example.com','测试','<p>测试</p>',$2,now()+interval '1 hour')", []any{"mail_custom", "lease_custom"}},
	} {
		if _, err := pool.Exec(ctx, fixture.sql, fixture.args...); err != nil {
			t.Fatal(err)
		}
	}
	// 字符串外键仍拒绝孤立记录，级联删除仍覆盖账户和会话。
	_, err = pool.Exec(ctx, "INSERT INTO accounts(id,user_id,provider_id,provider_namespace,provider_account_id) VALUES('account_orphan','missing_user','github','github','orphan')")
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "23503" {
		t.Fatalf("基线外键未生效: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id=$1", user); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM accounts) + (SELECT count(*) FROM user_sessions)").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("基线级联删除失效")
	}
}
