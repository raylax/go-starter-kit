package account

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"log/slog"
	"time"
)

func audit(ctx context.Context, q *store, r Request, action string, outcome AuditOutcome, kind, id, scope, reason string, metadata any) error {
	data, e := json.Marshal(metadata)
	if e != nil {
		return e
	}
	if metadata == nil {
		data = []byte(`{}`)
	}
	actorType := ActorAnonymous
	var actor *string
	if r.UserID != "" {
		actorType = ActorUser
		actor = ptr(r.UserID)
	}
	var session *uuid.UUID
	if sid, e := uuid.Parse(r.SessionID); e == nil {
		session = &sid
	}
	optional := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	return q.AppendAudit(ctx, sqlc.AppendAuditParams{Action: action, Outcome: string(outcome), ActorType: string(actorType), ActorID: actor, ResourceType: kind, ResourceID: optional(id), ScopeSubject: optional(scope), SessionID: session, RequestID: optional(r.RequestID), ReasonCode: optional(reason), Metadata: data})
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
	if e := audit(auditCtx, s.queries, r, action, outcome, "user", "", r.UserID, reason, nil); e != nil {
		slog.ErrorContext(ctx, "审计写入失败", "action", action)
	}
}
