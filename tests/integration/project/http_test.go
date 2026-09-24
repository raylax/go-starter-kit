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

	"github.com/example/go-starter-kit/internal/modules/project"
	"github.com/example/go-starter-kit/tests/integration/testutil"
)

func TestProjectCRUD(t *testing.T) {
	pool, _ := testutil.Database(t)
	service := project.NewService(pool)
	newServer := func(subject string, timeout time.Duration) *httptest.Server {
		return testutil.ModuleServer(t, subject, timeout, service, project.Routes())
	}
	alice, bob := newServer("alice", 5*time.Second), newServer("bob", 5*time.Second)
	request := testutil.Request(t)
	request(alice, "GET", "/v1/projects", "", false, 401)
	request(alice, "POST", "/v1/projects", `{"name":"   "}`, true, 422)
	request(alice, "POST", "/v1/projects", `{"name":"x","unknown":1}`, true, 422)
	request(alice, "POST", "/v1/projects", `{`, true, 400)
	request(alice, "POST", "/v1/projects", `{"name":"`+strings.Repeat("a", (1<<20)+1)+`"}`, true, 413)
	request(alice, "GET", "/v1/projects?limit=101", "", true, 422)
	request(alice, "GET", "/v1/projects?limit=0", "", true, 422)
	request(alice, "GET", "/v1/projects?unexpected=1", "", true, 422)
	request(alice, "GET", "/v1/projects/not-a-uuid", "", true, 422)
	created := request(alice, "POST", "/v1/projects", `{"name":"  中文项目  ","description":"first"}`, true, 201)
	var item project.Project
	if err := json.Unmarshal(created, &item); err != nil {
		t.Fatal(err)
	}
	if item.Name != "中文项目" || item.CreatedAt.IsZero() {
		t.Fatalf("unexpected project: %+v", item)
	}
	if item.ID.Version() != 4 {
		t.Fatalf("新建资源 ID 应为 UUID v4，实际为 %s", item.ID)
	}
	if strings.Contains(string(created), "owner_id") {
		t.Fatal("database owner field leaked")
	}
	path := "/v1/projects/" + item.ID.String()
	request(alice, "GET", path, "", true, 200)
	request(alice, "POST", "/v1/projects", `{"name":"中文项目"}`, true, 409)
	request(bob, "GET", path, "", true, 404)
	request(bob, "PUT", path, `{"name":"stolen"}`, true, 404)
	request(bob, "DELETE", path, "", true, 404)
	otherList := request(bob, "GET", "/v1/projects", "", true, 200)
	if !strings.Contains(string(otherList), `"items":[]`) {
		t.Fatalf("expected empty isolated list: %s", otherList)
	}
	request(bob, "POST", "/v1/projects", `{"name":"中文项目"}`, true, 201)
	request(alice, "POST", "/v1/projects", `{"name":"second"}`, true, 201)
	request(alice, "PUT", path, `{"name":"second"}`, true, 409)
	updated := request(alice, "PUT", path, `{"name":"updated","description":"changed"}`, true, 200)
	if !strings.Contains(string(updated), `"description":"changed"`) {
		t.Fatalf("update not persisted: %s", updated)
	}
	firstPage := request(alice, "GET", "/v1/projects?limit=1", "", true, 200)
	secondPage := request(alice, "GET", "/v1/projects?limit=1&offset=1", "", true, 200)
	var page1, page2 struct {
		Items   []project.Project `json:"items"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(firstPage, &page1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(secondPage, &page2); err != nil {
		t.Fatal(err)
	}
	if len(page1.Items) != 1 || len(page2.Items) != 1 || !page1.HasMore || page2.HasMore || page1.Items[0].ID == page2.Items[0].ID {
		t.Fatal("pagination lost its ordering/bounds")
	}

	t.Run("concurrent create has one winner", func(t *testing.T) {
		var group sync.WaitGroup
		statuses := make(chan int, 2)
		for range 2 {
			group.Go(func() {
				req, err := http.NewRequestWithContext(t.Context(), "POST", alice.URL+"/v1/projects", strings.NewReader(`{"name":"concurrent"}`))
				if err != nil {
					t.Error(err)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer integration-only-token")
				res, err := alice.Client().Do(req)
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
		if counts[201] != 1 || counts[409] != 1 {
			t.Fatalf("unexpected outcomes: %v", counts)
		}
	})

	t.Run("database deadline", func(t *testing.T) {
		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(t.Context(), "SELECT id FROM projects WHERE id=$1 FOR UPDATE", item.ID); err != nil {
			t.Fatal(err)
		}
		short := newServer("alice", 100*time.Millisecond)
		request(short, "PUT", path, `{"name":"blocked"}`, true, 504)
	})
	request(alice, "DELETE", path, "", true, 204)
	request(alice, "GET", path, "", true, 404)
	request(alice, "DELETE", path, "", true, 404)

}
