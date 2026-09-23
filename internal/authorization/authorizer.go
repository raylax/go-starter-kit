// Package authorization 定义跨业务模块的授权契约，不依赖账户、数据库或 HTTP。
package authorization

import "context"

// Subject 标识已认证用户和本次请求使用的原会话，不接受客户端自报角色。
type Subject struct{ UserID, SessionID string }

// Authorizer 在业务操作入口重新检查权限，失败时必须阻止操作。
// 实现使用普通读取，不保证授权检查与后续业务写入的串行一致性。
type Authorizer interface {
	RequireAdmin(context.Context, Subject) error
}

// AdminCheckFunc 适配应用装配函数或测试替身。
type AdminCheckFunc func(context.Context, Subject) error

func (f AdminCheckFunc) RequireAdmin(ctx context.Context, subject Subject) error {
	return f(ctx, subject)
}
