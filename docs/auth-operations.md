# 认证运行说明

## 初始化与运行

空数据库先应用 `00001_init.sql` 完整基线，再启动 API 和 Worker。基线包含邮件队列及全部业务表。已应用旧版迁移的数据库不能直接复用这条新基线；需要另行迁移数据，或在允许丢弃数据的开发环境重建数据库。历史 `projects.owner_id`、`tasks.owner_id` 不自动改写；已有外部 subject 必须由可信用户映射迁移，不能按邮箱猜测归属。旧 JWT 和演示令牌不能用于当前认证。

在 `.env` 设置前端配置；生产环境使用真实 HTTPS origin。第三方登录的客户端密钥文件见下文提供商配置。

```text
FRONTEND_URL=https://localhost:3000
ALLOWED_ORIGINS=https://localhost:3000
```

当前邮件发送器固定为日志 mock，无 SMTP 配置，也不调用外部服务。它只记录 `message_id`、`kind`、`provider=log`、`delivered=false`，不输出收件地址、正文、验证码或验证链接。密码注册仍要求邮箱持有证明；mock 不会自动激活用户，也不能靠日志完成真实邮箱验证。账户集成测试从事务提交后的队列读取测试消息。

本仓库交付后端，前端需要实现 `/auth/verify` 和 `/auth/callback`，具体协议见 [认证设计](auth-design.md)。验证邮件在 fragment 中携带一次性挑战；前端取出后立即清理地址，通过 JSON 正文的 `token` 字段调用验证接口。密码注册先提交邮箱，收到邮件后设置密码，再登录取得 `tk_` 会话令牌。

## 邮件发送抽象

`internal/platform/mail` 的统一契约为：

```go
type Sender interface {
    Send(context.Context, Message) error
}
```

`Message` 包含 `ID`、`Kind`、`To`、`Subject`、`HTML`。账户模块在业务事务内写入邮件队列，`mailoutbox` 模块消费队列，Worker 的 app 层负责映射消息并选择发送器。后续接入 SendGrid 等服务时，在 platform/mail 增加适配器并在 app 装配；供应商配置、密钥、HTTP 客户端和错误转换留在适配器，不进入业务模块。

邮件在业务事务中写入 `mail_outbox`，提交后由 Worker 通过 `platform/mail.Sender` 异步发送。注册和恢复的 202 为统一受理响应，用户状态不符合条件时不会入队；实际生成的邮件与业务事务共同提交，投递失败由队列重试。当前使用日志 mock，不输出地址或正文。领取、重试、有效期和幂等语义见 [邮件队列](mail-outbox.md)。

## 第三方登录配置

`AUTH_PROVIDERS_FILE` 留空时只启用密码登录。配置文件是数组；客户端密钥另存秘密文件。例如：

```json
[
  {
    "id": "github",
    "protocol": "github",
    "client_id": "registered-github-client-id",
    "client_secret_file": "/run/secrets/github-client-secret",
    "redirect_uri": "https://app.example.com/auth/callback"
  }
]
```

示例地址和客户端标识必须替换。回调必须为 `FRONTEND_URL` 的 `/auth/callback`，无 query/fragment；提供商控制台登记完全一致的回调。入口 ID 与提供商命名空间及用户 ID 对应关系是身份的一部分，已有账号时修改配置需先制定迁移规则。

前端保留短期 `flow_` 令牌，回调后将 `{token, code, state}` 通过 JSON 正文交给后端，`token` 使用流程令牌。绑定最终确认使用原 `tk_` 会话。会话令牌和流程令牌都不能放入 URL；OAuth 弹窗的消息必须核对 origin/source/state。未绑定身份只有显式选择注册时才能创建用户，不按提供商邮箱自动关联。

GitHub OAuth 只能证明新授权流程中的账号控制权，不保证提供商要求再次输入密码；当前普通账号管理接受这种证明，不表示 MFA。

## 首位管理员

先通过正常注册建立用户，确认目标 UUID 后，由受控运维数据库账号执行事务。应用没有公开的角色赋予接口。下列 `:user_id` 是 SQL 客户端绑定参数，必须替换或绑定为核对后的 UUID：

```sql
BEGIN;
SELECT id, status, role FROM users WHERE id = :user_id FOR UPDATE;
UPDATE users SET role = 'admin', auth_version = auth_version + 1, updated_at = now()
WHERE id = :user_id AND status = 'active';
UPDATE user_sessions SET revoked_at = now() WHERE user_id = :user_id AND revoked_at IS NULL;
INSERT INTO audit_events(action, outcome, actor_type, actor_id, resource_type, resource_id, scope_subject)
SELECT 'user.role_change', 'success', 'system', 'operator-bootstrap', 'user', id::text, id::text
FROM users WHERE id = :user_id AND status = 'active' AND role = 'admin';
COMMIT;
```

操作前确认用户存在且 active，检查受影响行数，并把 `operator-bootstrap` 改为可追溯的运维身份。角色变更后重新登录。不要把第一个注册用户自动提升为管理员。

## 权限、轮换与投递

- 迁移账号持有 DDL 权限；应用使用独立且非表所有者的 DML 账号。对 `audit_events` 仅授予 INSERT，不能 UPDATE/DELETE/TRUNCATE；审计读取及保留清理由独立运维角色管理。迁移中的 PUBLIC 撤权不能限制表所有者，不要用所有者账号运行生产 API。
- `audit_events` 当前没有公开查询接口。新增业务可在当前事务中追加具名事件，不能写入密码、令牌或任意请求正文；将来提供查询时必须落实归属或管理员授权。
- `tk_` 会话在数据库中只保存 SHA-256 摘要，重启不会撤销有效会话。
- `mail_outbox` 暂明文保存收件人及正文，终态时清除；不要记录载荷或供应商原始错误。限制数据库及备份访问。
- 当前没有认证临时数据自动清理任务，过期记录保留在数据库中，但认证仍检查有效期。记录保留和删除需另行安排；审计保留策略独立管理。关注邮件发送失败与 429，按环境配置告警。
- 限速使用数据库原子计数：单入口来源 IP 每分钟 60 次，账户/主体每 15 分钟 10 次；验证和回调按 IP 每分钟 60 次。客户端 IP 取直接连接对端，代理后的用户会共享 IP 桶，部署时配合网关策略。

## 验收边界

本地测试使用真实临时 PostgreSQL、持久化邮件队列和模拟发送器。接入真实发送器后，用实际邮件服务、提供商客户端和前端做端到端联调，并演练存量资源映射、密钥轮换和邮件故障恢复。不要把这些本地测试视为外部服务已完成配置。

应用启动不自动迁移，数据库初始化使用独立迁移命令。
