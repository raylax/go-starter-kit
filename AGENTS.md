# 仓库约定

- 使用 `any` 和 `...any`，禁止旧的空接口写法。`make fmt` 统一格式，`make lint` 检查此约定，包括生成的 Go 代码。
- HTTP 状态码与方法使用 `net/http` 的具名常量，测试同样遵守。业务限制、容量上限、协议长度和重试策略在所属模块就近定义常量，能推导的值通过计算得到；容量常量用名称说明用途与字节单位，并标注 KiB/MiB，不在调用处内联位移表达式，即使目标字段已有名称。普通计数和测试样例无需机械创建常量。结构标签保留静态字面量，并与业务规则同步维护。
- 注释和说明文档使用中文；Go 标识符保留英文。生成器标记、工具指令和第三方固定文本按工具要求保留。
- `cmd/api/main.go` 与 `cmd/worker/main.go` 只处理进程信号、执行命令树和退出码；Cobra 命令定义在各自 `internal/app/<入口>/command.go`。应用实现统一放在 `internal/`。
- `internal/app/api/` 负责配置、依赖装配、路由清单和生命周期；`internal/httpapi/` 负责 HTTP 公共能力；`internal/identity/` 负责随机令牌、格式校验和摘要，不依赖数据库、应用配置或 HTTP 处理器；会话与用户有效性由 `modules/account` 校验，`app` 适配认证接口；`internal/db/` 管理数据库连接、迁移与查询；`internal/platform/` 提供遥测基础设施。
- 业务按 `internal/modules/<业务名>/` 组织。`model.go` 定义业务模型和错误，`service.go` 实现规则与数据访问，`http.go` 处理协议映射，`dto.go` 定义 API 类型；SQL 查询集中在 `internal/db/queries/`，查询文件名与完整表名一致，跨表查询归入主要返回或写入的资源；生成代码集中在 `internal/db/sqlc/`。
- 禁止模块直接依赖其他模块；共享的 `internal/db/sqlc` 仅用于业务的数据访问实现，HTTP 和 DTO 不直接使用生成类型。跨模块组合在应用层显式装配。外部导入由 Go 的 `internal` 规则限制，仓库内部依赖方向由 `tests/architecture_test.go` 检查。
- 公共业务错误定义在 `internal/apperror/`，业务模块保留具体错误提示；`httpapi.Endpoint` 在运行时统一调用 `FromError`，模块处理函数直接返回业务错误，禁止重复实现状态映射。普通业务接口使用 `CreatedEndpoint`、`ItemEndpoint`、`PageEndpoint[具名分页 DTO]` 统一响应包装；特殊响应可使用 `MapEndpoint`；无正文接口使用 `NoContentEndpoint`，自定义完整处理函数仍可使用 `Endpoint`。底层原因通过错误链保留，HTTP 只公开业务提示。
- 数据库错误使用 `db.MapError`，模块声明 `ErrorPolicy`；唯一约束必须按名称匹配，未知约束保持内部异常。
- 分页规则复用 `pagination.Params`、`Build` 和 `Map`；HTTP 分页字段复用 `httpapi.PageQuery`、`Page` 和 `PageFrom`。模块列表响应使用具名类型提供稳定的 OpenAPI schema 名称。
- 文本基础校验使用 `internal/validation/`，具体字段上限和状态规则由业务模块决定；认证参数复用 `identity` 校验，所有环境使用一致的认证规则。保留各服务入口的所有者检查；字段长度、枚举和字段组合规则由 DTO 与业务代码校验，不使用数据库 CHECK。数据库保留主键、外键、唯一约束及 NOT NULL。账户写入通过模块内 store 执行校验，禁止绕过该入口直接调用生成写入方法。
- UUID 由 Go 代码生成并显式写入，默认使用 v4（`uuid.New()`），数据库 UUID 列不设置生成默认值，不依赖数据库的 UUID 生成功能。生成策略由对应资源的创建代码维护，后续可按表独立切换 v7；不改写已有记录 ID。
- OpenAPI schema 使用 PascalCase：资源模型为 `<资源>`，请求正文为 `<资源><动作>Request`，列表响应为 `<资源>ListResponse`；公共响应为 `HealthResponse`、`ErrorResponse`，错误明细为 `ErrorDetail`。公开正文使用具名 DTO，不使用匿名正文生成的 `InputBody`、`OutputBody` 或无资源前缀的 `WriteBody`；修改源码后重新生成契约，禁止手改 JSON。
- API DTO 与业务模型、生成的数据库类型分别维护。所有业务资源的读写 SQL 必须按已认证的 subject 限定作用域。
- 迁移集中放在 `internal/db/migrations/`，全局有序。修改表结构时新增迁移，不修改已应用到部署数据库的历史迁移。
- 仅为不可接受的并发后果加显式锁；低概率且可接受失败、重试或证明失效的竞争不增加锁与协调层。优先使用唯一约束、条件更新和版本复核；跨表安全不变量、账号数量限制和任务独占领取仍需保护，事务原子性不因删锁而取消。
- 修改 SQL、迁移或 API 类型后运行 `make generate`。禁止手改 `internal/db/sqlc/` 或 `api/openapi.json`；生成过程包含 `any` 规范化。
- 管理能力归属对应业务模块：管理业务、HTTP 映射和专用 DTO 分别放在 `admin.go`、`admin_http.go`、`admin_dto.go`，通过 `AdminRoutes()` 由应用层单独装配。管理入口通过注入的 `authorization.Authorizer` 检查权限；跨领域管理操作由应用层组合。将来需要独立管理进程时，使用 `internal/app/admin` 装配已有业务模块。
- 路由契约通过 `internal/app/api/routes.go` 的统一清单装配。离线导出不能初始化数据库、认证客户端或加载运行配置，也不能用空依赖调用运行时构造函数。
- 代码变更运行 `make check`；数据库、API、认证、迁移或生命周期变更还须运行 `make test-integration`。需要 Docker 的验收不能因基础设施不可用而静默跳过。
- 所有集成测试统一放在 `tests/integration/`，按业务与基础设施分子目录；应用装配和生命周期场景放在该目录根部。共享夹具位于 `tests/integration/testutil/`，仅供测试使用。业务目录只保留单元测试；集成测试通过公开入口和依赖注入验证行为，不为测试暴露业务内部实现。
- 不记录或提交凭据、Bearer 令牌和真实用户数据。`.env` 仅供本地使用并已忽略。

