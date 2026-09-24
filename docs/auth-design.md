# 本地用户与认证体系

本文描述当前后端实现。前端页面、真实邮件发送器及外部提供商部署联调不包含在仓库交付范围内。术语见 [CONTEXT.md](../CONTEXT.md)，配置与接入步骤见 [认证运行说明](auth-operations.md)。

## 范围与领域模型

本项目管理自己的用户、登录账号和数据库会话。当前登录方式为邮箱密码与 GitHub OAuth；不支持 OIDC、Passkey、MFA、用户合并或授权服务器，不使用 Cookie、本地 JWT、JWKS、刷新令牌或开发认证模式。

模型参考 Better Auth 的 User、Account、Session、Verification 分工，具体行为由本项目实现：

| 模型 | 表 | 职责 |
| --- | --- | --- |
| User | `users` | 业务主体，保存资料、联系邮箱、状态、角色和认证版本 |
| Account | `accounts` | 可撤销的密码或第三方登录身份，不是业务资源所有者 |
| Session | `user_sessions` | 登录后的访问资格，保存令牌摘要、期限及认证版本 |
| Verification | `auth_verifications` | 注册、密码恢复和联系邮箱确认的一次性挑战 |
| AuthFlow | `auth_flows` | 第三方登录、注册、绑定和重新认证的短期状态 |
| AuditEvent | `audit_events` | 通用追加审计，当前账户模块写入 |
| RateLimit | `auth_rate_limits` | 各类认证请求的跨副本原子计数 |
| MailOutbox | `mail_outbox` | 事务内入队的邮件与投递状态 |

同一 User 可拥有一个有效密码 Account 和多个第三方 Account，最多 10 个有效 Account。项目和任务的 `owner_id` 指向本地 User 的 ID 字符串；增删登录方式不改变资源归属。第三方身份不按邮箱自动合并。

密码账号使用 `provider_id=credential`、本地命名空间以及用户 ID 字符串作为 `provider_account_id`。第三方账号使用提供商命名空间和不可变用户 ID；GitHub 命名空间固定为 `https://api.github.com`，身份来自固定用户 API。撤销账号保留历史行，重新绑定创建新记录。

用户状态为 `pending/active/disabled`，角色为 `user/admin`。所有注册方式创建普通用户；管理员提升通过受控运维流程完成。联系邮箱可空，标准化时去除首尾空白并转小写，不做提供商特定转换。有效身份唯一性、外键和 NOT NULL 由数据库保证；长度、枚举与字段组合由业务代码校验，无数据库 CHECK。

## 模块与装配

| 位置 | 职责 |
| --- | --- |
| `modules/account/service.go`、`dependencies.go`、`model.go` | 服务与选项、外部依赖契约、模型与业务错误 |
| `registration.go`、`challenges.go`、`email.go` | 注册与恢复、挑战编排、邮箱变更，各用途的完成逻辑独立 |
| `reauthentication.go`、`password.go`、`linked_accounts.go` | 重新认证授权、密码设置与修改、账号绑定与解绑及可用性规则 |
| `federation.go` | OAuth 发起、回调校验与各用途的事务完成逻辑 |
| `store.go`、`store_queries.go`、`store_flows.go` | 写入校验、邮件入队与安全通知、显式查询集合和原子流程状态转换 |
| `session.go`、`profile.go` | 会话校验与管理、资料读取与更新 |
| `audit.go` | 成功审计及独立失败记录 |
| `admin.go`、`admin_http.go`、`admin_dto.go` | 本领域管理业务、路由与协议类型 |
| `admin_checker.go` | 独立管理员资格查询 |
| `identity` | 随机令牌、摘要、主体校验与认证上下文 |
| `authorization` | 公共 Subject 与 Authorizer 契约 |
| `platform/password`、`platform/federation` | 密码哈希与 GitHub OAuth 适配 |
| `modules/mailoutbox`、`platform/mail` | 邮件消费业务与供应商发送接口 |
| `app/api`、`app/worker`、`app/appfx` | Fx 依赖装配、配置和生命周期 |

