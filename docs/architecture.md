# 目录结构与功能边界

本项目采用按业务模块组织的单体应用，入口为 `cmd/api/main.go` 和 `cmd/worker/main.go`。应用实现统一位于 `internal/`，依赖通过构造函数显式传入。API 和 Worker 分别构建、部署；Worker 当前消费持久化邮件队列。API 与 Worker 通过 Fx 装配依赖，业务模块不依赖 Fx。

## 应用装配

| 文件 | 职责 |
| --- | --- |
| `cmd/api/main.go` | 信号处理、执行 Cobra 命令树与退出码 |
| `internal/app/api/command.go` | serve、migrate、openapi 命令与配置按需加载 |
| `internal/app/api/config.go` | env 标签、配置规范化和业务校验 |
| `internal/app/appfx/` | 公共 Fx 基础设施、启动回滚和停止预算 |
| `internal/app/api/module.go` | API 的 Fx 依赖图 |
| `internal/app/api/accounts.go` | 账户依赖装配、密码与 GitHub OAuth 适配 |
| `internal/app/api/run.go`、`server.go` | 运行 Fx 应用、HTTP 启停及请求排空 |
| `internal/app/api/wire.go` | 共用的 NewServices 构造入口、管理员授权器注入、会话认证适配、依赖检查与 HTTP handler 构造 |
| `internal/app/api/routes.go` | 统一路由清单及离线 OpenAPI 导出 |

`app.Config` 留在装配层；HTTP 接收 `httpapi.Config`，账户服务接收独立的 `account.Options` 与功能依赖。业务服务不读取环境变量，也不访问全局连接池。API 与 Worker 的数据库 URL、连接数、关闭期限和遥测服务名由 `appfx.Config.Validate` 共用校验；环境变量与各入口默认值仍分别维护。日志器显式注入，不通过 SetDefault 修改进程全局日志。

启动钩子响应取消和超时；Fx 回滚使用独立上下文，预留停止预算释放已启动的资源。启动顺序为遥测、数据库、HTTP 或邮件消费者，停止时反转。API 停止信号先停止接收新连接并排空请求，超过期限再取消在途请求；Worker 取消消费并等待退出。`SHUTDOWN_TIMEOUT` 限制服务排空，应用额外预留 10 秒关闭基础设施。第三方登录不在启动时访问提供商。迁移通过独立命令执行。

## HTTP 与身份认证

`httpapi` 提供 Chi/Huma 初始化、请求标识和日志、超时上下文、异常恢复、公共错误映射、405 的 `Allow`、健康检查和操作契约。模块中的 `http.go` 声明路由并将请求映射为服务参数；`dto.go` 定义独立协议类型和模型到 DTO 的转换，通用响应包装由 `httpapi` 负责。原始业务错误由路由绑定层统一映射。

路由声明按配置、处理函数、响应转换分行书写。配置使用 `huma.Operation` 的具名字段表达方法、路径、操作 ID 和说明，避免连续字符串参数；处理函数使用 `service`、`ctx`、`input` 等明确名称，响应转换复用具名函数。认证策略仍显式传入，共用的错误契约、请求上限与缓存策略由模块配置函数集中补充。

`identity.Authenticator` 接收原始令牌并返回 subject 与 session ID。HTTP 中间件解析 `Authorization`，将认证结果放入请求上下文。`identity` 负责随机令牌、格式校验与摘要，不访问数据库；`account.Service` 查询会话、用户状态和认证版本，由 `app` 适配为认证接口。所有环境使用相同认证规则，无开发认证或固定演示令牌。非法凭据、认证依赖故障、请求超时分别映射为 401、503、504。

每条路由声明 `Public` 或 `Session` 策略，运行时中间件与 OpenAPI 共用声明；验证与回调的 token 是必填正文，由业务校验，Authorization 专用于会话。证明失效返回 422，实际会话失效仍返回 401；管理员操作在业务事务开始前通过注入的 `authorization.Authorizer` 重新检查角色和原会话；使用普通查询，不保证授权检查与后续写入串行一致。应用不配置 Origin 白名单或 CORS；跨域部署由网关处理预检和响应头，不引入 Cookie 认证。校验错误响应删除原始值、输入字段路径和解析消息，防止密码、令牌或 OAuth code 回显。

