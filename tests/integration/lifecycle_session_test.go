//go:build integration

package integration_test

import (
	"context"
	"fmt"
	app "github.com/example/go-starter-kit/internal/app/api"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/tests/integration/testutil"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServerLifecycle(t *testing.T) {
	pool, databaseURL := testutil.Database(t)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	token, digest := identity.NewToken("tk_")
	if _, err := pool.Exec(t.Context(), `WITH u AS (INSERT INTO users(id,status) VALUES($2,'active') RETURNING id,auth_version)
 INSERT INTO user_sessions(id,user_id,token_hash,auth_method,auth_source_id,auth_version,idle_expires_at,absolute_expires_at)
 SELECT $3,id,$1,'password',$4,auth_version,now()+interval '30 minutes',now()+interval '24 hours' FROM u`, digest, uuid.New(), uuid.New(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"APP_ENV": "test", "DATABASE_URL": databaseURL, "HTTP_ADDR": addr, "SHUTDOWN_TIMEOUT": "3s", "REQUEST_TIMEOUT": "5s", "OTEL_ENABLED": "false", "AUTH_PROVIDERS_FILE": "", "FRONTEND_URL": "https://web.example"} {
		t.Setenv(k, v)
	}
	cfg, err := app.Load()
	if err != nil {
		t.Fatal(err)
	}
	oldLogger := slog.Default()
	defer slog.SetDefault(oldLogger)
	canceled, cancelBefore := context.WithCancel(t.Context())
	cancelBefore()
	if e := app.Run(canceled, cfg); e == nil {
		t.Fatal("启动忽略取消")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, cfg) }()
	finished := false
	defer func() {
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("服务未退出")
			}
		}
	}()
	client := &http.Client{Timeout: 4 * time.Second}
	ready := false
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		res, e := client.Get("http://" + addr + "/health/ready")
		if e == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		select {
		case e := <-done:
			finished = true
			t.Fatalf("服务提前退出: %v", e)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("服务没有就绪")
	}
	tx, e := pool.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(t.Context(), "LOCK TABLE tasks IN ACCESS EXCLUSIVE MODE"); e != nil {
		t.Fatal(e)
	}
	response := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/v1/tasks", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, e := client.Do(req)
		if e == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				e = fmt.Errorf("在途请求状态 %d", res.StatusCode)
			}
		}
		response <- e
	}()
	waiting := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		var n int
		if e := pool.QueryRow(t.Context(), "SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '%ListTasks%'").Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			waiting = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("请求未进入数据库")
	}
	cancel()
	select {
	case e := <-done:
		finished = true
		t.Fatalf("未排空请求即退出: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	if e = tx.Rollback(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e = <-response; e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		finished = true
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("排空未完成")
	}
}
