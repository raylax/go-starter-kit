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
	request(alice, http.MethodGet, "/v1/projects", "", false, http.StatusUnauthorized)
	request(alice, http.MethodPost, "/v1/projects", `{"name":"   "}`, true, http.StatusUnprocessableEntity)
	request(alice, http.MethodPost, "/v1/projects", `{"name":"x","unknown":1}`, true, http.StatusUnprocessableEntity)
	request(alice, http.MethodPost, "/v1/projects", `{`, true, http.StatusBadRequest)
	request(alice, http.MethodPost, "/v1/projects", `{"name":"`+strings.Repeat("a", (1<<20)+1)+`"}`, true, http.StatusRequestEntityTooLarge)
	request(alice, http.MethodGet, "/v1/projects?limit=101", "", true, http.StatusUnprocessableEntity)
	request(alice, http.MethodGet, "/v1/projects?limit=0", "", true, http.StatusUnprocessableEntity)
	request(alice, http.MethodGet, "/v1/projects?unexpected=1", "", true, http.StatusUnprocessableEntity)
	request(alice, http.MethodGet, "/v1/projects/not-a-uuid", "", true, http.StatusUnprocessableEntity)
	created := request(alice, http.MethodPost, "/v1/projects", `{"name":"  中文项目  ","description":"first"}`, true, http.StatusCreated)
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
	request(alice, http.MethodGet, path, "", true, http.StatusOK)
	request(alice, http.MethodPost, "/v1/projects", `{"name":"中文项目"}`, true, http.StatusConflict)
	request(bob, http.MethodGet, path, "", true, http.StatusNotFound)
	request(bob, http.MethodPut, path, `{"name":"stolen"}`, true, http.StatusNotFound)
	request(bob, http.MethodDelete, path, "", true, http.StatusNotFound)
	otherList := request(bob, http.MethodGet, "/v1/projects", "", true, http.StatusOK)
	if !strings.Contains(string(otherList), `"items":[]`) {
		t.Fatalf("expected empty isolated list: %s", otherList)
	}
	request(bob, http.MethodPost, "/v1/projects", `{"name":"中文项目"}`, true, http.StatusCreated)
	request(alice, http.MethodPost, "/v1/projects", `{"name":"second"}`, true, http.StatusCreated)
	request(alice, http.MethodPut, path, `{"name":"second"}`, true, http.StatusConflict)
	updated := request(alice, http.MethodPut, path, `{"name":"updated","description":"changed"}`, true, http.StatusOK)
	if !strings.Contains(string(updated), `"description":"changed"`) {
		t.Fatalf("update not persisted: %s", updated)
	}
	firstPage := request(alice, http.MethodGet, "/v1/projects?limit=1", "", true, http.StatusOK)
	secondPage := request(alice, http.MethodGet, "/v1/projects?limit=1&offset=1", "", true, http.StatusOK)
	var page1, page2 project.ProjectListResponse
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
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, alice.URL+"/v1/projects", strings.NewReader(`{"name":"concurrent"}`))
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
		if counts[http.StatusCreated] != 1 || counts[http.StatusConflict] != 1 {
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
		request(short, http.MethodPut, path, `{"name":"blocked"}`, true, http.StatusGatewayTimeout)
	})
	request(alice, http.MethodDelete, path, "", true, http.StatusNoContent)
	request(alice, http.MethodGet, path, "", true, http.StatusNotFound)
	request(alice, http.MethodDelete, path, "", true, http.StatusNotFound)

}