## 业务模块

账户、项目与任务采用相同基础文件职责；账户的注册、会话、方式管理、第三方流程和管理员操作按用例分文件，邮件消费单独归属 `mailoutbox` 模块：

| 文件或目录 | 职责 |
| --- | --- |
| `model.go` | 无 JSON 标签的 `Record`、业务状态、输入类型和业务错误 |
| `service.go` | 业务校验、所有权作用域、分页与数据库访问 |
| `http.go` | 类型化路由声明、请求参数到服务调用的适配 |
| `dto.go` | Huma 请求与响应类型、JSON/校验标签、业务模型到独立 DTO 的转换、公共响应类型别名 |
| `*_test.go` | 业务单元测试；集成测试统一放在 `tests/integration/<领域>/` |

数据库行不直接成为接口返回值，业务模型也不承担接口校验标签。项目 DTO 仍名为 `Project`，任务 DTO 仍名为 `Task`，使用资源名作为 OpenAPI schema 名称。

文件按完整职责聚合：项目与任务的校验、数据库行转换和存储错误策略随 `service.go` 维护；邮件状态定义归入 `model.go`，发送、结果落库与重试退避归入 `delivery.go`，轮询调度保留在 `runner.go`。账户模块按业务用例拆分，OAuth 发起、回调及完成逻辑放在同一个 `federation.go` 中。文件大小本身不作为拆分依据。

`dto.go` 保留一个显式 `toDTO`。当前模型与 DTO 字段一致，可以使用 `Project(item)` 或 `Task(item)` 的结构转换；二者仍是独立类型，字段不再匹配时编译失败，再改为逐字段映射。不要将 DTO 定义为 `Record` 或 sqlc 类型的别名。模块响应别名复用 `httpapi.ItemOutput` 与 `CreatedOutput`，不再编写 `createdOutput`、`itemOutput`、`listOutput`。

## 邮件发送

`platform/mail` 定义 `Message{ID, Kind, To, Subject, HTML}` 与 `Sender.Send`。当前 `LogSender` 仅记录消息 ID、类型、provider 和 `delivered=false`，不输出收件地址、标题、正文或验证链接。

账户模块通过 store 在业务事务中写入共享 `mail_outbox`；入队失败会回滚业务。`modules/mailoutbox` 在领取提交后调用发送函数，Worker 的 `module.go` 将其适配为 `platform/mail.Sender`。供应商故障由后台重试，不改变已提交的 API 结果。202 是统一受理响应，为防止邮箱枚举，并不保证每次请求都实际创建邮件。

租约、重试、有效期与终态载荷清理见 [邮件队列](mail-outbox.md)。接入 SendGrid 等服务时实现相同 Sender 并在 Worker 装配；发送器接受请求与实际送达分别处理。

## 数据库管理

数据库相关代码统一放在 `internal/db/`：

| 文件或目录 | 职责 |
| --- | --- |
| `internal/db/pool.go` | 初始化和校验 PostgreSQL 连接池 |
| `internal/db/migrate.go` | 执行嵌入二进制的版本迁移 |
| `internal/db/errors.go` | 按业务声明的策略识别缺失记录和唯一约束错误 |
| `internal/db/migrations/` | 全局有序的 SQL 迁移 |
| `internal/db/queries/` | 按完整表名命名，例如 `user_sessions.sql`、`auth_verifications.sql`；跨表查询归入主要资源 |
| `internal/db/sqlc/` | sqlc 统一生成的查询方法、参数和数据库模型 |

`sqlc.yaml` 只有一份生成配置，读取全部迁移与查询，生成包名为 `sqlc`。`make generate` 先清理旧的 `.sql.go` 再生成，防止改名残留，并自动规范为 `any`，`make check-generated` 检查统一输出与接口契约的一致性；生成文件不手工修改。

