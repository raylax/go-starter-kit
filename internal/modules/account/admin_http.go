package account

import (
	"context"
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/pagination"
)

// AdminRoutes 独立提供管理接口，业务事务内重新校验管理员角色与会话。
func AdminRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.PageEndpoint[UserListResponse](operation("User Administration", "list-users", "GET", "/v1/admin/users", "管理员查询用户", httpapi.Session), func(s *Service, c context.Context, i *ListInput) (pagination.Result[UserRecord], error) {
			return s.Users(c, request(c), i.Params())
		}, func(v UserRecord) User { return User(v) }),
		httpapi.ItemEndpoint(operation("User Administration", "set-user-status", "PATCH", "/v1/admin/users/{id}/status", "管理员启用或禁用用户", httpapi.Session), func(s *Service, c context.Context, i *StatusInput) (UserRecord, error) {
			return s.SetStatus(c, request(c), i.ID, i.Body.Status)
		}, func(v UserRecord) User { return User(v) }),
		httpapi.NoContentEndpoint(operation("User Administration", "revoke-user-sessions", "DELETE", "/v1/admin/users/{id}/sessions", "管理员撤销用户会话", httpapi.Session), func(s *Service, c context.Context, i *IDInput) error {
			return s.RevokeUserSessions(c, request(c), i.ID)
		}),
	}
}
