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

func TestTaskAuthentication(t *testing.T) {
	fixture := newTaskFixture(t)
	item := createTask(t, fixture.alice, task.TaskCreateRequest{Title: "需要登录的任务"})
	testutil.Request(t, fixture.alice, http.MethodGet, "/v1/tasks", "", false, http.StatusUnauthorized)
	testutil.Request(t, fixture.alice, http.MethodPost, "/v1/tasks", `{"title":"task"}`, false, http.StatusUnauthorized)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		testutil.Request(t, fixture.alice, method, "/v1/tasks/"+item.ID.String(), "", false, http.StatusUnauthorized)
	}
}

func TestTaskValidation(t *testing.T) {
	fixture := newTaskFixture(t)
	alice := fixture.alice
	for _, body := range []string{
		`{}`, `{"title":"   "}`, `{"title":"task","status":"archived"}`,
		`{"title":"task","owner_id":"bob"}`, `{"title":"` + strings.Repeat("中", 201) + `"}`,
	} {
		testutil.Request(t, alice, http.MethodPost, "/v1/tasks", body, true, http.StatusUnprocessableEntity)
	}
	const oversizedRequestBodyBytes = 1024*1024 + 1 // 超过默认 1 MiB 请求体上限。
	testutil.Request(t, alice, http.MethodPost, "/v1/tasks", `{`, true, http.StatusBadRequest)
	testutil.Request(t, alice, http.MethodPost, "/v1/tasks", `{"title":"`+strings.Repeat("a", oversizedRequestBodyBytes)+`"}`, true, http.StatusRequestEntityTooLarge)
	for _, query := range []string{"status=invalid", "limit=0", "limit=101", "offset=-1", "offset=10001", "unknown=1"} {
		testutil.Request(t, alice, http.MethodGet, "/v1/tasks?"+query, "", true, http.StatusUnprocessableEntity)
	}
	testutil.Request(t, alice, http.MethodGet, "/v1/tasks/not-a-uuid", "", true, http.StatusUnprocessableEntity)
	item := createTask(t, alice, task.TaskCreateRequest{Title: "用于更新校验"})
	for _, body := range []string{`{"title":"task"}`, `{"title":"task","status":""}`, `{"title":"task","status":"archived"}`} {
		testutil.Request(t, alice, http.MethodPut, "/v1/tasks/"+item.ID.String(), body, true, http.StatusUnprocessableEntity)
	}
}

func TestTaskCRUD(t *testing.T) {
	fixture := newTaskFixture(t)
	alice := fixture.alice
	created := testutil.Request(t, alice, http.MethodPost, "/v1/tasks", `{"title":"  编写文档  ","description":"first"}`, true, http.StatusCreated)
	item := decodeTask(t, created)
	if item.Title != "编写文档" || item.Status != task.Todo || item.CreatedAt.IsZero() {
		t.Fatalf("创建的任务不符合预期: %+v", item)
	}
	if item.ID.Version() != 4 {
		t.Fatalf("新建资源 ID 应为 UUID v4，实际为 %s", item.ID)
	}
	if strings.Contains(string(created), "owner_id") {
		t.Fatal("响应泄漏了数据库归属字段")
	}
	path := "/v1/tasks/" + item.ID.String()
	if got := decodeTask(t, testutil.Request(t, alice, http.MethodGet, path, "", true, http.StatusOK)); got.ID != item.ID || got.Description != "first" {
		t.Fatalf("读取的任务不符合预期: %+v", got)
	}
	updated := decodeTask(t, testutil.Request(t, alice, http.MethodPut, path, `{"title":"文档完成","status":"done"}`, true, http.StatusOK))
	if updated.Title != "文档完成" || updated.Status != task.Done || updated.Description != "" || updated.UpdatedAt.Before(item.UpdatedAt) {
		t.Fatalf("更新结果不符合预期: %+v", updated)
	}
	if got := decodeTask(t, testutil.Request(t, alice, http.MethodGet, path, "", true, http.StatusOK)); got.Status != task.Done || got.Description != "" {
		t.Fatalf("更新未持久化: %+v", got)
	}
	// 任务标题允许重复，任务无需关联项目。
	createTask(t, alice, task.TaskCreateRequest{Title: updated.Title, Status: task.Done})
	testutil.Request(t, alice, http.MethodDelete, path, "", true, http.StatusNoContent)
	testutil.Request(t, alice, http.MethodGet, path, "", true, http.StatusNotFound)
	testutil.Request(t, alice, http.MethodPut, path, `{"title":"deleted","status":"todo"}`, true, http.StatusNotFound)
	testutil.Request(t, alice, http.MethodDelete, path, "", true, http.StatusNotFound)
}