`app/appfx` 注入数据库适配器，在 OnStart 中通过 `db.Open()` 打开连接池；业务构造函数不查询在线数据库。服务内部通过 `sqlc.New()` 构造查询，HTTP 和 DTO 不直接依赖生成代码。集中生成减少配置重复，也让跨表查询与事务使用同一套类型；项目和任务服务继续使用各自的生成查询。账户模块通过私有 store 持有生成查询，仅显式暴露需要的操作；新增生成方法不会自动进入业务接口，架构检查禁止 store 之外构造或访问原始查询。业务仍应只操作职责范围内的数据，资源所有权必须由 SQL 条件保证。认证入口可按令牌摘要或可信第三方身份查找主体；管理员跨用户操作必须先通过公共授权接口检查角色和原会话，再开始业务事务。`audit_events` 是共享追加表，业务成功事件与变更在同一事务写入，失败记录在回滚后另写；生产数据库账号只拥有该表 INSERT 权限，查询与保留清理由独立运维权限控制。

SQL 在查询、更新和删除时同时限制资源 ID 与所有者。服务仍校验 subject，防止绕过 HTTP 直接调用时访问无作用域数据。不引入通用 CRUD repository；Fx 仅用于应用装配。跨表操作复用 `db.WithTransaction(ctx, database, ErrorPolicy, fn)`，统一提交、回滚和错误映射；账户事务内通过 `newStore(tx)` 校验写入。

显式锁仅保护不可接受的并发后果。低概率且可接受失败、重试或临时证明失效的竞争优先使用现有唯一约束、条件更新与版本校验，不新增协调层。只读列表和证明签发不预先锁定用户；密码、账号数量、会话安全及邮件独占领取仍保留必要保护。取消预读锁不取消数据库写入自身的锁或事务原子性。

## 公共错误处理

`apperror.Error` 只保存分类和安全提示，`Wrap` 同时保留具体业务错误与底层原因。`errors.Is` 可区分项目和任务的具体错误；`errors.As` 可读取公共分类。同一种错误分类不会让不同业务错误互相相等。

模块中的 HTTP 处理函数直接返回业务错误。`httpapi.Endpoint.Bind` 调用 `FromError`：不存在、冲突、无效输入、未认证、无权操作、限速、依赖故障分别对应 404、409、422、401、403、429、503。未知错误在兜底处理中检查请求超时，超时返回 504，其余记录原因后返回通用 500。业务分类优先于底层原因，HTTP 层明确构造的 Huma 错误也保持原语义，例如健康检查失败始终返回 503。

认证中间件在处理函数之前运行，继续负责 Bearer 请求头、401 登录挑战以及认证依赖故障到 503/504 的映射。它与业务错误映射承担不同的协议职责。

`db.MapError` 根据模块的 `ErrorPolicy` 转换 `pgx.ErrNoRows` 和已声明的唯一约束。未知约束不推断业务含义，取消和超时错误保留原样；错误链中的数据库细节不会进入公开提示。删除影响零行的判断仍由业务方法负责。

路由适配器按返回形式选择：

- `CreatedEndpoint(operation, execute, toDTO, location)`：成功返回 201，先转换独立 DTO，再从 DTO 构造 `Location`。
- `ItemEndpoint(operation, execute, toDTO)`：查询和更新成功返回 200，统一包装 DTO 正文。
- `PageEndpoint[具名分页 DTO](operation, execute, toDTO)`：成功返回 200，转换分页条目并保留分页元数据，空列表输出 `[]`。
- `MapEndpoint(operation, execute, present)`：特殊响应的底层适配器，仅成功时执行自定义响应组装。
- `NoContentEndpoint(operation, execute)`：适配只返回错误的调用，默认成功返回 204 且没有正文。
- `Endpoint(operation, handle)`：保留对完整 HTTP 输出的直接控制，例如健康检查。

模块的 `Routes()` 只列出操作声明、业务调用函数、DTO 转换和创建地址。请求中的 subject、任务状态、创建响应的 `Location` 等仍由模块显式处理，公共适配器只负责所选响应形式。所有入口最终共用错误映射和离线描述；导出契约时不会执行业务调用或响应组装。

