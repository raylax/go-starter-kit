//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	app "github.com/example/go-starter-kit/internal/app/api"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/tests/integration/testutil"
)

type openAPIContract struct {
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}

func TestApplicationAndMigrations(t *testing.T) {
	ctx := t.Context()
	pool, databaseURL := testutil.Database(t)
	deps := applicationServices(t, pool)
	deps.Authenticator = testutil.Authenticator{Subject: "alice"}
	handler, _, err := app.NewHandler(app.Config{RequestTimeout: 5 * time.Second, DocsEnabled: true}, slog.New(slog.NewJSONHandler(io.Discard, nil)), deps)
	if err != nil {
		t.Fatal(err)
	}
	alice := httptest.NewServer(handler)
	t.Cleanup(alice.Close)
	request := testutil.Request(t)
	request(alice, http.MethodGet, "/health/ready", "", false, http.StatusOK)
	request(alice, http.MethodPost, "/v1/projects", `{"name":"保留的项目"}`, true, http.StatusCreated)
	request(alice, http.MethodPost, "/v1/tasks", `{"title":"测试任务"}`, true, http.StatusCreated)
	spec := request(alice, http.MethodGet, "/openapi.json", "", false, http.StatusOK)
	var contract openAPIContract
	if err := json.Unmarshal(spec, &contract); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []struct{ path, method string }{
		{"/v1/projects", "post"}, {"/v1/projects", "get"}, {"/v1/projects/{id}", "get"}, {"/v1/projects/{id}", "put"}, {"/v1/projects/{id}", "delete"},
		{"/v1/tasks", "post"}, {"/v1/tasks", "get"}, {"/v1/tasks/{id}", "get"}, {"/v1/tasks/{id}", "put"}, {"/v1/tasks/{id}", "delete"},
	} {
		if len(contract.Paths[endpoint.path][endpoint.method]) == 0 {
			t.Fatal(fmt.Sprintf("missing contract: %s %s", endpoint.method, endpoint.path))
		}
	}
	// 基线一次创建全部业务表，重复执行 up 不应修改已有记录。
	createRecords := func(label string) {
		t.Helper()
		for _, resource := range []struct{ table, field string }{{"projects", "name"}, {"tasks", "title"}} {
			id := uuid.New()
			var storedID uuid.UUID
			query := fmt.Sprintf("INSERT INTO %s (id, owner_id, %s) VALUES ($1, 'alice', $2) RETURNING id", resource.table, resource.field)
			if err := pool.QueryRow(ctx, query, id, label).Scan(&storedID); err != nil {
				t.Fatal(err)
			}
			if storedID != id {
				t.Fatalf("%s 未保存应用传入的 ID", resource.table)
			}
		}
	}
	tables := []string{"projects", "tasks", "users", "accounts", "user_sessions", "auth_flows", "auth_verifications", "audit_events", "auth_rate_limits", "mail_outbox"}
	assertTables := func(present bool) {
		t.Helper()
		for _, name := range tables {
			var table *string
			if err := pool.QueryRow(ctx, "SELECT to_regclass($1)::text", "public."+name).Scan(&table); err != nil {
				t.Fatal(err)
			}
			if (table != nil) != present {
				t.Fatalf("表 %s 状态不符", name)
			}
		}
	}
	assertTables(true)
	createRecords("初始化记录")
	if err := db.Migrate(ctx, databaseURL, "up"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM projects").Scan(&count); err != nil || count != 2 {
		t.Fatalf("重复迁移改变了记录: %d %v", count, err)
	}
	var version int64
	if err := pool.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil || version != 1 {
		t.Fatalf("基线版本错误: %d %v", version, err)
	}
	if err := db.Migrate(ctx, databaseURL, "down"); err != nil {
		t.Fatal(err)
	}
	assertTables(false)
	if err := db.Migrate(ctx, databaseURL, "up"); err != nil {
		t.Fatal(err)
	}
	assertTables(true)
	createRecords("重建后的记录")
	request(alice, http.MethodGet, "/v1/projects", "", true, http.StatusOK)
	request(alice, http.MethodGet, "/v1/tasks", "", true, http.StatusOK)
	pool.Close()
	request(alice, http.MethodGet, "/health/ready", "", false, http.StatusServiceUnavailable)
	request(alice, http.MethodGet, "/health/live", "", false, http.StatusOK)
}
