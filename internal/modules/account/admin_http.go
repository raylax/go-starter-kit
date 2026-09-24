package account

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/example/go-starter-kit/internal/httpapi"
	"github.com/example/go-starter-kit/internal/pagination"
)

// AdminRoutes 独立提供管理接口，由账户模块复核管理员角色与原会话。
func AdminRoutes() []httpapi.Route[*Service] {
	return []httpapi.Route[*Service]{
		httpapi.PageEndpoint[UserListResponse](
			operation(httpapi.Session, huma.Operation{
				OperationID: "list-users",
				Method:      http.MethodGet,
				Path:        "/v1/admin/users",
				Summary:     "管理员查询用户",
				Tags:        []string{"User Administration"},
			}),
			listUsers,
			userDTO,
		),
		httpapi.ItemEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "set-user-status",
				Method:      http.MethodPatch,
				Path:        "/v1/admin/users/{id}/status",
				Summary:     "管理员启用或禁用用户",
				Tags:        []string{"User Administration"},
			}),
			setUserStatus,
			userDTO,
		),
		httpapi.NoContentEndpoint(
			operation(httpapi.Session, huma.Operation{
				OperationID: "revoke-user-sessions",
				Method:      http.MethodDelete,
				Path:        "/v1/admin/users/{id}/sessions",
				Summary:     "管理员撤销用户会话",
				Tags:        []string{"User Administration"},
			}),
			revokeUserSessions,
		),
	}
}

func listUsers(service *Service, ctx context.Context, input *ListInput) (pagination.Result[UserRecord], error) {
	return service.Users(ctx, request(ctx), input.Params())
}

func setUserStatus(service *Service, ctx context.Context, input *StatusInput) (UserRecord, error) {
	return service.SetStatus(ctx, request(ctx), input.ID, input.Body.Status)
}

func revokeUserSessions(service *Service, ctx context.Context, input *IDInput) error {
	return service.RevokeUserSessions(ctx, request(ctx), input.ID)
}
