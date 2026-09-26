package account

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
)

// 审计元数据独立于 HTTP DTO，固定字段使用领域类型。
type userStatusAuditMetadata struct {
	Status UserStatus `json:"status"`
}

type accountProviderAuditMetadata struct {
	Provider string `json:"provider"`
}

type loginAuditMetadata struct {
	Method AuthMethod `json:"method"`
}

type auditEvent struct {
	Action       string
	Outcome      AuditOutcome
	ResourceType string
	ResourceID   string
	ScopeSubject string
	Reason       string
	Metadata     any
}

func audit(ctx context.Context, q *store, r Request, event auditEvent) error {
	data, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	if event.Metadata == nil {
		data = []byte(`{}`)
	}
	actorType := ActorAnonymous
	var actor *string
	if r.UserID != "" {
		actorType = ActorUser
		actor = ptr(r.UserID)
	}
	var session *uuid.UUID
	if sessionID, err := uuid.Parse(r.SessionID); err == nil {
		session = &sessionID
	}
	optional := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	return q.AppendAudit(ctx, sqlc.AppendAuditParams{
		Action:       event.Action,
		Outcome:      string(event.Outcome),
		ActorType:    string(actorType),
		ActorID:      actor,
		ResourceType: event.ResourceType,
		ResourceID:   optional(event.ResourceID),
		ScopeSubject: optional(event.ScopeSubject),
		SessionID:    session,
		RequestID:    optional(r.RequestID),
		ReasonCode:   optional(event.Reason),
		Metadata:     data,
	})
}

// recordFailureOnReturn 供 defer 调用，退出时读取具名错误返回值。
// err 必须指向调用方的具名返回值；审计失败不覆盖业务结果。
func (s *Service) recordFailureOnReturn(ctx context.Context, r Request, action string, err *error) {
	s.recordFailure(ctx, r, action, *err)
}

// recordFailure 在业务事务结束后独立记录，不覆盖原来的失败结果。
func (s *Service) recordFailure(ctx context.Context, r Request, action string, err error) {
	if err == nil || errors.Is(err, ErrRateLimited) {
		return
	}
	outcome, reason := AuditFailure, "operation_failed"
	if errors.Is(err, ErrCredentials) {
		reason = "invalid_credentials"
	}
	if errors.Is(err, ErrForbidden) {
		outcome, reason = AuditDenied, "permission_denied"
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := audit(auditCtx, s.queries, r, auditEvent{
		Action:       action,
		Outcome:      outcome,
		ResourceType: "user",
		ScopeSubject: r.UserID,
		Reason:       reason,
	}); err != nil {
		attrs := []slog.Attr{slog.String("stage", "append_audit"), slog.String("action", action), slog.String("request_id", r.RequestID)}
		attrs = append(attrs, apperror.DiagnosticAttrs(err)...)
		s.deps.Logger.LogAttrs(ctx, slog.LevelError, "审计写入失败", attrs...)
	}
}
