package account

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/validation"
	"github.com/google/uuid"
)

const (
	maxAuditActionRunes     = 100
	maxAuditMetadataBytes   = 8 << 10 // 8 KiB
	maxMailKindBytes        = 64
	maxMailSubjectBytes     = 998
	maxMailBodyBytes        = 64 << 10 // 64 KiB
	securityNotificationTTL = 24 * time.Hour

	securityNotificationSubject = "Account security notification"
)

// store 在账户模块的写入入口维护字段和组合规则，SQL 仅负责持久化与并发约束。
// 固定角色 user、初始版本 1、状态转换和恢复开关由专用写入操作构造，调用方不能任意设置。
type store struct{ rawQueries *sqlc.Queries }

func newStore(database sqlc.DBTX) *store { return &store{rawQueries: sqlc.New(database)} }

func validBytes(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}

func validEmailPair(email, normalized *string) bool {
	if email == nil || normalized == nil {
		return false
	}
	value, err := normalizeEmail(*email)
	return err == nil && value == *normalized && *email == strings.TrimSpace(*email)
}

func (q *store) CreatePendingUser(ctx context.Context, p sqlc.CreatePendingUserParams) (sqlc.User, error) {
	if !validEmailPair(p.Email, p.EmailNormalized) {
		return sqlc.User{}, ErrInvalid
	}
	p.ID = uuid.New()
	return q.rawQueries.CreatePendingUser(ctx, p)
}

func (q *store) CreateFederatedUser(ctx context.Context, name string) (sqlc.User, error) {
	if !validation.TextWithin(name, maxDisplayNameRunes) {
		return sqlc.User{}, ErrInvalid
	}
	return q.rawQueries.CreateFederatedUser(ctx, sqlc.CreateFederatedUserParams{ID: uuid.New(), DisplayName: name})
}

func (q *store) ActivateUser(ctx context.Context, id uuid.UUID) (sqlc.User, error) {
	u, err := q.GetUser(ctx, id)
	if err != nil {
		return u, err
	}
	// 激活同时开启恢复，必须已有合法的待验证邮箱。
	if !validEmailPair(u.Email, u.EmailNormalized) {
		return sqlc.User{}, ErrInvalid
	}
	return q.rawQueries.ActivateUser(ctx, id)
}

func (q *store) UpdateUserEmail(ctx context.Context, p sqlc.UpdateUserEmailParams) (sqlc.User, error) {
	if !validEmailPair(p.Email, p.EmailNormalized) {
		return sqlc.User{}, ErrInvalid
	}
	return q.rawQueries.UpdateUserEmail(ctx, p)
}

func (q *store) UpdateUserProfile(ctx context.Context, p sqlc.UpdateUserProfileParams) (sqlc.User, error) {
	if !validation.TextWithin(p.DisplayName, maxDisplayNameRunes) {
		return sqlc.User{}, ErrInvalid
	}
	return q.rawQueries.UpdateUserProfile(ctx, p)
}

func (q *store) SetUserStatus(ctx context.Context, p sqlc.SetUserStatusParams) (sqlc.User, error) {
	if !UserStatus(p.Status).Settable() {
		return sqlc.User{}, ErrInvalid
	}
	return q.rawQueries.SetUserStatus(ctx, p)
}

func (q *store) CreateAccount(ctx context.Context, p sqlc.CreateAccountParams) (sqlc.Account, error) {
	if p.UserID == uuid.Nil || p.ProviderID == "" || !validation.TextWithin(p.ProviderID, maxProviderIDRunes) || !validBytes(p.ProviderAccountID, maxProviderSubjectBytes) || !validBytes(p.ProviderNamespace, maxProviderNamespaceBytes) {
		return sqlc.Account{}, ErrInvalid
	}
	if p.ProviderID == CredentialProvider {
		if p.ProviderNamespace != LocalNamespace || p.ProviderAccountID != p.UserID.String() || p.PasswordHash == nil || *p.PasswordHash == "" {
			return sqlc.Account{}, ErrInvalid
		}
	} else if p.ProviderNamespace == LocalNamespace || p.PasswordHash != nil {
		return sqlc.Account{}, ErrInvalid
	}
	p.ID = uuid.New()
	return q.rawQueries.CreateAccount(ctx, p)
}

func (q *store) ChangePassword(ctx context.Context, p sqlc.ChangePasswordParams) (int64, error) {
	if p.PasswordHash == nil || *p.PasswordHash == "" {
		return 0, ErrInvalid
	}
	return q.rawQueries.ChangePassword(ctx, p)
}

func (q *store) UpgradePasswordHash(ctx context.Context, p sqlc.UpgradePasswordHashParams) error {
	if p.PasswordHash == nil || *p.PasswordHash == "" {
		return ErrInvalid
	}
	a, err := q.GetAccount(ctx, sqlc.GetAccountParams{ID: p.ID, UserID: p.UserID})
	if err != nil {
		return err
	}
	if a.ProviderID != CredentialProvider {
		return ErrInvalid
	}
	return q.rawQueries.UpgradePasswordHash(ctx, p)
}

