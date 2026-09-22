//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/example/go-starter-kit/app"
	"github.com/example/go-starter-kit/db"
	"github.com/example/go-starter-kit/identity"
	"github.com/example/go-starter-kit/tests/testutil"
)

func TestApplicationAndMigrations(t *testing.T) {
	ctx := t.Context()
	pool, databaseURL := testutil.Database(t)
	authenticator, err := identity.NewDevelopment("integration-only-token", "alice")
	if err != nil {
		t.Fatal(err)
	}
	handler, _, err := app.NewHandler(app.Config{RequestTimeout: 5 * time.Second, DocsEnabled: true}, slog.New(slog.NewJSONHandler(io.Discard, nil)), app.NewDependencies(pool, authenticator, pool.Ping))
	if err != nil {
		t.Fatal(err)
	}
	alice := httptest.NewServer(handler)
	t.Cleanup(alice.Close)
	request := testutil.Request(t)
	request(alice, "GET", "/health/ready", "", false, 200)
	request(alice, "POST", "/v1/projects", `{"name":"保留的项目"}`, true, 201)
	request(alice, "POST", "/v1/tasks", `{"title":"测试任务"}`, true, 201)
	spec := request(alice, "GET", "/openapi.json", "", false, 200)
	var contract struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
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
	// 验证 UUID 迁移的升降级以及两种版本的数据共存。
	createWithDefault := func(version uuid.Version, label string) map[string]uuid.UUID {
		t.Helper()
		ids := make(map[string]uuid.UUID)
		for _, resource := range []struct{ table, field string }{{"projects", "name"}, {"tasks", "title"}} {
			var id uuid.UUID
			query := fmt.Sprintf("INSERT INTO %s (owner_id, %s) VALUES ('alice', $1) RETURNING id", resource.table, resource.field)
			if err := pool.QueryRow(ctx, query, label).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if id.Version() != version {
				t.Fatalf("%s 默认 ID 版本错误：期望 %d，实际 %d", resource.table, version, id.Version())
			}
			ids[resource.table] = id
		}
		return ids
	}
	currentIDs := createWithDefault(7, "升级后的记录")
	if err := db.Migrate(ctx, databaseURL, "down"); err != nil {
		t.Fatal(err)
	}
	legacyIDs := createWithDefault(4, "旧版本生成的记录")
	if err := db.Migrate(ctx, databaseURL, "up"); err != nil {
		t.Fatal(err)
	}
	createWithDefault(7, "重新升级后的记录")
	for _, ids := range []map[string]uuid.UUID{currentIDs, legacyIDs} {
		for table, id := range ids {
			data := request(alice, "GET", "/v1/"+table+"/"+id.String(), "", true, 200)
			var item struct{ ID uuid.UUID }
			if err := json.Unmarshal(data, &item); err != nil || item.ID != id {
				t.Fatal("迁移改写了已有 ID 或旧记录无法读取")
			}
		}
	}
	// 先撤销 UUID 默认值迁移，再验证任务表的独立回滚。
	if err := db.Migrate(ctx, databaseURL, "down"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, databaseURL, "down"); err != nil {
		t.Fatal(err)
	}
	var table *string
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.tasks')::text").Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != nil {
		t.Fatal("task down migration did not remove table")
	}
	var projectCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM projects").Scan(&projectCount); err != nil {
		t.Fatal(err)
	}
	if projectCount == 0 {
		t.Fatal("task rollback lost existing project data")
	}
	if err := db.Migrate(ctx, databaseURL, "up"); err != nil {
		t.Fatal(err)
	}
	request(alice, "GET", "/v1/tasks", "", true, 200)
	// 回滚三个迁移，再从空数据库重建完整表结构。
	for range 3 {
		if err := db.Migrate(ctx, databaseURL, "down"); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.projects')::text").Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != nil {
		t.Fatal("down migration did not remove table")
	}
	if err := db.Migrate(ctx, databaseURL, "up"); err != nil {
		t.Fatal(err)
	}
	createWithDefault(7, "重建后的记录")
	request(alice, "GET", "/v1/projects", "", true, 200)
	request(alice, "GET", "/v1/tasks", "", true, 200)
	pool.Close()
	request(alice, "GET", "/health/ready", "", false, 503)
	request(alice, "GET", "/health/live", "", false, 200)
}
