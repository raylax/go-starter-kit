package account

import (
	"github.com/example/go-starter-kit/internal/db/sqlc"
)

func ptr[T any](v T) *T { return &v }
func text(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func userRecord(u sqlc.User) UserRecord {
	return UserRecord{ID: u.ID, DisplayName: u.DisplayName, Email: u.Email, EmailVerified: u.EmailVerifiedAt != nil, Status: UserStatus(u.Status), Role: UserRole(u.Role), CreatedAt: u.CreatedAt}
}
func (s *Service) accountRecords(rows []sqlc.Account) []AccountRecord {
	out := make([]AccountRecord, 0, len(rows))
	for _, a := range rows {
		out = append(out, AccountRecord{ID: a.ID, Provider: a.ProviderID, CreatedAt: a.CreatedAt, LastUsedAt: a.LastUsedAt, Enabled: s.accountEnabled(a)})
	}
	return out
}
