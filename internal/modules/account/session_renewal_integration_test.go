//go:build integration

package account

import (
	"testing"
	"time"
)

func TestMinimumIdleTTLStillRenews(t *testing.T) {
	f := newFixture(t)
	f.s.options.IdleTTL = time.Minute
	session := f.register("renewal@example.com")
	// 模拟持续活跃会话达到半个闲置窗口，不依赖真实等待。
	if _, err := f.pool.Exec(t.Context(), `UPDATE user_sessions SET last_seen_at=now()-interval '31 seconds',idle_expires_at=now()+interval '29 seconds' WHERE id=$1`, session.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.AuthenticateSession(t.Context(), session.Token); err != nil {
		t.Fatal(err)
	}
	var renewed bool
	if err := f.pool.QueryRow(t.Context(), `SELECT idle_expires_at>now()+interval '45 seconds' AND idle_expires_at<=absolute_expires_at FROM user_sessions WHERE id=$1`, session.SessionID).Scan(&renewed); err != nil {
		t.Fatal(err)
	}
	if !renewed {
		t.Fatal("一分钟闲置期限下未续期或突破绝对期限")
	}
	var before, after time.Time
	if err := f.pool.QueryRow(t.Context(), "SELECT last_seen_at FROM user_sessions WHERE id=$1", session.SessionID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.AuthenticateSession(t.Context(), session.Token); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(t.Context(), "SELECT last_seen_at FROM user_sessions WHERE id=$1", session.SessionID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !before.Equal(after) {
		t.Fatal("未达到续期间隔仍重复写入")
	}
}