## 公共分页与校验

`pagination.Params` 集中限制分页参数，服务在查询前调用 `Validate`，再用 `FetchLimit` 多取一条记录。`Build` 截断额外记录并调用字段转换函数，`Map` 在类型转换时保留分页元数据和非 nil 空列表。

HTTP 查询标签放在 `httpapi.PageQuery`，通过 `Params()` 传入业务层。`httpapi.PageFrom` 将业务分页结果转换为响应类型。模块使用 `ProjectListResponse` 和 `TaskListResponse` 具名类型，明确资源归属与响应用途。`PageEndpoint` 的首个类型参数显式指定具名分页 DTO，编译期约束保证结构与 `httpapi.Page[DTO]` 一致，无需反射或字符串命名规则。Go 结构标签不能引用常量，标签与分页规则的一致性由测试检查。

`validation.RequiredText` 按字符数检查必填文本并去除首尾空白；`TextWithin` 保留描述字段的空白并允许空字符串。字段上限、任务状态和创建默认值仍由业务模块决定。DTO 与业务代码负责字段校验，数据库不使用 CHECK，保留主键、外键、唯一约束与 NOT NULL。项目和任务服务同时限制所有者长度；账户模块的 store 在写入前校验凭据组合、流程用途、摘要长度、会话时限和审计数据。

所有环境统一使用数据库会话认证，主体格式由 `identity.RequireSubject` 校验，上限按字节计算。前端和第三方协议端点统一要求 HTTPS；环境标识不改变安全规则。服务入口保留主体校验。

## 测试复用

Fx 与应用测试统一通过 `api.NewServices` 构造依赖；`internal/app/api/test_helpers_test.go` 只替换测试需要的认证和健康检查能力。本地会话与用户体系使用真实 PostgreSQL 验证。

真实 PostgreSQL、HTTP 请求等已有夹具继续复用；业务 CRUD、所有权、迁移链和生命周期场景保留独立断言。

## Schema 命名

公开 schema 使用 PascalCase，名称表达资源与用途，不携带 Huma 的输入输出包装细节：

| 用途 | 规则 | 示例 |
| --- | --- | --- |
| 资源模型 | `<资源>` | `Project`、`Task` |
| 请求正文 | `<资源><动作>Request` | `ProjectCreateRequest`、`ProjectUpdateRequest`、`TaskCreateRequest`、`TaskUpdateRequest` |
| 列表响应 | `<资源>ListResponse` | `ProjectListResponse`、`TaskListResponse` |
| 公共响应 | `<用途>Response` | `HealthResponse`、`ErrorResponse` |
| 错误明细 | `ErrorDetail` | 公共错误的条目类型 |

业务正文使用具名 DTO，避免匿名字段派生出 `TaskCreateInputBody` 等名字。Huma 的 `Input`、`Output` 包装仍用于 Go 路由参数与响应头，不作为公开正文的命名规则。项目创建和更新目前字段相同，使用两个独立的具名请求类型；更新类型复用创建类型的底层结构，将来校验规则不同再单独定义。

`httpapi.New` 通过当前 API 的 schema registry 将 Huma 自带的 `ErrorModel` 命名为 `ErrorResponse`，其余类型沿用 DTO 名称；不修改全局错误工厂；当前 API 的响应转换器负责校验错误脱敏。命名调整会改变 OpenAPI 的组件名与引用，使用契约生成 SDK 的客户端需重新生成类型；HTTP 路径、状态码和 JSON 字段不变。

## 离线契约

各模块的 `Routes()` 声明 Huma operation、输入输出类型和处理函数。`httpapi.Route` 支持两种用途：

- `Bind`：传入真实服务和认证中间件，注册运行时处理函数。
- `Describe`：只向独立的契约路由器注册类型信息，不持有服务依赖。

`internal/app/api/routes.go` 的清单同时用于运行时和离线导出。`app.OpenAPI()` 不加载环境配置、连接 PostgreSQL 或访问第三方服务，也不返回可以对外服务的处理器。`app.NewHandler()` 要求完整运行时依赖，缺失则立即返回错误。单测比较开启和关闭在线文档时的契约与离线契约，防止生成结果与服务偏离。

