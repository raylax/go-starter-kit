package api

import (
	"github.com/example/go-starter-kit/internal/app/appfx"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/modules/account"
)

// newAuthorizer 将账户权限查询适配为各业务模块共用的授权接口。
func newAuthorizer(database *appfx.Database) authorization.Authorizer {
	checker := account.NewAdminChecker(database)
	return authorization.AdminCheckFunc(checker.CheckAdmin)
}