业务模块不互相依赖，也不导入 Fx。账户服务接收完整的 `Passwords`、`Federation` 接口、管理员授权器和日志器；密码与日志是必需依赖，未提供 Federation 表示整体禁用，不支持分别注入不完整的 OAuth 函数集合。邮件直接在业务事务内入队，发送器由 Worker 装配。`identity` 不访问数据库、HTTP 或应用配置。`app/api/wire.go` 将账户会话校验适配为 `identity.Authenticator`。

事务复用 `db.WithTransaction`；账户写入通过 `store` 校验。仅为不可接受的并发后果加显式锁：会话签发、密码与账号变更、挑战消费、会话撤销继续通过用户锁保护跨表校验；Me 和 Sessions 普通读取。找回密码、邮箱挑战、重新认证及绑定流程的证明签发不预先显式锁定用户，接受竞争导致证明失效，最终消费时复核原会话、用户与账号版本。邮箱与版本必须来自同一次用户读取；并发换邮箱时可能向旧邮箱投递已失效挑战，不能把旧邮箱与新版本组合签发。

流程读取不使用行锁，一次性认领和消费依赖带预期状态的条件更新及影响行数判定；创建绑定流程与认领证明同事务，先插入流程完成外键检查，再认领证明，竞争失败则整体回滚。UpdateProfile 不预先加行锁，其更新 SQL 检查用户与原会话仍有效，并与审计一起提交。管理员权限检查独立于业务事务，详见文末。

## 令牌与会话

所有令牌由 32 字节随机值和类型前缀组成，使用无填充 Base64URL。认证表仅保存完整令牌的 SHA-256 摘要：

| 用途 | 前缀 | 传递位置 |
| --- | --- | --- |
| 登录会话 | `tk_` | `Authorization: Bearer tk_...` |
| 第三方流程 | `flow_` | OAuth callback 的 JSON 正文 `token` |
| 邮件挑战 | `verify_` | verify 的 JSON 正文 `token` |

记录引用 `session_id/flow_id/reauthentication_id` 不能单独作为凭据。验证与回调不把 Authorization 作为正文 token 的备用来源。会话令牌只在登录响应中返回；邮件挑战原文会短期存在邮件正文中，邮件终态清除载荷。

每次受保护请求按摘要查询会话与用户，检查 active 状态、未撤销、闲置与绝对期限、认证版本一致。当前不缓存认证成功结果；数据库故障不能放行认证。默认闲置期限 30 分钟、绝对期限 24 小时，统一适用于普通用户和管理员；仅提供全局期限配置。

续期写入间隔取 `min(1 分钟, IdleTTL / 2)`，Go 与 SQL 使用同一阈值。续期不超过绝对期限，不更新认证时间、不更换令牌。单个会话可撤销；全部退出、改密、恢复、解绑及相关用户变更通过撤销或版本失配使旧会话失效。已经通过认证的在途请求不保证立即中断。

认证临时记录目前不自动删除；过期会话、挑战和流程仍由查询及业务规则拒绝。不存在刷新令牌表或重放缓存。

## 密码、注册与恢复

密码使用 `alexedwards/argon2id` 生成和解析 PHC，当前参数为 19 MiB、2 次迭代、并行度 1，API 装配最多 4 个并发哈希。密码须为有效 UTF-8、15–128 个字符且最多 512 字节，允许空格，不裁剪、不要求字符组合，不检查弱密码名单或重复字符。不存在的账号使用 dummy 校验；验证旧参数后可升级哈希。

邮箱注册先创建 pending 用户和 24 小时挑战。邮件持有者提交挑战与新密码，事务内创建密码账号、确认邮箱、激活用户及消费挑战，成功后正常登录。重复提交已激活邮箱返回统一 202；pending 用户可重新申请挑战，不覆盖已存在的密码。

