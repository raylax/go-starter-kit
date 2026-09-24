//go:build integration

package task_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/go-starter-kit/internal/modules/task"
	"github.com/example/go-starter-kit/tests/integration/testutil"
)

func TestTaskCRUD(t *testing.T) {
	pool, _ := testutil.Database(t)
	service := task.NewService(pool)
	alice := testutil.ModuleServer(t, "alice", 5*time.Second, service, task.Routes())
	bob := testutil.ModuleServer(t, "bob", 5*time.Second, service, task.Routes())
	request := testutil.Request(t)
	request(alice, http.MethodGet, "/v1/tasks", "", false, http.StatusUnauthorized)
	request(alice, http.MethodPost, "/v1/tasks", `{"title":"task"}`, false, http.StatusUnauthorized)
	for _, body := range []string{
		`{}`, `{"title":"   "}`, `{"title":"task","status":"archived"}`,
		`{"title":"task","owner_id":"bob"}`, `{"title":"` + strings.Repeat("中", 201) + `"}`,
	} {
		request(alice, http.MethodPost, "/v1/tasks", body, true, http.StatusUnprocessableEntity)
	}
	request(alice, http.MethodPost, "/v1/tasks", `{`, true, http.StatusBadRequest)
	request(alice, http.MethodPost, "/v1/tasks", `{"title":"`+strings.Repeat("a", (1<<20)+1)+`"}`, true, http.StatusRequestEntityTooLarge)
	for _, query := range []string{"status=invalid", "limit=0", "limit=101", "offset=-1", "offset=10001", "unknown=1"} {
		request(alice, http.MethodGet, "/v1/tasks?"+query, "", true, http.StatusUnprocessableEntity)
	}
	request(alice, http.MethodGet, "/v1/tasks/not-a-uuid", "", true, http.StatusUnprocessableEntity)
	decode := func(data []byte) task.Task {
		t.Helper()
		var item task.Task
		if err := json.Unmarshal(data, &item); err != nil {
			t.Fatal(err)
		}
		return item
	}
	created := request(alice, http.MethodPost, "/v1/tasks", `{"title":"  编写文档  ","description":"first"}`, true, http.StatusCreated)
	item := decode(created)
	if item.Title != "编写文档" || item.Status != task.Todo || item.CreatedAt.IsZero() {
		t.Fatalf("unexpected task: %+v", item)
	}
	if item.ID.Version() != 4 {
		t.Fatalf("新建资源 ID 应为 UUID v4，实际为 %s", item.ID)
	}
	if strings.Contains(string(created), "owner_id") {
		t.Fatal("owner_id leaked")
	}
	path := "/v1/tasks/" + item.ID.String()
	if got := decode(request(alice, http.MethodGet, path, "", true, http.StatusOK)); got.ID != item.ID || got.Description != "first" {
		t.Fatalf("incorrect persisted task: %+v", got)
	}
	request(bob, http.MethodGet, path, "", true, http.StatusNotFound)
	request(bob, http.MethodPut, path, `{"title":"stolen","status":"done"}`, true, http.StatusNotFound)
	request(bob, http.MethodDelete, path, "", true, http.StatusNotFound)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		request(alice, method, path, "", false, http.StatusUnauthorized)
	}
	for _, body := range []string{`{"title":"task"}`, `{"title":"task","status":""}`, `{"title":"task","status":"archived"}`} {
		request(alice, http.MethodPut, path, body, true, http.StatusUnprocessableEntity)
	}
	updated := decode(request(alice, http.MethodPut, path, `{"title":"文档完成","status":"done"}`, true, http.StatusOK))
	if updated.Title != "文档完成" || updated.Status != task.Done || updated.Description != "" || updated.UpdatedAt.Before(item.UpdatedAt) {
		t.Fatalf("incorrect update: %+v", updated)
	}
	if got := decode(request(alice, http.MethodGet, path, "", true, http.StatusOK)); got.Status != task.Done || got.Description != "" {
		t.Fatal("update was not persisted")
	}
	// 任务标题允许重复，任务无需关联项目。
	request(alice, http.MethodPost, "/v1/tasks", `{"title":"文档完成","status":"done"}`, true, http.StatusCreated)
	request(alice, http.MethodPost, "/v1/tasks", `{"title":"另一项","status":"in_progress"}`, true, http.StatusCreated)
	request(bob, http.MethodPost, "/v1/tasks", `{"title":"其他用户","status":"done"}`, true, http.StatusCreated)
	list := func(server *httptest.Server, query string) task.TaskListResponse {
		t.Helper()
		var result task.TaskListResponse
		data := request(server, http.MethodGet, "/v1/tasks"+query, "", true, http.StatusOK)
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if all := list(alice, ""); len(all.Items) != 3 || all.Limit != 20 || all.Offset != 0 {
		t.Fatalf("invalid unfiltered list: %+v", all)
	}
	first, second := list(alice, "?status=done&limit=1"), list(alice, "?status=done&limit=1&offset=1")
	if len(first.Items) != 1 || len(second.Items) != 1 || !first.HasMore || second.HasMore || first.Items[0].ID == second.Items[0].ID || first.Items[0].Status != task.Done || second.Items[0].Status != task.Done {
		t.Fatalf("incorrect filtered pagination: %+v %+v", first, second)
	}
	if other := list(bob, "?status=done"); len(other.Items) != 1 || other.Items[0].Title != "其他用户" {
		t.Fatalf("owner filter was bypassed: %+v", other)
	}
	if empty := list(alice, "?status=todo"); empty.Items == nil || len(empty.Items) != 0 || empty.HasMore {
		t.Fatalf("invalid empty page: %+v", empty)
	}
	if progress := list(alice, "?status=in_progress"); len(progress.Items) != 1 || progress.Items[0].Status != task.InProgress {
		t.Fatalf("invalid status filter: %+v", progress)
	}
	request(alice, http.MethodDelete, path, "", true, http.StatusNoContent)
	request(alice, http.MethodGet, path, "", true, http.StatusNotFound)
	request(alice, http.MethodPut, path, `{"title":"deleted","status":"todo"}`, true, http.StatusNotFound)
	request(alice, http.MethodDelete, path, "", true, http.StatusNotFound)
}
