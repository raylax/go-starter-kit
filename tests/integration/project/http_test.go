//go:build integration

package project_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/go-starter-kit/internal/modules/project"
	"github.com/example/go-starter-kit/tests/integration/testutil"
)

func TestProjectValidation(t *testing.T) {
	fixture := newProjectFixture(t)
	alice := fixture.alice
	const oversizedRequestBodyBytes = 1024*1024 + 1 // 超过默认 1 MiB 请求体上限。
	testutil.Request(t, alice, http.MethodGet, "/v1/projects", "", false, http.StatusUnauthorized)
	testutil.Request(t, alice, http.MethodPost, "/v1/projects", `{"name":"   "}`, true, http.StatusUnprocessableEntity)
	testutil.Request(t, alice, http.MethodPost, "/v1/projects", `{"name":"x","unknown":1}`, true, http.StatusUnprocessableEntity)
	testutil.Request(t, alice, http.MethodPost, "/v1/projects", `{`, true, http.StatusBadRequest)
	testutil.Request(t, alice, http.MethodPost, "/v1/projects", `{"name":"`+strings.Repeat("a", oversizedRequestBodyBytes)+`"}`, true, http.StatusRequestEntityTooLarge)
	for _, query := range []string{"limit=101", "limit=0", "unexpected=1"} {
		testutil.Request(t, alice, http.MethodGet, "/v1/projects?"+query, "", true, http.StatusUnprocessableEntity)
	}
	testutil.Request(t, alice, http.MethodGet, "/v1/projects/not-a-uuid", "", true, http.StatusUnprocessableEntity)
}

func TestProjectCRUD(t *testing.T) {
	fixture := newProjectFixture(t)
	alice := fixture.alice
	created := testutil.Request(t, alice, http.MethodPost, "/v1/projects", `{"name":"  中文项目  ","description":"first"}`, true, http.StatusCreated)
	item := decodeProject(t, created)
	if item.Name != "中文项目" || item.CreatedAt.IsZero() {
		t.Fatalf("创建的项目不符合预期: %+v", item)
	}
	if item.ID.Version() != 4 {
		t.Fatalf("新建资源 ID 应为 UUID v4，实际为 %s", item.ID)
	}
	if strings.Contains(string(created), "owner_id") {
		t.Fatal("响应泄漏了数据库归属字段")
	}
	path := "/v1/projects/" + item.ID.String()
	if got := decodeProject(t, testutil.Request(t, alice, http.MethodGet, path, "", true, http.StatusOK)); got.ID != item.ID || got.Description != "first" {
		t.Fatalf("读取的项目不符合预期: %+v", got)
	}
	updated := decodeProject(t, testutil.Request(t, alice, http.MethodPut, path, `{"name":"updated","description":"changed"}`, true, http.StatusOK))
	if updated.Name != "updated" || updated.Description != "changed" {
		t.Fatalf("更新结果不符合预期: %+v", updated)
	}
	if got := decodeProject(t, testutil.Request(t, alice, http.MethodGet, path, "", true, http.StatusOK)); got.Name != updated.Name || got.Description != updated.Description {
		t.Fatalf("更新未持久化: %+v", got)
	}
	testutil.Request(t, alice, http.MethodDelete, path, "", true, http.StatusNoContent)
	testutil.Request(t, alice, http.MethodGet, path, "", true, http.StatusNotFound)
	testutil.Request(t, alice, http.MethodDelete, path, "", true, http.StatusNotFound)
}

func TestProjectOwnership(t *testing.T) {
	fixture := newProjectFixture(t)
	bob := testutil.ModuleServer(t, "bob", 5*time.Second, project.NewService(fixture.pool), project.Routes())
	item := createProject(t, fixture.alice, "归属项目")
	path := "/v1/projects/" + item.ID.String()
	testutil.Request(t, bob, http.MethodGet, path, "", true, http.StatusNotFound)
	testutil.Request(t, bob, http.MethodPut, path, `{"name":"stolen"}`, true, http.StatusNotFound)
	testutil.Request(t, bob, http.MethodDelete, path, "", true, http.StatusNotFound)
	otherList := listProjects(t, bob, "")
	if otherList.Items == nil || len(otherList.Items) != 0 {
		t.Fatalf("其他用户应得到空列表: %+v", otherList)
	}
	// 名称唯一性仅约束当前用户，其他用户可以创建同名项目。
	createProject(t, bob, item.Name)
	if got := decodeProject(t, testutil.Request(t, fixture.alice, http.MethodGet, path, "", true, http.StatusOK)); got.Name != item.Name {
		t.Fatalf("其他用户改变了项目: %+v", got)
	}
}