忘记密码只为 active、已验证邮箱、启用恢复且有有效密码账号的用户创建 15 分钟挑战；其他正常请求仍返回 202。恢复成功撤销旧会话并更新认证版本，不自动登录、不解除封禁、不为纯第三方用户新增密码。202 表示受理，不保证实际生成邮件。

`PUT /v1/me/password` 根据数据库是否存在未撤销的 credential Account 选择分支：

| 当前状态 | 所需证明 |
| --- | --- |
| 已有密码 | 有效会话、`current_password` 和 `new_password` |
| 尚无密码 | 有效会话、`new_password`、匹配 `set_password` 的 `reauthentication_id`，且联系邮箱已验证；不得传当前密码 |

前端可根据 Me 返回的 accounts 中是否存在 `provider=credential` 展示“设置密码”或“修改密码”；后端在事务中复核，不能通过省略当前密码切换分支。两种成功路径都会撤销会话并入队安全通知。

邮件主题与文案在所属业务文件中定义为具名常量。首次设置密码与修改密码统一通知“账户密码已修改”；注册完成和密码重置使用各自文案。密码安全通知说明已发生的操作及非本人操作时的处理方式；注册完成通知提示用户可以登录。

修改联系邮箱要求重新认证，在原会话和授权记录上绑定一个 5 分钟挑战。确认时复核会话、版本、目标邮箱和授权，再更新邮箱并撤销旧会话。安全通知发往原已验证联系邮箱。

## GitHub OAuth、绑定与重新认证

提供商由 `AUTH_PROVIDERS_FILE` 配置，当前 protocol 仅接受 `github`。客户端密钥从文件读取；授权、令牌与用户 API 地址固定，scope 固定为 `read:user`，`redirect_uri` 独立指定 HTTPS 回调页面，不与 `FRONTEND_URL` 绑定。配置版本摘要防止旧流程继续使用变更后的配置。

1. 前端创建 `login/register` 流程，或携带原会话和重新认证引用发起绑定。
2. 后端保存流程令牌摘要、state 摘要、PKCE verifier、目的与归属，返回流程令牌和授权 URL。流程有效期 5 分钟。
3. 提供商跳转到前端；前端携带 `{token, code, state}` 调用后端 callback。
4. 后端校验 state、原子认领流程，然后在数据库事务外交换授权码并请求 GitHub 用户身份。
5. 登录或注册返回本地会话；重新认证返回操作授权引用；绑定返回待确认身份，再由原会话调用确认接口完成。

普通 login 不会为未知身份自动注册；必须显式使用 register。绑定不能转移别人已绑定的身份。第三方网络、限流、5xx 或配置故障归类为依赖不可用；明确的无效授权码或用户凭据才归类为证明无效，请求上下文超时保留超时语义，错误文本不暴露供应商正文。流程成功或失败时清除协议状态；过期未完成流程目前没有自动载荷清理任务。

重新认证引用绑定用户、原会话、用户/账号版本、操作和目标，只能消费一次。绑定和邮箱变更在开始时认领引用，最终提交时再次核对。解绑必须通过一个保留的可用账号证明控制权，且不能删除最后一个可用登录方式。

GitHub OAuth 证明新授权流程中的账号控制权，不保证用户再次输入密码，不提供 MFA 或认证保证等级。提供商 access token 仅在此次交换中使用，不长期保存，也不能用作本地 API 的会话凭据。

## 前端接入约定

仓库不包含前端实现。前端需实现 `/auth/verify` 与配置的 OAuth 回调页面。邮件验证链接通过 fragment 携带 token 和 purpose，前端读取后清理地址，再 POST 消费挑战，避免页面 GET 直接改变账户状态。