func TestTaskOwnership(t *testing.T) {
	fixture := newTaskFixture(t)
	bob := testutil.ModuleServer(t, "bob", 5*time.Second, fixture.service, task.Routes())
	item := createTask(t, fixture.alice, task.TaskCreateRequest{Title: "归属任务", Status: task.Done})
	path := "/v1/tasks/" + item.ID.String()
	testutil.Request(t, bob, http.MethodGet, path, "", true, http.StatusNotFound)
	testutil.Request(t, bob, http.MethodPut, path, `{"title":"stolen","status":"done"}`, true, http.StatusNotFound)
	testutil.Request(t, bob, http.MethodDelete, path, "", true, http.StatusNotFound)
	otherItem := createTask(t, bob, task.TaskCreateRequest{Title: "其他用户", Status: task.Done})
	if other := listTasks(t, bob, ""); len(other.Items) != 1 || other.Items[0].ID != otherItem.ID {
		t.Fatalf("默认列表未按归属过滤: %+v", other)
	}
	if own := listTasks(t, fixture.alice, ""); len(own.Items) != 1 || own.Items[0].ID != item.ID {
		t.Fatalf("默认列表包含了其他用户的任务: %+v", own)
	}
	if other := listTasks(t, bob, "?status=done"); len(other.Items) != 1 || other.Items[0].ID != otherItem.ID {
		t.Fatalf("列表未按归属过滤: %+v", other)
	}
	if own := listTasks(t, fixture.alice, "?status=done"); len(own.Items) != 1 || own.Items[0].ID != item.ID || own.Items[0].Title != item.Title {
		t.Fatalf("其他用户改变了任务或列表归属: %+v", own)
	}
}

func TestTaskPagination(t *testing.T) {
	fixture := newTaskFixture(t)
	firstDone := createTask(t, fixture.alice, task.TaskCreateRequest{Title: "已完成一", Status: task.Done})
	secondDone := createTask(t, fixture.alice, task.TaskCreateRequest{Title: "已完成二", Status: task.Done})
	inProgress := createTask(t, fixture.alice, task.TaskCreateRequest{Title: "进行中", Status: task.InProgress})
	if all := listTasks(t, fixture.alice, ""); len(all.Items) != 3 || all.Limit != 20 || all.Offset != 0 {
		t.Fatalf("默认列表参数或记录数错误: %+v", all)
	}
	first := listTasks(t, fixture.alice, "?status=done&limit=1")
	second := listTasks(t, fixture.alice, "?status=done&limit=1&offset=1")
	if len(first.Items) != 1 || len(second.Items) != 1 || !first.HasMore || second.HasMore || first.Items[0].ID == second.Items[0].ID || first.Items[0].Status != task.Done || second.Items[0].Status != task.Done {
		t.Fatalf("筛选分页的顺序或边界错误: %+v %+v", first, second)
	}
	seen := map[string]bool{first.Items[0].ID.String(): true, second.Items[0].ID.String(): true}
	if !seen[firstDone.ID.String()] || !seen[secondDone.ID.String()] {
		t.Fatal("分页遗漏了当前场景创建的已完成任务")
	}
	if empty := listTasks(t, fixture.alice, "?status=todo"); empty.Items == nil || len(empty.Items) != 0 || empty.HasMore {
		t.Fatalf("空分页响应错误: %+v", empty)
	}
	if progress := listTasks(t, fixture.alice, "?status=in_progress"); len(progress.Items) != 1 || progress.Items[0].ID != inProgress.ID || progress.Items[0].Status != task.InProgress {
		t.Fatalf("状态筛选错误: %+v", progress)
	}
}

type taskFixture struct {
	service *task.Service
	alice   *httptest.Server
}

func newTaskFixture(t *testing.T) taskFixture {
	t.Helper()
	pool, _ := testutil.Database(t)
	service := task.NewService(pool)
	alice := testutil.ModuleServer(t, "alice", 5*time.Second, service, task.Routes())
	return taskFixture{service: service, alice: alice}
}

func createTask(t *testing.T, server *httptest.Server, input task.TaskCreateRequest) task.Task {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return decodeTask(t, testutil.Request(t, server, http.MethodPost, "/v1/tasks", string(body), true, http.StatusCreated))
}

func decodeTask(t *testing.T, data []byte) task.Task {
	t.Helper()
	var item task.Task
	if err := json.Unmarshal(data, &item); err != nil {
		t.Fatal(err)
	}
	return item
}

func listTasks(t *testing.T, server *httptest.Server, query string) task.TaskListResponse {
	t.Helper()
	data := testutil.Request(t, server, http.MethodGet, "/v1/tasks"+query, "", true, http.StatusOK)
	var result task.TaskListResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
