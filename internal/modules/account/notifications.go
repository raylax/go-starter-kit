package account

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"time"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
)

const (
	securityNotificationTTL = 24 * time.Hour

	verificationMailSubject           = "Verify your account"
	verificationMailBodyTemplate      = `<p>请在有效期内完成账户验证。请勿转发此邮件。</p><p><a href="%s">验证账户</a></p>`
	securityNotificationSubject       = "Account security notification"
	registrationCompletedNotification = "您的账户已完成注册，邮箱验证及密码设置成功。您现在可以登录。"
	passwordResetNotification         = "您的账户密码已重置。如非本人操作，请立即联系管理员。"
	passwordChangedNotification       = "您的账户密码已修改。如非本人操作，请立即联系管理员。"
	emailChangedNotification          = "账户联系邮箱已修改。如果不是本人操作，请联系管理员。"
	accountLinkedNotification         = "新的第三方登录账号已绑定。"
	accountUnlinkedNotification       = "一个登录账号已解除绑定，请使用保留的方式重新登录。"
)

// enqueueVerificationMail 使用挑战的有效期，与挑战创建共用业务事务。
func (s *Service) enqueueVerificationMail(ctx context.Context, q *store, verification sqlc.AuthVerification, token string) error {
	link, err := url.Parse(s.options.FrontendURL)
	if err != nil {
		return ErrUnavailable
	}
	link.Path = "/auth/verify"
	link.RawQuery = ""
	link.Fragment = url.Values{"token": {token}, "purpose": {verification.Purpose}}.Encode()
	return q.EnqueueMail(ctx, sqlc.EnqueueMailParams{
		ID:        uuid.New(),
		Kind:      verification.Purpose,
		Recipient: verification.Email,
		Subject:   verificationMailSubject,
		Body:      fmt.Sprintf(verificationMailBodyTemplate, html.EscapeString(link.String())),
		ExpiresAt: verification.ExpiresAt,
	})
}

// enqueueSecurityNotification 仅向已验证邮箱通知，与安全变更共用业务事务。
func enqueueSecurityNotification(ctx context.Context, q *store, user sqlc.User, body string) error {
	if user.Email == nil || user.EmailVerifiedAt == nil {
		return nil
	}
	return q.EnqueueMail(ctx, sqlc.EnqueueMailParams{
		ID:        uuid.New(),
		Kind:      SecurityNotificationKind,
		Recipient: *user.Email,
		Subject:   securityNotificationSubject,
		Body:      "<p>" + html.EscapeString(body) + "</p>",
		ExpiresAt: time.Now().Add(securityNotificationTTL),
	})
}