建议会话令牌只存内存，通过 Authorization 调用 API，并设置 `credentials: "omit"`。页面完全重载后需重新登录；需要持久登录时另行明确存储策略。回调推荐弹窗，严格校验消息 origin/source/state；整页跳转需自行保留短期流程上下文及敏感绑定所需的原会话。

API 不检查 Origin，也不处理 CORS；跨域预检与响应头由网关配置。会话继续仅使用 Authorization，不引入 Cookie 凭据。明确会话失效时清理令牌；证明错误、403 或服务故障不应直接当作登出。所有令牌、OAuth code、PKCE verifier 和邮件正文不得进入客户端遥测或日志。

## HTTP 契约

按 `HTTP 方法 + 路径` 计数，共 20 个操作，其中用户侧 17 个、管理侧 3 个。自助管理统一使用 `/v1/me`。合并内部流程步骤，并将账号摘要随用户资料返回；每个路由仍固定一种认证策略。

| 接口 | 用途与认证要求 |
| --- | --- |
| `POST /v1/auth/register` | 公开；提交邮箱，统一 202 |
| `POST /v1/auth/login` | 公开；邮箱 + 密码，返回本地会话令牌 |
| `POST /v1/auth/oauth/{provider}` | 公开；创建第三方登录或明确同意的注册流程，返回流程令牌和授权 URL |
| `POST /v1/auth/oauth/callback` | 正文 token + code/state；按服务端流程目的完成登录、重新认证或生成待确认绑定结果 |
| `POST /v1/auth/password/forgot` | 请求恢复邮件，统一 202 |
| `POST /v1/auth/verify` | 正文 token + purpose，按目的补充 new_password；完成注册、密码恢复或邮箱确认，成功统一 204 |
| `GET /v1/me` | 会话令牌；返回个人资料、有效 Accounts 的安全摘要和当前 session_id |
| `PATCH /v1/me` | 会话令牌；仅修改展示资料，邮箱、密码、状态、角色及 Accounts 均不可写 |
| `POST /v1/me/reauthenticate` | 会话令牌；密码方式直接返回重新认证引用，第三方方式返回授权流程 |
| `POST /v1/me/accounts/link` | 会话令牌 + provider + 重新认证引用；创建绑定流程，固定目标 User/Session |
| `POST /v1/me/accounts/link/confirm` | 原会话的令牌 + flow_id；确认展示过的账号并完成绑定 |
| `POST /v1/me/accounts/{id}/unlink` | 会话令牌 + 保留方式的重新认证引用；id 为本地 Account 行 ID |
| `PUT /v1/me/password` | 会话令牌 + 新密码；已有密码时另传当前密码，无密码时另传重新认证引用且服务端确认已验证邮箱；成功后重新登录 |
| `POST /v1/me/email` | 会话令牌 + 目标邮箱 + 重新认证引用；发送邮箱确认挑战，统一 202 |
| `GET /v1/me/sessions` | 会话令牌；仅列出自己的会话 |
| `DELETE /v1/me/sessions/{id}` | 会话令牌；撤销自己的某个会话 |
| `DELETE /v1/me/sessions` | 会话令牌；退出全部设备 |
| `GET /v1/admin/users` | 会话令牌 + 管理员权限；分页查询用户 |
| `PATCH /v1/admin/users/{id}/status` | 会话令牌 + 管理员权限；启用/禁用非 pending 用户；两种操作均更新认证版本并撤销会话 |
| `DELETE /v1/admin/users/{id}/sessions` | 会话令牌 + 管理员权限；撤销目标用户的全部会话 |

### 文档分组

分组按使用场景组织，每个操作只归属一个 tag；数据库表名、业务模块名不决定文档分组。