func (q *store) CreateSession(ctx context.Context, p sqlc.CreateSessionParams) (sqlc.UserSession, error) {
	if len(p.TokenHash) != sha256.Size || !AuthMethod(p.AuthMethod).Valid() || p.AuthVersion <= 0 || p.IdleSeconds <= 0 || p.MaxSeconds < p.IdleSeconds {
		return sqlc.UserSession{}, ErrInvalid
	}
	p.ID = uuid.New()
	return q.rawQueries.CreateSession(ctx, p)
}

func (q *store) CreateFlow(ctx context.Context, p sqlc.CreateFlowParams) (sqlc.AuthFlow, error) {
	if !FlowPurpose(p.Purpose).Valid() || !FlowStatus(p.Status).Initial() || (p.TokenHash != nil && len(p.TokenHash) != sha256.Size) {
		return sqlc.AuthFlow{}, ErrInvalid
	}
	if (p.UserID == nil) != (p.SessionID == nil) || (!FlowPurpose(p.Purpose).PublicStart() && p.UserID == nil) {
		return sqlc.AuthFlow{}, ErrInvalid
	}
	if p.UserID != nil && p.AuthVersion <= 0 {
		return sqlc.AuthFlow{}, ErrInvalid
	}
	if p.AccountID != nil && p.AccountVersion <= 0 {
		return sqlc.AuthFlow{}, ErrInvalid
	}
	return q.rawQueries.CreateFlow(ctx, p)
}

func (q *store) CreateVerification(ctx context.Context, p sqlc.CreateVerificationParams) (sqlc.AuthVerification, error) {
	if !VerificationPurpose(p.Purpose).Valid() || len(p.TokenHash) != sha256.Size || p.AuthVersion <= 0 || p.TtlSeconds <= 0 {
		return sqlc.AuthVerification{}, ErrInvalid
	}
	if _, err := normalizeEmail(p.Email); err != nil {
		return sqlc.AuthVerification{}, ErrInvalid
	}
	p.ID = uuid.New()
	return q.rawQueries.CreateVerification(ctx, p)
}

func (q *store) RateLimit(ctx context.Context, p sqlc.RateLimitParams) (sqlc.RateLimitRow, error) {
	if len(p.BucketKey) != sha256.Size || p.WindowSeconds <= 0 {
		return sqlc.RateLimitRow{}, ErrInvalid
	}
	return q.rawQueries.RateLimit(ctx, p)
}

func (q *store) AppendAudit(ctx context.Context, p sqlc.AppendAuditParams) error {
	if p.Action == "" || !validation.TextWithin(p.Action, maxAuditActionRunes) || !AuditOutcome(p.Outcome).Valid() || !AuditActorType(p.ActorType).Valid() {
		return ErrInvalid
	}
	if (AuditActorType(p.ActorType) == ActorAnonymous) != (p.ActorID == nil) {
		return ErrInvalid
	}
	if p.ActorID != nil && *p.ActorID == "" {
		return ErrInvalid
	}
	// 以实际发送的 JSON 字节数限制大小；禁止数组、标量与 null。
	var metadata map[string]json.RawMessage
	if len(p.Metadata) > maxAuditMetadataBytes || json.Unmarshal(p.Metadata, &metadata) != nil || metadata == nil {
		return ErrInvalid
	}
	p.ID = uuid.New()
	return q.rawQueries.AppendAudit(ctx, p)
}

// EnqueueMail 校验队列载荷，队列写入必须与账户变更共用事务。
func (q *store) EnqueueMail(ctx context.Context, p sqlc.EnqueueMailParams) error {
	if p.ID == uuid.Nil || !validBytes(p.Kind, maxMailKindBytes) || !validBytes(p.Subject, maxMailSubjectBytes) || !validBytes(p.Body, maxMailBodyBytes) || p.ExpiresAt.IsZero() {
		return ErrInvalid
	}
	if _, err := normalizeEmail(p.Recipient); err != nil {
		return err
	}
	return q.rawQueries.EnqueueMail(ctx, p)
}

// enqueueSecurityNotification 在当前业务事务中写入已验证邮箱的安全通知。
func (q *store) enqueueSecurityNotification(ctx context.Context, user sqlc.User, body string) error {
	if user.Email == nil || user.EmailVerifiedAt == nil {
		return nil
	}
	return q.EnqueueMail(ctx, sqlc.EnqueueMailParams{ID: uuid.New(), Kind: SecurityNotificationKind, Recipient: *user.Email, Subject: securityNotificationSubject, Body: "<p>" + html.EscapeString(body) + "</p>", ExpiresAt: time.Now().Add(securityNotificationTTL)})
}