func TestProjectNameConflicts(t *testing.T) {
	fixture := newProjectFixture(t)
	first := createProject(t, fixture.alice, "  first  ")
	createProject(t, fixture.alice, "second")
	testutil.Request(t, fixture.alice, http.MethodPost, "/v1/projects", `{"name":"first"}`, true, http.StatusConflict)
	testutil.Request(t, fixture.alice, http.MethodPut, "/v1/projects/"+first.ID.String(), `{"name":"second"}`, true, http.StatusConflict)
}

func TestProjectPagination(t *testing.T) {
	fixture := newProjectFixture(t)
	first := createProject(t, fixture.alice, "first")
	second := createProject(t, fixture.alice, "second")
	page1 := listProjects(t, fixture.alice, "?limit=1")
	page2 := listProjects(t, fixture.alice, "?limit=1&offset=1")
	if len(page1.Items) != 1 || len(page2.Items) != 1 || !page1.HasMore || page2.HasMore || page1.Items[0].ID == page2.Items[0].ID {
		t.Fatalf("分页顺序或边界错误: %+v %+v", page1, page2)
	}
	seen := map[string]bool{page1.Items[0].ID.String(): true, page2.Items[0].ID.String(): true}
	if !seen[first.ID.String()] || !seen[second.ID.String()] {
		t.Fatal("分页遗漏了当前场景创建的项目")
	}
}

func TestProjectConcurrentCreate(t *testing.T) {
	fixture := newProjectFixture(t)
	var group sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		group.Go(func() {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fixture.alice.URL+"/v1/projects", strings.NewReader(`{"name":"concurrent"}`))
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer integration-only-token")
			res, err := fixture.alice.Client().Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			statuses <- res.StatusCode
		})
	}
	group.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("并发创建的结果错误: %v", counts)
	}
}

func TestProjectDatabaseDeadline(t *testing.T) {
	fixture := newProjectFixture(t)
	item := createProject(t, fixture.alice, "被锁定的项目")
	tx, err := fixture.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), "SELECT id FROM projects WHERE id=$1 FOR UPDATE", item.ID); err != nil {
		t.Fatal(err)
	}
	short := testutil.ModuleServer(t, "alice", 100*time.Millisecond, project.NewService(fixture.pool), project.Routes())
	testutil.Request(t, short, http.MethodPut, "/v1/projects/"+item.ID.String(), `{"name":"blocked"}`, true, http.StatusGatewayTimeout)
}

type projectFixture struct {
	pool  *pgxpool.Pool
	alice *httptest.Server
}

func newProjectFixture(t *testing.T) projectFixture {
	t.Helper()
	pool, _ := testutil.Database(t)
	alice := testutil.ModuleServer(t, "alice", 5*time.Second, project.NewService(pool), project.Routes())
	return projectFixture{pool: pool, alice: alice}
}

func createProject(t *testing.T, server *httptest.Server, name string) project.Project {
	t.Helper()
	body, err := json.Marshal(project.ProjectCreateRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return decodeProject(t, testutil.Request(t, server, http.MethodPost, "/v1/projects", string(body), true, http.StatusCreated))
}

func decodeProject(t *testing.T, data []byte) project.Project {
	t.Helper()
	var item project.Project
	if err := json.Unmarshal(data, &item); err != nil {
		t.Fatal(err)
	}
	return item
}

func listProjects(t *testing.T, server *httptest.Server, query string) project.ProjectListResponse {
	t.Helper()
	data := testutil.Request(t, server, http.MethodGet, "/v1/projects"+query, "", true, http.StatusOK)
	var result project.ProjectListResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