| OpenAPI tag | 用途 | 操作数 |
| --- | --- | --- |
| `Authentication` | 注册、密码登录、第三方流程启动/回调、忘记密码、一次性验证 | 6 |
| `Profile` | 当前用户资料读取与修改 | 2 |
| `Account Security` | 重新认证、密码设置/修改、联系邮箱变更 | 3 |
| `Linked Accounts` | 发起绑定、确认绑定、解除登录账号绑定 | 3 |
| `Sessions` | 查询自己的会话、撤销单个会话、退出全部设备 | 3 |
| `User Administration` | 管理员查询用户、修改状态、撤销目标用户会话 | 3 |

共用的 OAuth 回调和验证入口归入 `Authentication`，即使流程最终用于绑定或邮箱变更，也不重复展示到多个分组。`Profile` 只包含普通资料操作；会改变登录或恢复能力的操作归入 `Account Security`。管理员操作统一归入 `User Administration`，与自助接口分开。分组仅用于组织文档，实际权限继续由路由认证策略和业务校验决定。

### 字段与错误约定

- 令牌字段统一为 `token`，由所在请求或响应确定用途；登录返回 `tk_...`，流程创建返回 `flow_...`，邮件验证提交 `verify_...`，前缀和服务端记录共同限制用途。
- `session_id`、`flow_id`、`account_id` 与 `reauthentication_id` 保留语义名称，它们是记录引用，不能单独作为凭据。`current_password` / `new_password` 区分当前与待设置密码。
- 验证、流程或重新认证证明失效返回 422，`detail` 分别为 `verification_invalid`、`flow_invalid`、`reauthentication_invalid`；缺失字段或正文格式不符使用公共校验错误。数据库故障仍保留故障状态，不能误报证明失效。
- 会话中间件拒绝无效会话时返回 401 `session_invalid`；密码登录失败也可返回 401，但不能据此删除另一个仍有效的会话。客户端按操作与错误语义处理。
- 解绑需要正文中的重新认证引用，因此使用 POST 动作接口；DELETE 正文缺少通用语义且可能被中间层拒绝。无正文的会话撤销仍使用 DELETE。[HTTP DELETE 语义](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.3.5)
- 所有账户接口响应（包括失败）使用 `Cache-Control: no-store`。请求正文、Authorization 和令牌响应必须脱敏。

### 合并后的校验约定

- **挑战消费共用入口**：`verify` 正文使用三个具名变体：`UserRegistrationVerifyRequest{token, purpose: register, new_password}`、`UserPasswordResetVerifyRequest{token, purpose: reset_password, new_password}`、`UserEmailVerifyRequest{token, purpose: change_email}`。请求目的必须与挑战记录精确匹配；实际执行操作、目标用户和邮箱由服务端记录决定。拒绝不属于该变体的字段，邮箱确认不能夹带密码修改。注册和恢复沿用各自的事务与校验；邮箱确认还检查发起会话、认证版本以及在发起时已校验并绑定的重新认证授权。合并路由不合并挑战权限。
- **第三方回调共用入口**：流程令牌已经定位流程，callback 无需再传 flow_id 或 provider。响应使用具名变体及固定 `result`：`session`、`reauthenticated`、`link_pending`；每种流程只允许对应结果。流程的状态转换、一次性消费和并发控制全部由后端完成。注册同意提前在创建流程时取得，因此登录成功后无需再调用 complete；未知用户的普通登录不会自动注册。
- **重新认证共用入口**：输入用 `method=password` 或 `method=oauth` 的具名变体；两者均包含受支持的敏感操作及精确目标。密码请求包含当前密码，第三方请求包含已绑定 Account 的 ID。前端只处理“已验证”或“跳转验证”两种结果。返回的 `reauthentication_id` 是服务端授权记录的引用，必须配合原会话的令牌使用，且只能被对应操作消费一次；绑定、邮箱确认等多步操作在发起时原子认领并绑定该引用，最终提交再核对有效性，不能被另一流程复用。
- **密码写入共用入口**：新增与修改只改变同一用户的 credential Account，由服务器当前状态选择分支；既有密码的修改不能仅凭第三方重新认证引用完成。两种成功路径均更新认证版本、撤销旧会话并通知用户。
- **账号读取随资料返回**：`GET /v1/me` 的 `accounts` 仅包含本地行 ID、提供商、创建时间、最后使用时间及可用状态，不含密码哈希和第三方令牌。首版设置每用户最多 10 个有效 Account，保证响应有界；会话仍独立分页。