## 包边界

允许的应用内依赖方向（除 main 外，以下包均位于 `internal/`）：

```text
main → app（Cobra 命令入口）
app → modules、httpapi、identity、db、platform
modules/<业务> 的 HTTP → httpapi、identity、pagination
modules/<业务> 的业务实现 → db、db/sqlc、apperror、pagination、validation、authorization
modules/account 的业务实现 → identity（令牌生成与摘要）
modules/<业务> 的 model → apperror
modules/<业务> 的 DTO → httpapi（公共响应类型）
httpapi → identity、apperror、pagination
db → apperror、标准库与数据库依赖
platform → platform 内的基础设施
identity → apperror、标准库
apperror、pagination、validation → 标准库
```

根目录的 `internal/` 通过 Go 导入规则限制仓库外部代码依赖应用实现，但不限制仓库内部各包互相导入。`tests/architecture_test.go` 继续约束内部依赖方向、数据库生成类型的使用位置以及模型/协议层隔离，并禁止生产代码依赖 `tests/integration/testutil`。模块文件默认使用业务依赖集合，仅 `http.go/*_http.go` 与 `dto.go/*_dto.go` 是协议依赖例外，拆分到新业务文件不会解除约束。根目录 `tests/` 中的测试可以正常导入这些内部包。此仓库不提供稳定的 Go SDK，需要对外发布 SDK 时应另建明确的公共契约。

## 新增业务模块

1. 在 `internal/modules/<业务>/` 建立业务模型、服务、HTTP 和 DTO，保持服务不依赖协议类型；复用公共业务错误、分页和文本校验，并声明本业务的数据库错误策略。
2. 需要新表时，在 `internal/db/migrations/` 增加下一个全局版本的迁移；查询按完整表名命名并放入 `internal/db/queries/`。
3. 执行 `make generate`，更新 `internal/db/sqlc/`。同一数据库下新增业务查询无需增加 sqlc 配置或修改生成脚本。
4. 在 `internal/app/api/wire.go` 的 `Dependencies` 与 `NewServices` 中增加服务，Fx 和测试共用该构造入口；在 `internal/app/api/routes.go` 清单中绑定模块路由。通过 `httpapi.NewOperation` 明确声明 Public 或 Session，不直接编写 Security。
5. 单元测试随业务代码放置；真实数据库及 HTTP 集成测试统一放入 `tests/integration/<领域>/`，复用 `tests/integration/testutil`；跨模块与生命周期场景放入 `tests/integration/` 根部。
6. 执行 `make generate`、`make check`、`make test-integration` 和 `make build`。

迁移只调整目录时必须保持版本和内容不变，防止迁移历史分叉。

## Worker 装配

`internal/app/api` 与 `internal/app/worker` 独立解析配置和管理生命周期，两者不互相导入。`cmd/worker` 只处理信号与退出状态。Worker 通过 `appfx` 装配日志、遥测和数据库，`module.go` 装配邮件 outbox 消费与日志发送器；不装配认证清理任务或 Locker。队列处理失败记录固定错误日志后按轮询间隔重试。

公共 Locker 位于 `platform/locker`，PostgreSQL 后端位于其 `postgres` 子包，由实际使用方按需装配。接口不依赖 Worker，详见 [Locker 设计](locker.md)。

将来接入 Temporal 时，由 Worker 注册 Workflow/Activity，公共客户端实现放在 `platform/temporal`，业务编排放在 `workflows/<业务>`。业务模块不依赖 Temporal SDK。当前不创建空目录或通用任务引擎抽象。

## 配置与命令

Cobra 命令树每次独立构造，不使用全局命令实例或 init 注册。环境转换统一复用 platform/configenv 与 caarlos0/env；业务 Validate 与解析分离，运行函数显式接收 Config。`migrate` 不要求认证配置，帮助与 `openapi` 不加载配置。公共解析入口支持注入 lookup 且不回退到进程环境；拒绝显式空的默认字段，对库转换错误进行脱敏。
