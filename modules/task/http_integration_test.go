//go:build integration

package task_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/go-starter-kit/modules/task"
	"github.com/example/go-starter-kit/tests/testutil"
)

func TestTaskCRUD(t *testing.T) {
	pool, _ := testutil.Database(t)
	service := task.NewService(pool)
	alice := testutil.ModuleServer(t, "alice", 5*time.Second, service, task.Routes())
	bob := testutil.ModuleServer(t, "bob", 5*time.Second, service, task.Routes())
	request := testutil.Request(t)
	request(alice, "GET", "/v1/tasks", "", false, 401)
	request(alice, "POST", "/v1/tasks", `{"title":"task"}`, false, 401)
	for _, body := range []string{
		`{}`, `{"title":"   "}`, `{"title":"task","status":"archived"}`,
		`{"title":"task","owner_id":"bob"}`, `{"title":"` + strings.Repeat("中", 201) + `"}`,
	} {
		request(alice, "POST", "/v1/tasks", body, true, 422)
	}
	request(alice, "POST", "/v1/tasks", `{`, true, 400)
	request(alice, "POST", "/v1/tasks", `{"title":"`+strings.Repeat("a", (1<<20)+1)+`"}`, true, 413)
	for _, query := range []string{"status=invalid", "limit=0", "limit=101", "offset=-1", "offset=10001", "unknown=1"} {
		request(alice, "GET", "/v1/tasks?"+query, "", true, 422)
	}
	request(alice, "GET", "/v1/tasks/not-a-uuid", "", true, 422)
	decode := func(data []byte) task.Task {
		t.Helper()
		var item task.Task
		if err := json.Unmarshal(data, &item); err != nil {
			t.Fatal(err)
		}
		return item
	}
	created := request(alice, "POST", "/v1/tasks", `{"title":"  编写文档  ","description":"first"}`, true, 201)
	item := decode(created)
	if item.Title != "编写文档" || item.Status != task.Todo || item.CreatedAt.IsZero() {
		t.Fatalf("unexpected task: %+v", item)
	}
	if item.ID.Version() != 7 {
		t.Fatalf("新建资源 ID 应为 UUID v7，实际为 %s", item.ID)
	}
	if strings.Contains(string(created), "owner_id") {
		t.Fatal("owner_id leaked")
	}
	path := "/v1/tasks/" + item.ID.String()
	if got := decode(request(alice, "GET", path, "", true, 200)); got.ID != item.ID || got.Description != "first" {
		t.Fatalf("incorrect persisted task: %+v", got)
	}
	request(bob, "GET", path, "", true, 404)
	request(bob, "PUT", path, `{"title":"stolen","status":"done"}`, true, 404)
	request(bob, "DELETE", path, "", true, 404)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		request(alice, method, path, "", false, 401)
	}
	for _, body := range []string{`{"title":"task"}`, `{"title":"task","status":""}`, `{"title":"task","status":"archived"}`} {
		request(alice, "PUT", path, body, true, 422)
	}
	updated := decode(request(alice, "PUT", path, `{"title":"文档完成","status":"done"}`, true, 200))
	if updated.Title != "文档完成" || updated.Status != task.Done || updated.Description != "" || updated.UpdatedAt.Before(item.UpdatedAt) {
		t.Fatalf("incorrect update: %+v", updated)
	}
	if got := decode(request(alice, "GET", path, "", true, 200)); got.Status != task.Done || got.Description != "" {
		t.Fatal("update was not persisted")
	}
	// 任务标题允许重复，任务无需关联项目。
	request(alice, "POST", "/v1/tasks", `{"title":"文档完成","status":"done"}`, true, 201)
	request(alice, "POST", "/v1/tasks", `{"title":"另一项","status":"in_progress"}`, true, 201)
	request(bob, "POST", "/v1/tasks", `{"title":"其他用户","status":"done"}`, true, 201)
	type page struct {
		Items   []task.Task `json:"items"`
		HasMore bool        `json:"has_more"`
		Limit   int32       `json:"limit"`
		Offset  int32       `json:"offset"`
	}
	list := func(server *httptest.Server, query string) page {
		t.Helper()
		var result page
		data := request(server, "GET", "/v1/tasks"+query, "", true, 200)
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
	request(alice, "DELETE", path, "", true, 204)
	request(alice, "GET", path, "", true, 404)
	request(alice, "PUT", path, `{"title":"deleted","status":"todo"}`, true, 404)
	request(alice, "DELETE", path, "", true, 404)
}