`oauth/callback` 是前端携带流程令牌调用的 POST 接口，提供商仍跳转到前端页面；固定路由 `callback` 必须优先于 `{provider}` 匹配，并禁止使用 `callback` 作为提供商 ID。退出当前设备复用会话删除接口，不再单设 logout 或 refresh。第三方绑定的最终确认仍单独保留，因为需要展示已验证的外部身份并验证原本地会话。

管理员入口在服务层再次验证操作者角色和状态，角色信息以数据库为准。初始管理员由受控运维流程将指定已验证用户提升，并记录审计事件，不设置默认管理员密码，也不开放公开角色修改参数。启用用户不会恢复旧会话。

路由清单为操作声明 `Public` 或 `Session` 策略；管理员权限在业务层检查。通过 `httpapi.NewOperation` 显式选择策略，同时驱动运行时装配和 OpenAPI Security；缺失、未知策略或独立设置 Security 在注册时拒绝。验证与回调在 OpenAPI 中不声明 HTTP 认证，必填的正文 token 由服务端校验目的、期限和一次性消费。Public 只表示不要求会话，仍受凭据校验、限速和请求体大小限制。

普通资源复用现有响应包装；令牌响应和授权流程使用 `MapEndpoint`，202/204 无正文成功使用 `NoContentEndpoint`。DTO 使用具名类型，例如 `UserRegisterRequest`、`UserLoginRequest`、`AccountLinkRequest`、`AccountLinkConfirmRequest`、`SessionCreateResponse`、`SessionListResponse`。验证与流程变体在 OpenAPI 中使用引用具名 schema 的 `oneOf`，由明确字段区分，并与运行时分支校验一致；不引入任意 action/payload 调度接口。公共错误分类已增加明确的 403 与 429 支持，继续由 `FromError` 统一映射；429 的 `Retry-After` 来自被触发限流桶的剩余有效秒数。

## 审计、限速与邮件

`audit_events` 是各模块可追加的共享表，当前账户业务写入；项目与任务 CRUD 尚未追加审计。字段包括 action、outcome、actor、resource、scope_subject、session_id、request_id、reason_code、schema_version 与 JSON metadata。固定结果及操作者类型使用枚举；metadata 须为对象且不超过 8192 字节。多态身份保留历史快照，不通过级联删除审计。

已实现的成功业务审计与变更同事务提交。配置失败审计的入口在返回时独立写入，使用最多 1 秒且不继承请求取消的上下文，失败使用注入的日志器记录固定提示、action 和 request_id，不覆盖原业务错误；429 不追加失败审计。会话认证与自动续期不写审计。当前无公开审计查询、自动保留清理或事件消费功能；生产运行账号仅授予审计 INSERT 权限。

认证限速使用 `SHA-256(类别 + NUL + 限速对象)` 作为计数桶键，无密钥。常规入口按 IP 每分钟 60 次、主体每 15 分钟 10 次；OAuth 发起、verify/callback 仅按 IP 每分钟 60 次，OAuth 发起不再把 IP 当作已知主体。计数到期后同桶重新计数，闲置桶没有自动删除任务。客户端 IP 使用连接对端，不信任转发头。数据库同时返回计数与剩余窗口，业务错误携带等待时间，HTTP 层映射为 Retry-After。

邮件通过共享 `mail_outbox` 与业务一起提交，Worker 异步领取、发送和重试。当前仅日志 mock，不实际投递，也不输出地址、正文或链接。正文为 HTML，动态内容转义；外部邮件 ID 用作幂等键，详细语义见 [邮件队列](mail-outbox.md)。

