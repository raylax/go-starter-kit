package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestLogSenderDoesNotExposeMail(t *testing.T) {
	var logs bytes.Buffer
	sender := NewLogSender(slog.New(slog.NewJSONHandler(&logs, nil)))
	message := Message{ID: "test-message", Kind: "register", To: "private@example.com", Subject: "private subject", HTML: `<p><a href="https://web.example/auth/verify#token=verify_private">验证账户</a></p>`}
	if err := sender.Send(t.Context(), message); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{message.To, message.Subject, message.HTML, "verify_private"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("mock 日志泄露邮件内容")
		}
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["provider"] != "log" || record["message_id"] != message.ID || record["kind"] != message.Kind || record["delivered"] != false {
		t.Fatal("mock 日志缺少发送关联或错误表示已投递")
	}
	logs.Reset()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sender.Send(ctx, message); !errors.Is(err, context.Canceled) || logs.Len() != 0 {
		t.Fatal("取消后仍记录发送成功")
	}
}