## 用户体系约定

- 会话令牌以 `tk_` 开头，流程与邮件挑战分别使用 `flow_`、`verify_`；认证表仅保存摘要；邮件挑战原文可短期存在 outbox 正文，投递终态清除。Authorization 专用于会话；验证及 OAuth 回调的令牌通过正文 `token` 传入，令牌响应也统一使用 `token`。记录引用保留 `session_id`、`flow_id` 等语义名称；不添加 Cookie、刷新令牌或本地 JWT。
- 当前第三方登录仅支持 GitHub OAuth，不引入 OIDC、JWT 验签、JWKS 或 nonce；OAuth 保留 state 与 PKCE。认证限速键使用类别与对象的 SHA-256 摘要，无独立密钥配置。`FRONTEND_URL` 仅用于生成邮件验证链接；应用不配置 Origin 白名单或 CORS，跨域响应头由部署网关处理。OAuth 回调地址由提供商配置独立声明。
- User 是业务主体，Account 是可撤销登录方式；第三方身份不按邮箱自动合并。账户模块的业务实现可依赖 `identity` 的令牌能力，不反向依赖 `httpapi` 或其他模块。
- 登录/验证入口可按凭据摘要定位主体；普通账户操作以当前用户和原会话限定范围；管理员操作在业务事务开始前通过公共授权接口重新检查用户角色与原会话，使用普通查询。
- `audit_events` 是各模块可追加的共享审计表。成功业务变更和审计同事务；失败事件在回滚后另写。查询限定归属或管理员权限，禁止以 metadata 隐藏授权条件；运行账号不得修改、删除审计。
- 提供商客户端密钥通过持久化秘密文件注入，生产不随机重建；OAuth 协议状态暂以 JSON 原文保存，流程成功或失败时清除，不记录到日志或审计。邮件与业务变更在同一事务中写入共享 `mail_outbox`；独立 `modules/mailoutbox` 负责租约领取、重试与终态清理，Worker 通过 app 适配 `platform/mail.Sender`。当前日志 mock 不输出地址或正文；外部发送在领取提交后执行，邮件 ID 作为供应商幂等键。正文暂明文保存，成功或最终失败后清除。
- 错误响应、日志、审计和指标禁止回显密码、令牌、Authorization、OAuth code、PKCE verifier、邮件正文及恢复链接。

- 验证/流程/重新认证证明失效使用 422，与会话中间件的 401 `session_invalid` 区分。需要正文证明的解绑使用 POST 动作接口，DELETE 保持无正文。
- `internal/app/worker/` 独立负责后台进程配置和启停，不依赖 `app/api`。当前装配数据库与邮件 outbox 消费，不装配认证清理任务或 Locker。公共 `platform/locker` 由使用方按需注入，PostgreSQL 后端使用独立连接池；新增后台任务应支持 context 取消并明确重试与幂等规则。

- 环境配置使用 caarlos0/env，经 `platform/configenv` 统一处理显式空值与错误脱敏；业务规则由 Config.Validate 维护，Run(ctx, cfg) 不读取环境。命令按需加载配置，帮助和离线 OpenAPI 不初始化运行依赖。

- 业务服务入口使用 `identity.RequireSubject` 校验资源归属主体并统一返回未认证错误；主体格式统一为非空白、有效 UTF-8、无 NUL、最多 255 字节。`identity` 可依赖公共 `apperror`，不依赖 HTTP 或数据库；会话有效性仍由认证层验证。

- 固定业务取值使用按领域区分的字符串枚举及 `Valid()`，业务方法与模型使用枚举类型，SQL 边界显式转换；状态转换使用独立规则。提供商 ID、审计 action/resource/reason、邮件 kind 等扩展标识保留字符串。无需 stringer，不引入数据库 CHECK；SQL 中的固定状态及 DTO enum 标签与领域枚举同步维护。

- API 与 Worker 在 `internal/app` 使用 Fx 装配依赖和生命周期，共享基础设施位于 `app/appfx`。业务模块不导入 Fx；资源在 OnStart 初始化，服务停止后才关闭数据库和遥测。构造函数不执行依赖数据库在线状态的查询；migrate/openapi 不构建 Fx 应用。

- 不提供 Development 认证模式、固定演示令牌或环境相关的安全豁免。OAuth 端点使用 HTTPS。APP_ENV 仅为环境标识，不改变认证规则；固定身份夹具仅允许位于测试辅助代码。

- `internal/authorization` 只定义 Subject 与 Authorizer 契约，不依赖业务模块。账户模块提供独立 AdminChecker，应用层适配并通过 Fx 注入，避免账户 Service 与授权器循环依赖。需要管理功能的其他模块只依赖授权接口，禁止直接依赖 account；权限查询与业务事务不保证串行一致性。