目前内置 HTTP 遥测及独立 Locker 指标；认证失败率、哈希耗时、邮件积压和审计缺口等专用监控需后续接入，不应将观测建议当作已实现指标。

## 数据库初始化与部署

`internal/db/migrations/00001_init.sql` 为当前唯一完整基线，包含项目、任务、用户、账号、会话、流程、挑战、审计、限速和邮件队列共 10 张表。新主键由 Go 代码生成 UUID v4，SQL 显式接收 ID，数据库不设置 UUID 生成默认值；已移除 nonce 字段，不使用 CHECK。空数据库运行 `make migrate-up`，API/Worker 启动不自动迁移；`migrate down` 删除全部业务表。

该基线用于空数据库，不能自动升级已经应用旧版 00001 或 00002 的数据库。存量环境需单独迁移或在允许丢弃数据时重建。项目和任务 owner 仍为 TEXT；若有历史外部 subject，必须通过可信映射迁移，不按邮箱或首个注册用户猜测归属。

新增部署后的结构变更应新增迁移；修改 SQL、迁移或 API 类型后执行 `make generate`。当前不包含真实提供商和发送器的部署验收结论；测试命令与工程约定见 README。

## 管理接口组织

模块按业务领域划分，管理能力归属对应业务模块。后续项目管理、任务管理分别由各自模块提供 `AdminRoutes()`，应用层负责统一注册及跨模块组合。若需要独立管理端进程、域名或契约，再增加 `internal/app/admin` 装配入口，复用已有业务能力。

账户模块的 `Routes()` 只包含认证和当前用户自助接口；`AdminRoutes()` 独立提供 `/v1/admin/users` 下的用户查询、状态变更和会话撤销接口，由应用层路由清单单独装配。管理 HTTP 映射和专用 DTO 分别放在 `admin_http.go`、`admin_dto.go`，业务实现保留在 `admin.go`，管理员角色与原会话由注入的公共授权接口在事务开始前校验。两组接口共用当前 API 进程和 OpenAPI 契约。

### 字符串枚举

账户固定取值在 `account/enums.go` 定义：用户状态、角色、认证方式、流程用途与状态、验证用途、敏感操作、认证结果，以及审计结果和操作者类型。业务参数与模型使用具名字符串类型，数据库类型保持独立，在读写边界转换。`Valid()` 拒绝空值和未知值；管理端可设置状态、公共流程入口及流程转换目标分别使用子集规则。任务状态、邮件队列状态和第三方协议各自由所属模块维护。提供商 ID、邮件类型、审计 action/resource/reason 保持可扩展。

管理接口通过普通读取校验操作者角色与原会话，不预先锁定操作者或目标用户。用户列表不使用事务，管理写入与审计仍同事务提交；数据库 UPDATE 自身的行锁由数据库管理。

### 公共管理员授权

`internal/authorization.Authorizer` 定义 `RequireAdmin(ctx, Subject)`；Subject 只包含服务端认证得到的用户 ID 和原会话 ID。`account.AdminChecker` 直接实现该接口，使用一个普通查询同时检查用户状态、会话归属、撤销及有效期、认证版本和角色。`app/api/wire.go` 在装配业务依赖时直接注入该实现，不增加函数适配层。

账户 Service 与权限查询独立，依赖图为数据库 → AdminChecker/Authorizer → 业务 Service。后续项目或任务管理入口注入同一接口，保持模块之间无直接依赖。授权失败立即返回；成功后再开启业务事务，业务变更与审计同事务。不使用显式行锁，权限在检查之后发生变化时不保证与本次业务写入串行化。

账户 `Request` 嵌入公共 `authorization.Subject`，HTTP 入口统一构造主体；管理操作直接调用 `RequireAdmin(ctx, r.Subject)`，避免各入口重复映射用户和会话字段。授权器负责转换查询错误，业务入口直接返回授权错误，不重复包装。
